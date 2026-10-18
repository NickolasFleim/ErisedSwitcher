//go:build windows

// Package app связывает всё вместе на Windows: перехват клавиш, ядро, ввод исправлений,
// значок в трее, окно настроек, сохранение файлов.
package app

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"erised/internal/config"
	"erised/internal/engine"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
	"erised/internal/settingswin"
	"erised/internal/trayicon"
	"erised/internal/webui"
	"erised/internal/win"
	"erised/internal/words"
)

// Version — версия программы. При сборке релиза подставляется из тега:
// go build -ldflags "-X erised/internal/app.Version=1.4.0" (так она не разойдётся с тегом,
// иначе программа считала бы выпуск на GitHub новее себя и обновлялась бы без конца).
var Version = "1.4.0"

// AppName — название программы.
const AppName = "Erised Switcher"

// Надпись на раме Зеркала Еиналеж (Erised — это desire задом наперёд).
const mirrorInscription = "Erised stra ehru oyt ube cafru oyt on wohsi"

const autostartName = "Erised Switcher"

const (
	className   = "ErisedSwitcherTray"
	mutexName   = "Local\\ErisedSwitcher"
	wmTray      = win.WM_APP + 1
	wmShowUI    = win.WM_APP + 2
	wmRefresh   = win.WM_APP + 3
	timerIcon   = 1
	timerFiles  = 2
	timerRehook = 3
)

// Пункты меню.
const (
	miUpdate   = 90
	miSettings = 100 + iota
	miEnabled
	miToggleApp
	miOpenDir
	miReload
	miAutostart
	miAdmin
	miAbout
	miExit
)

type rawKey struct {
	vk, scan uint16
	flags    uint32
	up       bool
}

type job struct {
	actions []engine.Action
	info    win.WindowInfo
	rule    config.Effective
}

// App — запущенная программа.
type App struct {
	dir       string
	cfgPath   string
	wordsPath string

	cfg   atomic.Pointer[config.Config]
	store *words.Store
	eng   *engine.Engine
	ui    *webui.Server

	hwnd           uintptr
	tray           win.Tray
	hooks          win.Hooks
	taskbarCreated uint32

	// Состояние хука (поток хука).
	mods         engine.Mods
	lastWin      uintptr
	ruleCache    map[string]config.Effective
	ruleVer      uint64
	cfgVer       atomic.Uint64
	tracked      atomic.Int32 // keymap.Lang: раскладка, известная нам самим
	consoleL     keymap.Lang
	consoleAt    time.Time
	consolePID   uint32
	consoleStale atomic.Bool
	selfExe      string // имя нашего exe в нижнем регистре
	comboMods    int    // для отслеживания Alt+Shift / Ctrl+Shift (режим «tracked»)
	mouseDownPt  win.POINT
	mouseDownT   uint32

	// Подсказки.
	hist        *predict.History
	pred        *predict.Predictor
	popup       *win.Popup
	popupWindow atomic.Uint64 // окно, для которого показана полоска

	upd             *updater // обновления
	crash           *crashes
	crashedLastTime bool

	// Очередь исправлений.
	jobs      chan job
	busyMu    sync.Mutex
	busy      bool
	busySince time.Time
	queued    []rawKey

	// Для трея.
	iconKey     string
	icon        uintptr
	prevApp     string // последнее «чужое» активное приложение
	prevAppMu   sync.Mutex
	layoutsDesc string

	logMu sync.Mutex
	logs  []engine.LogEntry

	recentMu sync.Mutex
	recent   map[string]time.Time

	// Поля ниже меняются из разных потоков — под mu.
	mu               sync.Mutex
	hkl              [3]win.HKL
	cfgMod, wordsMod time.Time
	lastSave         time.Time
}

func (a *App) getTracked() keymap.Lang  { return keymap.Lang(a.tracked.Load()) }
func (a *App) setTracked(l keymap.Lang) { a.tracked.Store(int32(l)) }

// Run — точка входа. Возвращает код выхода.
func Run() int {
	runtime.LockOSThread()
	restarted := len(os.Args) > 1 && (os.Args[1] == "--quiet" || os.Args[1] == "--after-install")
	single := win.SingleInstance(mutexName)
	// После «Перезапустить от администратора» прежний экземпляр ещё мгновение жив — подождём.
	// После обновления новая версия запускается, пока старая ещё может доживать — тоже ждём.
	if !single {
		for i := 0; !single && i < 20; i++ {
			time.Sleep(100 * time.Millisecond)
			single = win.SingleInstance(mutexName)
		}
	}
	for i := 0; !single && restarted && i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		single = win.SingleInstance(mutexName)
	}
	if !single {
		if h := win.FindWindow(className); h != 0 {
			win.PostMessage(h, wmShowUI, 0, 0)
		}
		return 0
	}
	win.IgnoreConsoleCtrlC()
	win.EnableDPIAwareness()
	a := &App{dir: config.Dir(), jobs: make(chan job, 64), recent: map[string]time.Time{}, ruleCache: map[string]config.Effective{}}
	a.setTracked(keymap.Unknown)
	if exe, err := os.Executable(); err == nil {
		a.selfExe = strings.ToLower(filepath.Base(exe))
	}
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		win.MessageBox(AppName, "Не удалось создать папку данных:\n"+a.dir+"\n"+err.Error(), 0x10)
		return 1
	}
	if f, err := os.OpenFile(filepath.Join(a.dir, "erised.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Printf("Erised Switcher %s запускается, данные в %s", Version, a.dir)
	a.setupCrashes()
	defer func() {
		// Сбой в главном потоке: сохраняем отчёт и выходим, не оставляя «висящий» перехват.
		if r := recover(); r != nil {
			a.onPanic("главный поток", r, debug.Stack())
			a.hooks.Uninstall()
			a.tray.Remove()
			os.Exit(1)
		}
	}()
	migrated := migrateFromSmena(a.dir)
	for _, n := range migrated {
		log.Printf("переход со Smena: %s", n)
	}
	a.cfgPath = filepath.Join(a.dir, "config.json")
	a.wordsPath = filepath.Join(a.dir, "words.json")

	cfg, cfgErr := config.Load(a.cfgPath)
	a.cfg.Store(cfg)
	st, wErr := words.Load(a.wordsPath)
	if wErr != nil {
		log.Printf("words.json: %v", wErr)
		// Не затираем повреждённый файл: сохраним копию.
		_ = copyFile(a.wordsPath, a.wordsPath+".broken")
	}
	a.store = st
	a.cfgMod, a.wordsMod = modTime(a.cfgPath), modTime(a.wordsPath)
	a.lastSave = time.Now()
	if a.offerInstall(cfg) {
		return 0 // установленная копия запускается сама
	}
	cfg = a.cfg.Load()

	go lang.Preload()
	a.eng = engine.New(cfg, st)
	a.eng.SetLogger(a.addLog)
	a.findLayouts() // до подсказок: от включённых языков зависит, какие индексы строить
	a.initPredict()

	a.hwnd = win.CreateMessageWindow(className, syscallCallback(a.wndProc))
	a.taskbarCreated = win.RegisterWindowMessage("TaskbarCreated")
	a.tray = win.Tray{Hwnd: a.hwnd, ID: 1, CbMsg: wmTray}
	a.refreshIcon(true)

	if ui, err := webui.Start(a, cfg.UIPort); err != nil {
		log.Printf("окно настроек: %v", err)
	} else {
		a.ui = ui
	}

	a.hooks = win.Hooks{OnKey: a.onKey, OnMouse: a.onMouse}
	if err := a.hooks.Install(); err != nil {
		win.MessageBox(AppName, err.Error(), 0x10)
		return 1
	}
	defer a.hooks.Uninstall()

	go a.worker()

	// Путь в автозапуске обновляем всегда: программу могли установить или перенести.
	if cfg.Autostart || win.AutostartEnabled(autostartName) {
		a.applyAutostart(cfg.Autostart)
	}

	win.SetTimer(a.hwnd, timerIcon, 250)
	win.SetTimer(a.hwnd, timerFiles, 2000)
	win.SetTimer(a.hwnd, timerRehook, 5*60*1000)

	if cfgErr != nil {
		a.tray.Balloon("Ошибка в настройках", cfgErr.Error()+"\nИспользуются значения по умолчанию.", true)
	} else if wErr != nil {
		a.tray.Balloon("Ошибка в словаре", "words.json повреждён, копия сохранена как words.json.broken", true)
	} else if a.crashedLastTime {
		a.tray.Balloon("Прошлый запуск завершился с ошибкой", "Отчёт сохранён — отправить его можно в настройках, вкладка «Журнал».", true)
	} else if len(migrated) > 0 {
		a.tray.Balloon("Smena теперь Erised Switcher", strings.Join(migrated, "; ")+".", false)
	} else if len(os.Args) > 1 && os.Args[1] == "--after-install" {
		a.tray.Balloon("Erised Switcher установлен", "Он в «Пуске» и в области уведомлений. Настройки — щелчком по значку.", false)
	} else if len(os.Args) < 2 || os.Args[1] != "--quiet" {
		a.tray.Balloon("Erised Switcher работает", "Раскладка исправляется автоматически. Настройки — по щелчку на значке.", false)
	}

	a.startUpdater() // после приветствия: сообщение об обновлении важнее

	win.MessageLoop()

	a.tray.Remove()
	if a.ui != nil {
		a.ui.Close()
	}
	if err := a.store.SaveIfDirty(); err != nil {
		log.Printf("сохранение слов: %v", err)
	}
	if a.hist != nil {
		_ = a.hist.SaveIfDirty()
	}
	log.Printf("Erised Switcher завершена")
	return 0
}

func syscallCallback(f func(hwnd uintptr, msg uint32, w, l uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(hwnd uintptr, msg uint32, w, l uintptr) uintptr { return f(hwnd, msg, w, l) })
}

func modTime(p string) time.Time {
	if fi, err := os.Stat(p); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func (a *App) findLayouts() {
	cfg := a.cfg.Load()
	hkl := [3]win.HKL{
		keymap.EN: win.FindLayout(keymap.EN, cfg.LayoutEN),
		keymap.RU: win.FindLayout(keymap.RU, cfg.LayoutRU),
		keymap.UK: win.FindLayout(keymap.UK, cfg.LayoutUK),
	}
	// Какие языки включены: заданы в настройках или те, чьи раскладки установлены в Windows.
	var langs []keymap.Lang
	if strings.TrimSpace(cfg.Languages) == "" || cfg.Languages == "auto" {
		for _, l := range []keymap.Lang{keymap.RU, keymap.UK} {
			if hkl[l] != 0 {
				langs = append(langs, l)
			}
		}
	} else {
		for _, part := range strings.Split(cfg.Languages, ",") {
			if l := keymap.ParseLang(part); l != keymap.Unknown {
				langs = append(langs, l)
			}
		}
	}
	keymap.SetLanguages(langs)
	names := map[keymap.Lang]string{keymap.EN: "английская", keymap.RU: "русская", keymap.UK: "украинская"}
	var parts []string
	for _, l := range keymap.Languages() {
		if hkl[l] == 0 {
			parts = append(parts, names[l]+" не найдена")
		} else {
			parts = append(parts, fmt.Sprintf("%s %08X", strings.ToUpper(l.String()), uint32(hkl[l])))
		}
	}
	a.mu.Lock()
	a.hkl = hkl
	a.layoutsDesc = strings.Join(parts, ", ")
	a.mu.Unlock()
	log.Printf("раскладки: %s", strings.Join(parts, ", "))
}

func (a *App) layoutsText() string { a.mu.Lock(); defer a.mu.Unlock(); return a.layoutsDesc }

// ---------- Хук клавиатуры (поток хука) ----------

func (a *App) trackMods(vk uint16, up bool) {
	d := !up
	switch vk {
	case keymap.VK_LSHIFT, keymap.VK_SHIFT:
		a.mods.LShift = d
	case keymap.VK_RSHIFT:
		a.mods.RShift = d
	case keymap.VK_LCONTROL, keymap.VK_CONTROL:
		a.mods.LCtrl = d
	case keymap.VK_RCONTROL:
		a.mods.RCtrl = d
	case keymap.VK_LMENU, keymap.VK_MENU:
		a.mods.LAlt = d
	case keymap.VK_RMENU:
		a.mods.RAlt = d
	case keymap.VK_LWIN:
		a.mods.LWin = d
	case keymap.VK_RWIN:
		a.mods.RWin = d
	}
}

func (a *App) onKey(ev win.KeyHookEvent) bool {
	if ev.Extra == win.MagicOwn {
		return false
	}
	replay := ev.Extra == win.MagicReplay
	cfg := a.cfg.Load()
	if ev.Injected && !replay && !cfg.ProcessInjected {
		return false
	}

	// Пока печатается исправление, ваши нажатия придерживаются и потом повторяются.
	if !replay {
		a.busyMu.Lock()
		if a.busy && time.Since(a.busySince) > 3*time.Second {
			a.busy = false // страховка: никогда не держим клавиатуру дольше 3 секунд
		}
		if a.busy {
			a.queued = append(a.queued, rawKey{ev.VK, ev.Scan, ev.Flags, ev.Up})
			a.busyMu.Unlock()
			return true
		}
		a.busyMu.Unlock()
	}

	before := a.mods
	a.trackMods(ev.VK, ev.Up)
	a.watchLayoutCombos(ev, before)

	info := win.Describe(a.rulesNeedTitle(cfg))
	if info.Hwnd != a.hwnd && info.Process != "" && !ev.Up {
		a.noteRecent(info.Process)
		// «Выключить в …» в меню трея относится к программе, в которой вы печатали последней
		// (при щелчке по трею активным становится сам рабочий стол).
		a.prevAppMu.Lock()
		a.prevApp = info.Process
		a.prevAppMu.Unlock()
	}
	rule := a.resolve(cfg, &info)
	if info.Process == a.selfExe {
		// В собственном окне настроек ничего не исправляем (горячие клавиши вкл/выкл работают).
		rule.Mode = "off"
	}
	// Раскладку спрашиваем только при наборе текста: для консоли это подключение к ней,
	// и делать его на Ctrl+C или на отпускание клавиш незачем.
	layout := a.getTracked()
	if !ev.Up && !before.Ctrl() && !before.Alt() && !before.Win() {
		layout = a.currentLang(&info, &rule)
	}
	ctx := &engine.Context{
		Window:   info.Hwnd,
		App:      info.Process,
		Class:    info.Class,
		Title:    info.Title,
		Layout:   layout,
		Password: info.Password,
		Rule:     rule,
	}
	kev := engine.KeyEvent{VK: ev.VK, Down: !ev.Up, Mods: before, Caps: win.CapsLockOn(), Time: ev.Time}
	if ev.VK == keymap.VK_CAPITAL && !ev.Up {
		kev.Caps = !kev.Caps
	}
	res := a.eng.HandleKey(kev, ctx)
	if res.SuggestChanged {
		a.pushSuggestions(&info)
	}
	if len(res.Actions) > 0 {
		a.busyMu.Lock()
		a.busy, a.busySince = true, time.Now()
		a.busyMu.Unlock()
		select {
		case a.jobs <- job{actions: res.Actions, info: info, rule: rule}:
		default:
			log.Printf("очередь исправлений переполнена")
			a.busyMu.Lock()
			a.busy = false
			a.busyMu.Unlock()
		}
	}
	return res.Swallow
}

// Мышь: щелчок сбрасывает набранное слово; протяжка, двойной щелчок и Shift+щелчок
// обычно выделяют текст — тогда двойной Shift конвертирует выделение.
func (a *App) onMouse(msg uint32, pt win.POINT, t uint32) {
	near := func(p, q win.POINT, d int32) bool {
		dx, dy := p.X-q.X, p.Y-q.Y
		return dx*dx+dy*dy <= d*d
	}
	if a.popup != nil && a.popup.Contains(pt) {
		return // щелчок по полоске подсказок: набранное слово не сбрасываем
	}
	if a.popup != nil && msg != win.WM_LBUTTONUP {
		a.popup.Hide()
	}
	switch msg {
	case win.WM_LBUTTONDOWN:
		a.eng.Reset()
		double := t-a.mouseDownT <= win.DoubleClickTime() && near(pt, a.mouseDownPt, 4)
		a.mouseDownPt, a.mouseDownT = pt, t
		a.eng.NoteSelection(double || a.mods.Shift())
	case win.WM_LBUTTONUP:
		if !near(pt, a.mouseDownPt, 4) {
			a.eng.NoteSelection(true)
		}
	default:
		a.eng.Reset()
	}
}

// Для режима «tracked»: замечаем системные сочетания переключения раскладки.
func (a *App) watchLayoutCombos(ev win.KeyHookEvent, before engine.Mods) {
	isMod := func(vk uint16) bool {
		switch vk {
		case keymap.VK_LSHIFT, keymap.VK_RSHIFT, keymap.VK_LCONTROL, keymap.VK_RCONTROL, keymap.VK_LMENU, keymap.VK_RMENU, keymap.VK_LWIN, keymap.VK_RWIN:
			return true
		}
		return false
	}
	if !ev.Up {
		if isMod(ev.VK) {
			if (before.Alt() && (ev.VK == keymap.VK_LSHIFT || ev.VK == keymap.VK_RSHIFT)) ||
				(before.Shift() && (ev.VK == keymap.VK_LMENU || ev.VK == keymap.VK_RMENU)) ||
				(before.Ctrl() && (ev.VK == keymap.VK_LSHIFT || ev.VK == keymap.VK_RSHIFT)) ||
				(before.Shift() && (ev.VK == keymap.VK_LCONTROL || ev.VK == keymap.VK_RCONTROL)) {
				a.comboMods = 1
			}
		} else {
			a.comboMods = 0
			if before.Win() && ev.VK == keymap.VK_SPACE {
				a.toggleTracked()
			}
		}
		return
	}
	if isMod(ev.VK) {
		if a.comboMods == 1 {
			a.toggleTracked()
		}
		a.comboMods = 0
		a.consoleStale.Store(true) // раскладку консоли перечитать
	}
}

func (a *App) toggleTracked() {
	if t := a.getTracked(); t != keymap.Unknown {
		a.setTracked(t.Other())
	}
	a.consoleStale.Store(true)
}

func (a *App) rulesNeedTitle(cfg *config.Config) bool {
	for i := range cfg.Apps {
		if cfg.Apps[i].Enabled && cfg.Apps[i].Title != "" {
			return true
		}
	}
	return false
}

func (a *App) resolve(cfg *config.Config, info *win.WindowInfo) config.Effective {
	v := a.cfgVer.Load()
	if v != a.ruleVer {
		a.ruleCache = map[string]config.Effective{}
		a.ruleVer = v
	}
	k := info.Process + "\x00" + info.Class + "\x00" + info.Title
	if r, ok := a.ruleCache[k]; ok {
		return r
	}
	r := cfg.Resolve(info.Process, info.Class, info.Title)
	if len(a.ruleCache) > 300 {
		a.ruleCache = map[string]config.Effective{}
	}
	a.ruleCache[k] = r
	return r
}

// currentLang определяет раскладку активного окна.
func (a *App) currentLang(info *win.WindowInfo, rule *config.Effective) keymap.Lang {
	src := rule.LayoutSource
	if src == "auto" || src == "" {
		if info.IsConsole() {
			src = "console"
		} else {
			src = "focus"
		}
	}
	var l keymap.Lang = keymap.Unknown
	switch src {
	case "tracked":
		if a.getTracked() == keymap.Unknown {
			a.setTracked(win.LangOf(win.KeyboardLayout(info.Thread)))
		}
		return a.getTracked()
	case "console":
		if a.consoleStale.Swap(false) {
			a.consoleAt = time.Time{}
		}
		if info.PID == a.consolePID && time.Since(a.consoleAt) < 400*time.Millisecond && a.consoleL != keymap.Unknown {
			l = a.consoleL
		} else if cl, ok := win.ConsoleLayout(info.PID); ok {
			l = cl
			a.consoleL, a.consoleAt, a.consolePID = cl, time.Now(), info.PID
		}
		if l == keymap.Unknown {
			l = win.LangOf(win.KeyboardLayout(info.Thread))
		}
	default:
		l = win.LangOf(win.KeyboardLayout(info.Thread))
	}
	if l != keymap.Unknown {
		a.setTracked(l)
		keymap.SetPreferredCyrillic(l) // какой кириллицей пользуетесь — на ту и переключаем с английской
	} else {
		l = a.getTracked()
	}
	return l
}

// ---------- Исполнение исправлений (отдельная горутина) ----------

func (a *App) worker() {
	for j := range a.jobs {
		win.Protect("ввод исправления", func() { a.execute(j) })
		a.flushQueued()
	}
}

func (a *App) flushQueued() {
	for {
		a.busyMu.Lock()
		if len(a.jobs) > 0 {
			// Ещё есть задания — нажатия повторим после них.
			a.busyMu.Unlock()
			return
		}
		q := a.queued
		a.queued = nil
		if len(q) == 0 {
			a.busy = false
			a.busyMu.Unlock()
			return
		}
		a.busyMu.Unlock()
		in := make([]win.INPUT, 0, len(q))
		for _, k := range q {
			in = append(in, win.RawInput(k.vk, k.scan, k.flags, k.up, win.MagicReplay))
		}
		win.SendInputs(in)
	}
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func (a *App) execute(j job) {
	r := &j.rule
	for _, act := range j.actions {
		switch act.Kind {
		case engine.ActReplace:
			win.WaitModifiersReleased(700 * time.Millisecond)
			win.Backspaces(act.Backspaces, ms(r.BackspaceDelayMs))
			if r.Inject == "keys" && act.Keys != nil && act.Target != keymap.Unknown {
				a.switchTo(j.info, act.Target)
				time.Sleep(ms(max(r.SwitchDelayMs, 20)))
				win.TypeKeys(act.Keys, ms(r.KeyDelayMs))
			} else {
				win.TypeUnicode(act.Text, ms(r.KeyDelayMs))
				if act.Target != keymap.Unknown {
					a.switchTo(j.info, act.Target)
					time.Sleep(ms(r.SwitchDelayMs))
				}
			}
			win.TapKeys(act.Trail, ms(r.KeyDelayMs))
		case engine.ActSwitchLayout:
			a.switchTo(j.info, act.Target)
			time.Sleep(ms(r.SwitchDelayMs))
		case engine.ActConvertSelection:
			a.convertSelection(j, act.Quiet)
		case engine.ActEnabledChanged:
			a.onEnabledChanged()
		case engine.ActToggleApp:
			a.toggleApp(j.info.Process)
		}
	}
}

func (a *App) switchTo(info win.WindowInfo, l keymap.Lang) {
	if l < keymap.EN || l > keymap.UK {
		return
	}
	a.mu.Lock()
	h := a.hkl[l]
	a.mu.Unlock()
	if h == 0 {
		return
	}
	win.RequestLayout(info, h)
	a.eng.NoteLayoutSwitch(l)
	a.setTracked(l)
	a.consoleStale.Store(true)
}

func (a *App) convertSelection(j job, quiet bool) {
	// Открыть и закрыть буфер обмена нужно из одного системного потока.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	win.WaitModifiersReleased(800 * time.Millisecond)
	hk, err := config.ParseHotkey(j.rule.CopyCombo)
	if err != nil || hk.Empty || hk.VK == 0 {
		hk, _ = config.ParseHotkey("Ctrl+C")
	}
	old, hadOld := win.GetClipboardText(a.hwnd)
	seq := win.ClipboardSeq()
	win.Combo(hk.Ctrl, hk.Alt, hk.Shift, hk.Win, hk.VK)
	deadline := time.Now().Add(500 * time.Millisecond)
	for win.ClipboardSeq() == seq && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	restore := func() {
		if hadOld {
			time.Sleep(100 * time.Millisecond)
			_ = win.SetClipboardText(a.hwnd, old)
		}
	}
	nothing := func() {
		if quiet {
			// Выделения, видимо, не было: ведём себя как раньше — переключаем раскладку.
			if l := a.getTracked(); l != keymap.Unknown {
				a.switchTo(j.info, l.Other())
			}
			return
		}
		a.tray.Balloon("Нечего конвертировать", "Не удалось скопировать выделенный текст. Выделите текст или проверьте «Чем копировать выделение» в правилах приложения.", false)
	}
	if win.ClipboardSeq() == seq {
		nothing()
		return
	}
	text, ok := win.GetClipboardText(a.hwnd)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// VS Code и другие редакторы без выделения копируют всю строку с переводом строки на конце.
	if quiet && strings.Count(text, "\n") == 1 && strings.HasSuffix(text, "\n") {
		ok = false
	}
	if !ok || strings.TrimSpace(text) == "" {
		restore()
		nothing()
		return
	}
	from := keymap.DetectTextLang(text)
	if from == keymap.Unknown {
		from = keymap.EN
	}
	conv := keymap.ConvertText(text, from)
	delay := ms(j.rule.KeyDelayMs)
	for i, line := range strings.Split(conv, "\n") {
		if i > 0 {
			win.TapKeys([]uint16{keymap.VK_RETURN}, delay)
		}
		win.TypeUnicode(line, delay)
	}
	a.switchTo(j.info, from.Other())
	// Снова выделяем результат: повторное нажатие вернёт текст обратно.
	if n := len([]rune(conv)); j.rule.ReselectAfterConvert && !strings.Contains(conv, "\n") && n <= 1000 {
		time.Sleep(ms(j.rule.SwitchDelayMs))
		win.SelectBack(n, delay)
		a.eng.NoteSelection(true)
	}
	restore()
}

func (a *App) onEnabledChanged() {
	cfg := a.cfg.Load().Clone()
	cfg.Enabled = a.eng.Enabled()
	_ = a.applyConfig(cfg, false)
	if cfg.Enabled {
		a.tray.Balloon("Erised Switcher включён", "", false)
	} else {
		a.tray.Balloon("Erised Switcher выключен", "Горячие клавиши продолжают работать.", false)
	}
	win.PostMessage(a.hwnd, wmRefresh, 0, 0)
}

// toggleApp добавляет или убирает правило «выключено» для программы.
func (a *App) toggleApp(process string) {
	if process == "" {
		return
	}
	cfg := a.cfg.Load().Clone()
	off := "off"
	for i := range cfg.Apps {
		ap := &cfg.Apps[i]
		if strings.EqualFold(ap.Process, process) && ap.Mode != nil && *ap.Mode == "off" {
			cfg.Apps = append(cfg.Apps[:i], cfg.Apps[i+1:]...)
			_ = a.applyConfig(cfg, true)
			a.tray.Balloon("Включено для "+process, "", false)
			return
		}
	}
	cfg.Apps = append(cfg.Apps, config.AppRule{Name: "Выключено: " + process, Enabled: true, Process: process, Rule: config.Rule{Mode: &off}})
	_ = a.applyConfig(cfg, true)
	a.tray.Balloon("Выключено для "+process, "Включить обратно — тем же пунктом меню или на вкладке «Приложения».", false)
}

// ---------- Настройки ----------

func (a *App) applyConfig(c *config.Config, save bool) error {
	old := a.cfg.Load()
	if save {
		if err := config.Save(a.cfgPath, c); err != nil {
			return err
		}
		a.mu.Lock()
		a.cfgMod = modTime(a.cfgPath)
		a.mu.Unlock()
	}
	a.cfg.Store(c)
	a.cfgVer.Add(1)
	a.eng.SetConfig(c)
	if c.Enabled != a.eng.Enabled() {
		a.eng.SetEnabled(c.Enabled)
	}
	if c.LayoutEN != old.LayoutEN || c.LayoutRU != old.LayoutRU || c.LayoutUK != old.LayoutUK || c.Languages != old.Languages {
		a.findLayouts()
		go prepareSimilar()
	}
	if c.Autostart != old.Autostart || c.Autostart != win.AutostartEnabled(autostartName) {
		a.applyAutostart(c.Autostart)
	}
	if !c.LogDecisions {
		a.ClearLog()
	}
	win.PostMessage(a.hwnd, wmRefresh, 0, 0)
	return nil
}

func (a *App) applyAutostart(on bool) {
	exe, _ := os.Executable()
	if err := win.SetAutostart(autostartName, exe, on); err != nil {
		log.Printf("автозапуск: %v", err)
	}
}

// Следим за файлами: если их поправили руками, перечитываем.
func (a *App) checkFiles() {
	a.mu.Lock()
	cfgMod, wordsMod, lastSave := a.cfgMod, a.wordsMod, a.lastSave
	a.mu.Unlock()
	if m := modTime(a.cfgPath); !m.Equal(cfgMod) && !m.IsZero() {
		a.mu.Lock()
		a.cfgMod = m
		a.mu.Unlock()
		c, err := config.Load(a.cfgPath)
		if err != nil {
			a.tray.Balloon("Ошибка в config.json", err.Error()+"\nДействуют прежние настройки.", true)
		} else {
			_ = a.applyConfig(c, false)
			a.tray.Balloon("Настройки перечитаны", "", false)
		}
	}
	if m := modTime(a.wordsPath); !m.Equal(wordsMod) && !m.IsZero() {
		a.mu.Lock()
		a.wordsMod = m
		a.mu.Unlock()
		if st, err := words.Load(a.wordsPath); err != nil {
			a.tray.Balloon("Ошибка в words.json", err.Error(), true)
		} else {
			a.store.SetData(st.Snapshot())
			_ = a.SaveWords()
		}
	}
	if time.Since(lastSave) > 10*time.Second {
		a.mu.Lock()
		a.lastSave = time.Now()
		a.mu.Unlock()
		_ = a.SaveWords()
		if a.hist != nil {
			if err := a.hist.SaveIfDirty(); err != nil {
				log.Printf("typing.json: %v", err)
			}
		}
	}
}

// ---------- Трей ----------

func (a *App) refreshIcon(force bool) {
	cfg := a.cfg.Load()
	enabled := a.eng.Enabled()
	info := win.Describe(false)
	text, bg := "--", trayicon.GrayOff
	l := keymap.Unknown
	if info.Hwnd != 0 && info.Hwnd != a.hwnd {
		rule := cfg.Resolve(info.Process, info.Class, "")
		if info.IsConsole() || rule.LayoutSource == "console" {
			// Не дёргаем AttachConsole 4 раза в секунду: берём то, что знает хук.
			l = a.getTracked()
		} else {
			l = win.LangOf(win.KeyboardLayout(info.Thread))
		}
	} else {
		l = a.getTracked()
	}
	if cfg.TrayShowsLayout {
		switch l {
		case keymap.EN:
			text, bg = "EN", trayicon.BlueEN
		case keymap.RU:
			text, bg = "RU", trayicon.RedRU
		case keymap.UK:
			text, bg = "UA", trayicon.Amber
		}
	} else {
		text, bg = "EN", trayicon.BlueEN
	}
	crossed := !enabled
	if !enabled {
		bg = trayicon.GrayOff
	}
	size := win.SmallIconSize()
	key := fmt.Sprintf("%s|%v|%v|%d", text, bg, crossed, size)
	if key == a.iconKey && !force {
		return
	}
	a.iconKey = key
	h := win.IconFromBGRA(size, trayicon.Render(size, text, bg, trayicon.White, crossed))
	tip := AppName
	if !enabled {
		tip += " (выключено)"
	}
	a.tray.Set(h, tip)
	win.DestroyIcon(a.icon)
	a.icon = h
}

func (a *App) wndProc(hwnd uintptr, msg uint32, w, l uintptr) (ret uintptr) {
	if win.Protect("окно программы", func() { ret = a.wndProcInner(hwnd, msg, w, l) }) {
		return win.DefWindowProc(hwnd, msg, w, l)
	}
	return ret
}

func (a *App) wndProcInner(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmTray:
		switch uint32(l) {
		case win.WM_LBUTTONUP:
			a.openUI()
		case win.WM_RBUTTONUP, win.WM_CONTEXTMENU:
			a.showMenu()
		}
		return 0
	case wmShowUI:
		a.openUI()
		return 0
	case wmRefresh:
		a.refreshIcon(true)
		return 0
	case win.WM_TIMER:
		switch w {
		case timerIcon:
			a.refreshIcon(false)
			a.checkPopup()
		case timerFiles:
			a.checkFiles()
		case timerRehook:
			// Windows молча снимает хук, если он однажды ответил слишком медленно. Переустанавливаем.
			a.busyMu.Lock()
			idle := !a.busy
			a.busyMu.Unlock()
			if idle {
				a.hooks.Uninstall()
				if err := a.hooks.Install(); err != nil {
					log.Printf("переустановка хука: %v", err)
				}
			}
		}
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage()
		return 0
	}
	if msg == a.taskbarCreated && msg != 0 {
		a.tray.Readd()
		return 0
	}
	return win.DefWindowProc(hwnd, msg, w, l)
}

func (a *App) openUI() {
	if a.ui == nil {
		win.ShellOpen(a.dir)
		return
	}
	url := a.ui.URL()
	if a.cfg.Load().UIMode == "browser" {
		win.ShellOpen(url)
		return
	}
	// Окно уже открыто — показываем его.
	if h := win.FindWindowByTitle(settingswin.ClassName, settingswin.Title); h != 0 {
		win.BringToFront(h)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		openAppWindow(url)
		return
	}
	cmd := exec.Command(exe, "--settings")
	// Адрес с ключом передаём через окружение, а не командной строкой.
	cmd.Env = append(os.Environ(), "ERISED_UI_URL="+url, "ERISED_DATA_DIR="+a.dir)
	if err := cmd.Start(); err != nil {
		log.Printf("окно настроек: %v", err)
		openAppWindow(url)
		return
	}
	win.AllowForeground(cmd.Process.Pid)
	started := time.Now()
	go func() {
		err := cmd.Wait()
		// Окно не смогло открыться (нет WebView2 или он сломан) — запасной вариант.
		if err != nil && time.Since(started) < 30*time.Second {
			log.Printf("окно настроек завершилось с ошибкой (%v), открываю запасной вариант", err)
			openAppWindow(url)
		}
	}()
}

// openAppWindow открывает настройки в отдельном окне Edge без адресной строки,
// а если Edge нет — в браузере по умолчанию.
func openAppWindow(url string) {
	if win.ShellRun("msedge.exe", "--app="+url+" --window-size=1140,820") {
		return
	}
	win.ShellOpen(url)
}

func (a *App) showMenu() {
	cfg := a.cfg.Load()
	a.prevAppMu.Lock()
	prev := a.prevApp
	a.prevAppMu.Unlock()
	appOff := false
	if prev != "" {
		appOff = cfg.Resolve(prev, "", "").Mode == "off"
	}
	items := []win.MenuItem{
		{ID: miSettings, Text: "Настройки…"},
		{},
		{ID: miEnabled, Text: "Исправлять раскладку", Checked: a.eng.Enabled()},
	}
	if prev != "" {
		items = append(items, win.MenuItem{ID: miToggleApp, Text: "Выключить в " + prev, Checked: appOff})
	}
	items = append(items,
		win.MenuItem{ID: miAutostart, Text: "Запускать вместе с Windows", Checked: cfg.Autostart},
		win.MenuItem{},
		win.MenuItem{ID: miOpenDir, Text: "Открыть папку с настройками"},
		win.MenuItem{ID: miReload, Text: "Перечитать настройки и словарь"},
	)
	if !win.IsAdmin() {
		items = append(items, win.MenuItem{ID: miAdmin, Text: "Перезапустить от администратора (для окон администратора)"})
	}
	if v := a.upd.Available(); v != "" {
		items = append(items, win.MenuItem{ID: miUpdate, Text: "Обновить до версии " + v})
	} else {
		items = append(items, win.MenuItem{ID: miUpdate, Text: "Проверить обновления"})
	}
	items = append(items, win.MenuItem{ID: miAbout, Text: "О программе"}, win.MenuItem{}, win.MenuItem{ID: miExit, Text: "Выход"})
	switch win.PopupMenu(a.hwnd, items) {
	case miSettings:
		a.openUI()
	case miEnabled:
		c := cfg.Clone()
		c.Enabled = !a.eng.Enabled()
		a.eng.SetEnabled(c.Enabled)
		_ = a.applyConfig(c, true)
	case miToggleApp:
		go a.toggleApp(prev)
	case miAutostart:
		c := cfg.Clone()
		c.Autostart = !c.Autostart
		_ = a.applyConfig(c, true)
	case miOpenDir:
		win.ShellOpen(a.dir)
	case miReload:
		_ = a.Action("reload")
	case miAdmin:
		exe, _ := os.Executable()
		a.hooks.Uninstall()
		a.tray.Remove()
		if win.RunAsAdmin(exe, "--quiet") {
			_ = a.store.SaveIfDirty()
			os.Exit(0)
		}
		_ = a.hooks.Install()
		a.refreshIcon(true)
	case miUpdate:
		if a.upd.Available() != "" {
			_ = a.upd.Action("install")
		} else {
			go a.upd.check(true)
		}
	case miAbout:
		win.MessageBox(AppName+" "+Version, mirrorInscription+"\n\n"+
			"Как и зеркало, показывает не то, что вы набрали, а то, что вы хотели написать.\n"+
			"Автоматическое исправление раскладки RU/EN, в том числе в терминалах.\n\n"+
			"Данные: "+a.dir+"\nРаскладки: "+a.layoutsText()+"\n\nСловари: FrequencyWords (Hermit Dave, MIT).", 0x40)
	case miExit:
		win.DestroyWindow(a.hwnd)
	}
}

// ---------- Журнал и недавние программы ----------

func (a *App) addLog(e engine.LogEntry) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	n := a.cfg.Load().LogSize
	if n <= 0 {
		n = 200
	}
	a.logs = append(a.logs, e)
	if len(a.logs) > n {
		a.logs = a.logs[len(a.logs)-n:]
	}
}

func (a *App) noteRecent(p string) {
	if p == "" {
		return
	}
	a.recentMu.Lock()
	a.recent[p] = time.Now()
	a.recentMu.Unlock()
}

// ---------- webui.Host ----------

// Config — текущие настройки.
func (a *App) Config() *config.Config { return a.cfg.Load() }

// ApplyConfig — сохранить и применить настройки из окна.
func (a *App) ApplyConfig(c *config.Config) error { return a.applyConfig(c, true) }

// Words — хранилище слов.
func (a *App) Words() *words.Store { return a.store }

// SaveWords — сохранить словари.
func (a *App) SaveWords() error {
	a.mu.Lock() // заодно не даём двум сохранениям идти одновременно
	defer a.mu.Unlock()
	err := a.store.SaveIfDirty()
	a.wordsMod = modTime(a.wordsPath)
	return err
}

// Status — сведения для окна настроек.
func (a *App) Status() map[string]any {
	var dict []string
	for _, l := range keymap.Languages() {
		dict = append(dict, fmt.Sprintf("%d %s", lang.Get(l).Size(), strings.ToUpper(l.String())))
	}
	return map[string]any{
		"version":  Version,
		"enabled":  a.eng.Enabled(),
		"admin":    win.IsAdmin(),
		"layouts":  a.layoutsText(),
		"dict":     strings.Join(dict, ", ") + " слов",
		"data_dir": a.dir,
	}
}

// Log — журнал решений.
func (a *App) Log() []engine.LogEntry {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	return append([]engine.LogEntry(nil), a.logs...)
}

// ClearLog очищает журнал.
func (a *App) ClearLog() {
	a.logMu.Lock()
	a.logs = nil
	a.logMu.Unlock()
}

// RecentApps — программы, в которых недавно печатали.
func (a *App) RecentApps() []string {
	a.recentMu.Lock()
	defer a.recentMu.Unlock()
	out := make([]string, 0, len(a.recent))
	for p := range a.recent {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return a.recent[out[i]].After(a.recent[out[j]]) })
	return out
}

// Action — кнопки в окне настроек.
func (a *App) Action(name string) error {
	switch name {
	case "enable", "disable":
		c := a.cfg.Load().Clone()
		c.Enabled = name == "enable"
		a.eng.SetEnabled(c.Enabled)
		return a.applyConfig(c, true)
	case "open_dir":
		win.ShellOpen(a.dir)
	case "reload":
		a.findLayouts()
		a.reloadFiles()
		win.PostMessage(a.hwnd, wmRefresh, 0, 0)
	default:
		return fmt.Errorf("неизвестное действие %q", name)
	}
	return nil
}

func (a *App) reloadFiles() {
	c, err := config.Load(a.cfgPath)
	if err != nil {
		a.tray.Balloon("Ошибка в config.json", err.Error(), true)
	} else {
		_ = a.applyConfig(c, false)
	}
	a.mu.Lock()
	a.cfgMod = modTime(a.cfgPath)
	a.mu.Unlock()
	if st, err := words.Load(a.wordsPath); err == nil {
		a.store.SetData(st.Snapshot())
		_ = a.SaveWords()
	} else {
		a.tray.Balloon("Ошибка в words.json", err.Error(), true)
	}
}

// UpdateStatus — состояние обновлений для окна настроек.
func (a *App) UpdateStatus() map[string]any { return a.upd.Status() }

// UpdateAction — проверить / установить / пропустить версию.
func (a *App) UpdateAction(op string) error { return a.upd.Action(op) }
