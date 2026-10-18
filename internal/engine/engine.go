// Package engine — платформонезависимое ядро переключателя. Оно получает нажатия
// клавиш (виртуальные коды + состояние Shift/CapsLock) и решает, что сделать:
// пропустить, исправить слово, отменить исправление, выполнить горячую клавишу.
// Ядро ничего не читает из окна приложения — поэтому работает и в терминалах.
package engine

import (
	"sync"
	"time"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
	"erised/internal/words"
)

// Mods — состояние модификаторов в момент события.
type Mods struct {
	LShift, RShift, LCtrl, RCtrl, LAlt, RAlt, LWin, RWin bool
}

func (m Mods) Shift() bool { return m.LShift || m.RShift }
func (m Mods) Ctrl() bool  { return m.LCtrl || m.RCtrl }
func (m Mods) Alt() bool   { return m.LAlt || m.RAlt }
func (m Mods) Win() bool   { return m.LWin || m.RWin }

// KeyEvent — нажатие или отпускание клавиши.
type KeyEvent struct {
	VK   uint16
	Down bool
	Mods Mods   // модификаторы, нажатые до этого события
	Caps bool   // CapsLock включён
	Time uint32 // мс (время события из системы)
}

// Context — окно, в которое идёт ввод.
type Context struct {
	Window   uintptr
	App      string // имя процесса
	Class    string
	Title    string
	Layout   keymap.Lang // текущая раскладка (Unknown — не удалось определить)
	Password bool
	Rule     config.Effective
}

// ActionKind — что нужно сделать платформенному слою.
type ActionKind int

const (
	// ActReplace — стереть Backspaces символов, (переключить раскладку), напечатать текст, затем Trail.
	ActReplace ActionKind = iota
	// ActSwitchLayout — только переключить раскладку.
	ActSwitchLayout
	// ActConvertSelection — сконвертировать выделенный текст через буфер обмена.
	ActConvertSelection
	// ActEnabledChanged — включено/выключено (обновить значок).
	ActEnabledChanged
	// ActToggleApp — включить/выключить программу для текущего приложения.
	ActToggleApp
)

// Action — команда для платформенного слоя.
type Action struct {
	Kind       ActionKind
	Backspaces int
	Text       string       // что напечатать (режим unicode)
	Keys       []keymap.Key // что нажать после переключения (режим keys); nil — только unicode
	Target     keymap.Lang  // на какую раскладку переключить (Unknown — не переключать)
	Trail      []uint16     // клавиши, которые надо нажать в конце (пробел/Enter/Tab)
	Reason     string
	// Для ActConvertSelection: выделение только предполагается (сработало «умное» действие
	// конвертации слова). Если скопировать нечего — молча переключить раскладку.
	Quiet bool
}

// Result — ответ ядра на событие.
type Result struct {
	Swallow bool // не пропускать это нажатие в приложение
	// Варианты в полоске подсказок изменились (см. Engine.Suggestions).
	SuggestChanged bool
	Actions        []Action
}

// LogEntry — запись журнала решений.
type LogEntry struct {
	Time  time.Time `json:"time"`
	App   string    `json:"app"`
	Rule  string    `json:"rule"`
	Event string    `json:"event"`
	Lang  string    `json:"lang"`
	V     Verdict   `json:"v"`
}

type token struct {
	keys      []keymap.Key
	lang      keymap.Lang
	trail     uint16 // клавиша-разделитель после слова (0 — нет)
	corrected bool   // слово уже исправлено автоматически
	mixed     bool   // раскладка менялась посреди слова
}

type correction struct {
	origKeys  []keymap.Key // нажатия исходного текста (включая пробелы для фразы)
	origLangs []keymap.Lang
	origLang  keymap.Lang
	newLang   keymap.Lang
	newLen    int
	trail     uint16
	auto      bool
	manual    bool
	learned   string // слово, которое было собрано этим действием (для отката)
	learnedL  keymap.Lang
	histIdx   int // индекс токена в истории (-1 — текущий)
}

// Engine — состояние ядра. Методы безопасны для вызова из одного потока хука;
// SetConfig/SetEnabled можно звать из других потоков.
type Engine struct {
	mu      sync.Mutex
	cfg     *config.Config
	hk      hotkeys
	store   *words.Store
	enabled bool
	logf    func(LogEntry)

	window uintptr
	pred   *predict.Predictor
	sv     SuggestionView
	sk     suggestKeys
	// Похоже, что в окне сейчас выделен текст (Shift+стрелки, Ctrl+A, протяжка или
	// двойной щелчок мышью). Тогда конвертация слова конвертирует выделение.
	selection  bool
	selByMouse bool // выделение сделано мышью (оно относится к окну, по которому щёлкнули)
	cur        token
	hist       []token
	last       *correction
	fresh      bool // после последнего действия не было нажатий
	swallowUp  map[uint16]bool

	// Нажатия модификаторов «сами по себе» (для DoubleShift и одиночного RCtrl).
	tapVK     uint16
	tapClean  bool
	lastTap   config.TapKey
	lastTapT  uint32
	lastTapVK uint16

	// Раскладка, на которую мы переключили сами (пока система не подтвердила).
	forced     keymap.Lang
	forcedTime time.Time
	now        func() time.Time
}

// New создаёт ядро.
func New(cfg *config.Config, st *words.Store) *Engine {
	e := &Engine{store: st, enabled: cfg.Enabled, swallowUp: map[uint16]bool{}, now: time.Now, forced: keymap.Unknown}
	e.SetConfig(cfg)
	lang.Preload()
	return e
}

// SetConfig применяет новые настройки.
func (e *Engine) SetConfig(cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
	e.hk = parseHotkeys(cfg)
	e.sk = parseSuggestKeys(cfg)
}

// SetLogger задаёт обработчик журнала решений.
func (e *Engine) SetLogger(f func(LogEntry)) { e.mu.Lock(); e.logf = f; e.mu.Unlock() }

// Enabled — включена ли автоматика.
func (e *Engine) Enabled() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.enabled }

// SetEnabled включает/выключает.
func (e *Engine) SetEnabled(v bool) { e.mu.Lock(); e.enabled = v; e.resetAll(); e.mu.Unlock() }

// Reset сбрасывает буфер (щелчок мышью, смена окна и т.п.) и прячет подсказки.
func (e *Engine) Reset() { e.mu.Lock(); e.resetAll(); e.clearSuggestions(); e.mu.Unlock() }

// NoteSelection сообщает, что текст, вероятно, выделен (или выделение снято).
// Вызывается для действий мышью.
func (e *Engine) NoteSelection(v bool) {
	e.mu.Lock()
	e.selection, e.selByMouse = v, v
	e.mu.Unlock()
}

// Selection — предполагается ли выделение.
func (e *Engine) Selection() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.selection }

func isNavKey(vk uint16) bool {
	return vk >= keymap.VK_PRIOR && vk <= keymap.VK_DOWN // PgUp PgDn End Home стрелки
}

// trackSelection угадывает, есть ли выделение, по нажатой клавише.
func (e *Engine) trackSelection(ev KeyEvent) {
	m := ev.Mods
	e.selByMouse = false
	switch {
	case m.Ctrl() && !m.Alt() && ev.VK == 'A':
		e.selection = true
	case m.Shift() && !m.Alt() && isNavKey(ev.VK):
		e.selection = true
	case m.Ctrl() && (ev.VK == 'C' || ev.VK == keymap.VK_INSERT):
		// копирование выделение не снимает
	case ev.VK == keymap.VK_CAPITAL || ev.VK == 0x90 || ev.VK == keymap.VK_SCROLL || ev.VK == keymap.VK_PAUSE || ev.VK == 0x03:
	case ev.VK >= keymap.VK_F1 && ev.VK <= keymap.VK_F24:
	default:
		e.selection = false // набор, Backspace, Enter, стрелки без Shift и т.п. снимают выделение
	}
}

func (e *Engine) resetAll() {
	e.cur = token{lang: keymap.Unknown}
	e.hist = nil
	e.last = nil
	e.fresh = false
}

func (e *Engine) resetWord() {
	e.cur = token{lang: keymap.Unknown}
}

// NoteLayoutSwitch сообщает ядру, что раскладку переключили мы сами.
func (e *Engine) NoteLayoutSwitch(l keymap.Lang) {
	e.mu.Lock()
	e.forced, e.forcedTime = l, e.now()
	e.mu.Unlock()
}

func (e *Engine) effectiveLayout(ctx *Context) keymap.Lang {
	if e.forced != keymap.Unknown {
		if ctx.Layout == e.forced || e.now().Sub(e.forcedTime) > 1500*time.Millisecond {
			e.forced = keymap.Unknown
		} else {
			return e.forced
		}
	}
	return ctx.Layout
}

func isModifier(vk uint16) bool {
	switch vk {
	case keymap.VK_SHIFT, keymap.VK_LSHIFT, keymap.VK_RSHIFT,
		keymap.VK_CONTROL, keymap.VK_LCONTROL, keymap.VK_RCONTROL,
		keymap.VK_MENU, keymap.VK_LMENU, keymap.VK_RMENU,
		keymap.VK_LWIN, keymap.VK_RWIN:
		return true
	}
	return false
}

// HandleKey — главная точка входа.
func (e *Engine) HandleKey(ev KeyEvent, ctx *Context) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !lang.Ready() {
		return Result{} // словари ещё загружаются (первую секунду после запуска) — не ждём
	}
	ver := e.sv.Version
	// Пока видна полоска подсказок, Tab / Ctrl+1… принимают вариант.
	if res, ok := e.acceptByKey(ev, ctx); ok {
		res.SuggestChanged = e.sv.Version != ver
		return res
	}
	res := e.handleKey(ev, ctx)
	if (ev.Down && !isModifier(ev.VK)) || len(res.Actions) > 0 {
		e.updateSuggestions(ctx)
	}
	res.SuggestChanged = e.sv.Version != ver
	return res
}

func (e *Engine) handleKey(ev KeyEvent, ctx *Context) Result {
	if ctx.Window != e.window {
		e.window = ctx.Window
		e.resetAll()
		// Щелчок мышью обычно и переключает окно, так что выделение мышью сохраняем.
		if !e.selByMouse {
			e.selection = false
		}
	}

	if !ev.Down {
		if isModifier(ev.VK) {
			return e.modifierUp(ev, ctx)
		}
		if e.swallowUp[ev.VK] {
			delete(e.swallowUp, ev.VK)
			return Result{Swallow: true}
		}
		return Result{}
	}

	if isModifier(ev.VK) {
		if e.tapVK != ev.VK { // автоповтор модификатора не сбрасывает «чистоту»
			e.tapVK, e.tapClean = ev.VK, !anyModExcept(ev.Mods, ev.VK)
		}
		return Result{}
	}
	e.tapVK, e.tapClean = 0, false
	e.lastTap = config.TapNone

	// Горячие клавиши-комбинации.
	act, isHotkey := e.hk.match(ev)
	if !isHotkey {
		e.trackSelection(ev)
	} else {
		res := e.runHotkey(act, ctx)
		if res.Swallow {
			e.swallowUp[ev.VK] = true
			if ev.VK == 0x03 { // Ctrl+Pause приходит как VK_CANCEL
				e.swallowUp[keymap.VK_PAUSE] = true
			}
		}
		return res
	}

	if !e.enabled || ctx.Rule.Mode == "off" || (ctx.Password && e.cfg.SkipPasswords) {
		e.resetAll()
		return Result{}
	}

	if ev.VK == keymap.VK_CAPITAL || ev.VK == 0x90 /*NumLock*/ || ev.VK == keymap.VK_SCROLL {
		return Result{}
	}

	if ev.Mods.Ctrl() || ev.Mods.Alt() || ev.Mods.Win() {
		e.resetAll()
		return Result{}
	}

	layout := e.effectiveLayout(ctx)
	r := &ctx.Rule

	if ev.VK == keymap.VK_BACK {
		return e.backspace(ctx)
	}

	e.fresh = false

	switch ev.VK {
	case keymap.VK_SPACE, keymap.VK_RETURN, keymap.VK_TAB:
		check := (ev.VK == keymap.VK_SPACE && r.CorrectOnSpace) ||
			(ev.VK == keymap.VK_RETURN && r.CorrectOnEnter) ||
			(ev.VK == keymap.VK_TAB && r.CorrectOnTab)
		return e.boundary(ev.VK, check, layout, ctx)
	}

	if !keymap.IsPrintable(ev.VK) {
		// Стрелки, Home/End, Delete, F-клавиши, цифровой блок и прочее — текст «потерян».
		e.resetAll()
		return Result{}
	}

	k := keymap.Key{VK: ev.VK, Shift: ev.Mods.Shift(), Caps: ev.Caps}
	if len(e.cur.keys) == 0 {
		e.cur = token{lang: layout}
	} else if layout != e.cur.lang && !e.cur.corrected {
		e.cur.mixed = true
	}
	// Посреди слова ничего, кроме исправления «на лету», не запоминаем как отменяемое.
	if e.last != nil && e.last.histIdx != -1 {
		e.last = nil
	}
	if r.Mode == "auto" && r.EarlySwitch && !e.cur.corrected && !e.cur.mixed &&
		e.cur.lang != keymap.Unknown && len(e.cur.keys)+1 >= r.EarlyMinLen {
		keys := append(append([]keymap.Key(nil), e.cur.keys...), k)
		v := Decide(keys, e.cur.lang, r, e.store, false, keymap.Unknown)
		e.log("на лету", ctx, v)
		if v.Switch {
			alt := v.AltLang
			c := &correction{origKeys: keys, origLang: e.cur.lang, newLang: alt, newLen: len(keys), auto: true, histIdx: -1}
			act := Action{Kind: ActReplace, Backspaces: len(e.cur.keys), Text: keymap.RenderAll(keys, alt),
				Keys: keys, Target: e.targetFor(r, alt), Reason: v.Reason}
			e.cur.keys, e.cur.lang, e.cur.corrected = keys, alt, true
			e.last, e.fresh = c, true
			return Result{Swallow: true, Actions: []Action{act}}
		}
	}
	e.cur.keys = append(e.cur.keys, k)
	if len(e.cur.keys) > 64 { // это уже не слово
		e.cur.mixed = true
	}
	return Result{}
}

func (e *Engine) targetFor(r *config.Effective, l keymap.Lang) keymap.Lang {
	if r.SwitchLayout {
		return l
	}
	return keymap.Unknown
}

func anyModExcept(m Mods, vk uint16) bool {
	switch vk {
	case keymap.VK_LSHIFT, keymap.VK_SHIFT:
		m.LShift = false
	case keymap.VK_RSHIFT:
		m.RShift = false
	case keymap.VK_LCONTROL, keymap.VK_CONTROL:
		m.LCtrl = false
	case keymap.VK_RCONTROL:
		m.RCtrl = false
	case keymap.VK_LMENU, keymap.VK_MENU:
		m.LAlt = false
	case keymap.VK_RMENU:
		m.RAlt = false
	case keymap.VK_LWIN:
		m.LWin = false
	case keymap.VK_RWIN:
		m.RWin = false
	}
	return m.LShift || m.RShift || m.LCtrl || m.RCtrl || m.LAlt || m.RAlt || m.LWin || m.RWin
}

func tapOf(vk uint16) []config.TapKey {
	switch vk {
	case keymap.VK_LSHIFT, keymap.VK_SHIFT:
		return []config.TapKey{config.TapLShift, config.TapShift}
	case keymap.VK_RSHIFT:
		return []config.TapKey{config.TapRShift, config.TapShift}
	case keymap.VK_LCONTROL, keymap.VK_CONTROL:
		return []config.TapKey{config.TapLCtrl, config.TapCtrl}
	case keymap.VK_RCONTROL:
		return []config.TapKey{config.TapRCtrl, config.TapCtrl}
	case keymap.VK_LMENU, keymap.VK_MENU:
		return []config.TapKey{config.TapLAlt, config.TapAlt}
	case keymap.VK_RMENU:
		return []config.TapKey{config.TapRAlt, config.TapAlt}
	case keymap.VK_LWIN, keymap.VK_RWIN:
		return []config.TapKey{config.TapWin}
	}
	return nil
}

func (e *Engine) modifierUp(ev KeyEvent, ctx *Context) Result {
	clean := e.tapVK == ev.VK && e.tapClean
	e.tapVK, e.tapClean = 0, false
	if !clean {
		e.lastTap = config.TapNone
		return Result{}
	}
	taps := tapOf(ev.VK)
	// Двойное нажатие: тот же физический модификатор дважды в пределах окна.
	double := e.lastTapVK == ev.VK && e.lastTap != config.TapNone && ev.Time-e.lastTapT <= uint32(e.cfg.DoubleTapMs)
	if double {
		e.lastTap = config.TapNone
		for _, t := range taps {
			if act, ok := e.hk.double[t]; ok {
				res := e.runHotkey(act, ctx)
				res.Swallow = false // отпускание модификатора глотать нельзя
				return res
			}
		}
		return Result{}
	}
	e.lastTap, e.lastTapT, e.lastTapVK = taps[0], ev.Time, ev.VK
	for _, t := range taps {
		if act, ok := e.hk.single[t]; ok {
			res := e.runHotkey(act, ctx)
			res.Swallow = false
			return res
		}
	}
	return Result{}
}

func (e *Engine) log(event string, ctx *Context, v Verdict) {
	if e.logf == nil || !e.cfg.LogDecisions {
		return
	}
	e.logf(LogEntry{Time: e.now(), App: ctx.App, Rule: ctx.Rule.RuleName, Event: event, Lang: v.CurLang.String(), V: v})
}

func (e *Engine) pushHist(t token) {
	e.hist = append(e.hist, t)
	if len(e.hist) > 40 {
		e.hist = e.hist[len(e.hist)-40:]
		if e.last != nil && e.last.histIdx >= 0 {
			e.last = nil
		}
	}
}

// boundary — пробел, Enter или Tab.
func (e *Engine) boundary(vk uint16, check bool, layout keymap.Lang, ctx *Context) Result {
	r := &ctx.Rule
	t := e.cur
	e.resetWord()
	e.last = nil
	newLine := vk != keymap.VK_SPACE
	finish := func(res Result) Result {
		if newLine {
			e.hist = nil
			e.last = nil
		}
		return res
	}
	if len(t.keys) == 0 {
		if newLine {
			e.hist = nil
		} else if len(e.hist) > 0 {
			// Несколько пробелов подряд: разделитель фразы — пробел, и он учтён у предыдущего слова.
			// Второй пробел разрывает связь (чтобы конвертация фразы не стирала лишнее).
			e.hist = nil
		}
		return finish(Result{})
	}
	t.trail = vk
	if !check || t.mixed || t.corrected || t.lang == keymap.Unknown {
		e.learnWord(&t, ctx)
		e.pushHist(t)
		return finish(Result{})
	}
	visible := keymap.RenderAll(t.keys, t.lang)

	// Автозамена.
	if e.store != nil {
		if to, ok := e.store.AutoReplace(visible); ok {
			e.pushHist(token{lang: t.lang}) // заменённый текст больше не конвертируем
			return finish(Result{Swallow: true, Actions: []Action{{Kind: ActReplace, Backspaces: len(t.keys), Text: to,
				Target: keymap.Unknown, Trail: []uint16{vk}, Reason: "автозамена"}}})
		}
	}
	if r.SkipFirstWord && len(e.hist) == 0 {
		e.learnWord(&t, ctx)
		e.pushHist(t)
		return finish(Result{})
	}
	if r.Mode != "auto" {
		return finish(e.finishWord(t, vk, newLine, ctx))
	}

	prevLang := keymap.Unknown
	if n := len(e.hist); n > 0 && e.hist[n-1].trail == keymap.VK_SPACE && len(e.hist[n-1].keys) > 0 {
		prevLang = e.hist[n-1].lang
	}
	v := Decide(t.keys, t.lang, r, e.store, true, prevLang)
	e.log("конец слова", ctx, v)
	if v.Switch {
		alt := v.AltLang
		c := &correction{origKeys: t.keys, origLang: t.lang, newLang: alt, newLen: len(t.keys), trail: vk, auto: true, histIdx: len(e.hist)}
		act := Action{Kind: ActReplace, Backspaces: len(t.keys), Text: keymap.RenderAll(t.keys, alt), Keys: t.keys,
			Target: e.targetFor(r, alt), Trail: []uint16{vk}, Reason: v.Reason}
		t.lang, t.corrected = alt, true
		e.learnWord(&t, ctx)
		e.pushHist(t)
		if !newLine {
			e.last, e.fresh = c, true
		}
		return finish(Result{Swallow: true, Actions: []Action{act}})
	}

	return finish(e.finishWord(t, vk, newLine, ctx))
}

// finishWord — раскладка верная: исправляем опечатку (если надо), учимся и запоминаем слово.
func (e *Engine) finishWord(t token, vk uint16, newLine bool, ctx *Context) Result {
	if act, c, ok := e.autocorrect(&t, vk, ctx); ok {
		e.learnWord(&t, ctx)
		e.pushHist(t)
		if !newLine {
			e.last, e.fresh = c, true
		}
		return Result{Swallow: true, Actions: []Action{act}}
	}
	// Слово оставили как есть — возможно, его стоит запомнить.
	e.learnPassive(t, ctx)
	e.learnWord(&t, ctx)
	e.pushHist(t)
	return Result{}
}

func (e *Engine) learnPassive(t token, ctx *Context) {
	l := &e.cfg.Learn
	if e.store == nil || !l.Enabled || !ctx.Rule.LearnPassive {
		return
	}
	for _, k := range t.keys {
		if keymap.IsDigit(k.VK) {
			return
		}
	}
	core, ok := lang.Core(keymap.RenderAll(t.keys, t.lang), t.lang)
	if !ok || len([]rune(core)) < l.PassiveMinLen {
		return
	}
	if _, in := lang.Get(t.lang).Zipf(core); in {
		return
	}
	e.store.Observe(words.KindPassive, t.lang, core, ctx.App, l.PassiveAfter, l.Review, l.MaxCandidates)
}

func (e *Engine) backspace(ctx *Context) Result {
	if e.fresh && e.last != nil && e.last.auto && ctx.Rule.UndoByBackspace {
		return e.undo(ctx, true)
	}
	e.fresh = false
	e.last = nil
	if n := len(e.cur.keys); n > 0 {
		e.cur.keys = e.cur.keys[:n-1]
		if n == 1 {
			e.resetWord()
		}
		return Result{}
	}
	if n := len(e.hist); n > 0 {
		// Стёрли разделитель — снова редактируем предыдущее слово.
		t := e.hist[n-1]
		e.hist = e.hist[:n-1]
		t.trail = 0
		e.cur = t
		return Result{}
	}
	return Result{}
}

// undo отменяет последнее исправление: возвращает исходный текст и раскладку.
func (e *Engine) undo(ctx *Context, byBackspace bool) Result {
	c := e.last
	e.last, e.fresh = nil, false
	trail := []uint16{}
	n := c.newLen
	if c.trail != 0 {
		n++
		trail = append(trail, c.trail)
	}
	var text string
	if len(c.origLangs) > 0 {
		text = renderPhrase(c.origKeys, c.origLangs)
	} else {
		text = keymap.RenderAll(c.origKeys, c.origLang)
	}
	act := Action{Kind: ActReplace, Backspaces: n, Text: text, Keys: c.origKeys, Target: e.targetFor(&ctx.Rule, c.origLang),
		Trail: trail, Reason: "отмена исправления"}
	if c.newLang == c.origLang { // отмена исправления опечатки: раскладку не трогаем
		act.Target = keymap.Unknown
	}
	if len(c.origLangs) > 0 {
		act.Keys = nil
	}
	// Состояние буфера.
	if c.histIdx == -1 {
		e.cur.lang, e.cur.corrected = c.origLang, true // не исправлять это слово повторно
		e.cur.keys = c.origKeys
	} else if c.histIdx >= 0 && c.histIdx < len(e.hist) {
		e.hist[c.histIdx].lang = c.origLang
		e.hist[c.histIdx].corrected = true
	} else if c.histIdx == -2 {
		for i := range e.hist {
			e.hist[i].lang = e.hist[i].lang.Other()
		}
		if len(e.cur.keys) > 0 {
			e.cur.lang = e.cur.lang.Other()
		}
	}
	// Сбор исключений: вы отменили автоисправление — значит, исходное слово правильное.
	if c.auto {
		if core, ok := lang.Core(keymap.RenderAll(c.origKeys, c.origLang), c.origLang); ok && e.store != nil && e.cfg.Learn.Enabled {
			l := &e.cfg.Learn
			if c.histIdx == -1 {
				// Отмена «на лету» — слово ещё не дописано; запоминать нечего.
			} else {
				e.store.Observe(words.KindException, c.origLang, core, ctx.App, l.ExceptionAfter, l.Review, l.MaxCandidates)
			}
		}
	}
	if c.learned != "" && e.store != nil {
		e.store.Unlearn(c.learnedL, c.learned)
	}
	e.log("отмена", ctx, Verdict{CurLang: c.newLang, Cur: keymap.RenderAll(c.origKeys, c.newLang), Alt: text, Reason: "отмена"})
	return Result{Swallow: byBackspace, Actions: []Action{act}}
}

func renderPhrase(keys []keymap.Key, langs []keymap.Lang) string {
	out := make([]rune, 0, len(keys))
	for i, k := range keys {
		if r := keymap.Render(k, langs[i]); r != 0 {
			out = append(out, r)
		}
	}
	return string(out)
}

// convertWord — ручная конвертация последнего слова (двойной Shift).
func (e *Engine) convertWord(ctx *Context) Result {
	if e.fresh && e.last != nil {
		// При трёх языках повторные нажатия перебирают раскладки по кругу, в конце — исходная.
		if c := e.last; c.manual && len(c.origLangs) == 0 && len(keymap.Languages()) > 2 {
			// Раскладки, где слово выглядит так же, пропускаем — иначе нажатие ничего бы не изменило.
			shown := keymap.RenderAll(c.origKeys, c.newLang)
			next := cycleNext(c.origLang, c.newLang)
			for next != c.origLang && keymap.RenderAll(c.origKeys, next) == shown {
				next = cycleNext(c.origLang, next)
			}
			if next != c.origLang {
				return e.recycle(c, next, ctx)
			}
		}
		// Повторное нажатие сразу после исправления — вернуть как было.
		return e.undo(ctx, false)
	}
	if e.selection && len(e.cur.keys) == 0 && ctx.Rule.SelectionAllowed {
		e.resetAll()
		return Result{Actions: []Action{{Kind: ActConvertSelection, Quiet: true, Reason: "конвертация выделения"}}}
	}
	layout := e.effectiveLayout(ctx)
	var t *token
	idx := -1
	if len(e.cur.keys) > 0 {
		t = &e.cur
	} else if n := len(e.hist); n > 0 {
		t, idx = &e.hist[n-1], n-1
	}
	if t == nil {
		// Нечего конвертировать — просто переключим раскладку.
		if layout == keymap.Unknown {
			return Result{}
		}
		return Result{Actions: []Action{{Kind: ActSwitchLayout, Target: layout.Other(), Reason: "нет слова — только переключение"}}}
	}
	from := t.lang
	if from == keymap.Unknown || t.mixed {
		from = layout
	}
	if from == keymap.Unknown {
		from = keymap.EN
	}
	to := from.Other()
	n := len(t.keys)
	var trail []uint16
	if t.trail != 0 {
		n++
		trail = []uint16{t.trail}
	}
	c := &correction{origKeys: t.keys, origLang: from, newLang: to, newLen: len(t.keys), trail: t.trail, manual: true, histIdx: idx}
	act := Action{Kind: ActReplace, Backspaces: n, Text: keymap.RenderAll(t.keys, to), Keys: t.keys, Target: to, Trail: trail, Reason: "ручная конвертация"}

	// Сбор слов.
	if e.store != nil && e.cfg.Learn.Enabled {
		l := &e.cfg.Learn
		if core, ok := lang.Core(keymap.RenderAll(t.keys, from), from); ok {
			e.store.Unlearn(from, core) // ошибочно собранное «слово» больше не считаем словом
		}
		if t.corrected {
			// Слово было исправлено автоматически, а вы вернули его — это исключение
			// (только для дописанного слова).
			if core, ok := lang.Core(keymap.RenderAll(t.keys, to), to); ok && idx >= 0 {
				e.store.Observe(words.KindException, to, core, ctx.App, l.ExceptionAfter, l.Review, l.MaxCandidates)
			}
		} else if core, ok := lang.Core(keymap.RenderAll(t.keys, to), to); ok {
			if _, in := lang.Get(to).Zipf(core); !in && len([]rune(core)) >= 2 {
				if e.store.Observe(words.KindManual, to, core, ctx.App, l.WordAfter, l.Review, l.MaxCandidates) {
					c.learned, c.learnedL = core, to
				}
			}
		}
	}
	t.lang, t.mixed, t.corrected = to, false, true
	e.last, e.fresh = c, true
	e.log("вручную", ctx, Verdict{CurLang: from, Cur: keymap.RenderAll(c.origKeys, from), Alt: act.Text, Reason: "ручная конвертация"})
	return Result{Actions: []Action{act}}
}

// cycleNext — следующая раскладка в круге: исходная, «парная», остальные.
func cycleNext(orig, cur keymap.Lang) keymap.Lang {
	cycle := []keymap.Lang{orig, orig.Other()}
	for _, l := range keymap.Alternatives(orig) {
		if l != orig.Other() {
			cycle = append(cycle, l)
		}
	}
	for i, l := range cycle {
		if l == cur {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return orig
}

// recycle — заменить только что сконвертированное слово тем же набором клавиш в раскладке next.
func (e *Engine) recycle(c *correction, next keymap.Lang, ctx *Context) Result {
	n := c.newLen
	var trail []uint16
	if c.trail != 0 {
		n++
		trail = []uint16{c.trail}
	}
	act := Action{Kind: ActReplace, Backspaces: n, Text: keymap.RenderAll(c.origKeys, next), Keys: c.origKeys,
		Target: next, Trail: trail, Reason: "ручная конвертация"}
	switch {
	case c.histIdx == -1:
		e.cur.lang = next
	case c.histIdx >= 0 && c.histIdx < len(e.hist):
		e.hist[c.histIdx].lang = next
	}
	c.newLang, c.newLen = next, len([]rune(act.Text))
	e.fresh = true
	return Result{Actions: []Action{act}}
}

// convertPhrase — вся фраза с начала строки (до Enter/щелчка/стрелок).
func (e *Engine) convertPhrase(ctx *Context) Result {
	if e.fresh && e.last != nil && len(e.last.origLangs) > 0 {
		return e.undo(ctx, false)
	}
	layout := e.effectiveLayout(ctx)
	toks := append([]token(nil), e.hist...)
	if len(e.cur.keys) > 0 {
		toks = append(toks, e.cur)
	}
	if len(toks) == 0 {
		return e.convertWord(ctx)
	}
	var keys []keymap.Key
	var origL, newL []keymap.Lang
	var lastTrail uint16
	for i, t := range toks {
		l := t.lang
		if l == keymap.Unknown || t.mixed {
			l = layout
		}
		if l == keymap.Unknown {
			l = keymap.EN
		}
		for _, k := range t.keys {
			keys = append(keys, k)
			origL = append(origL, l)
			newL = append(newL, l.Other())
		}
		if t.trail != 0 {
			if i == len(toks)-1 {
				lastTrail = t.trail
			} else {
				keys = append(keys, keymap.Key{VK: keymap.VK_SPACE})
				origL = append(origL, l)
				newL = append(newL, l.Other())
			}
		}
	}
	target := newL[len(newL)-1]
	uniform := true
	for _, l := range newL {
		if l != target {
			uniform = false
		}
	}
	n := len(keys)
	var trail []uint16
	if lastTrail != 0 {
		n++
		trail = []uint16{lastTrail}
	}
	act := Action{Kind: ActReplace, Backspaces: n, Text: renderPhrase(keys, newL), Target: target, Trail: trail, Reason: "конвертация фразы"}
	if uniform {
		act.Keys = keys
	}
	c := &correction{origKeys: keys, origLangs: origL, origLang: origL[len(origL)-1], newLang: target, newLen: len(keys), trail: lastTrail, manual: true, histIdx: -2}
	for i := range e.hist {
		e.hist[i].lang, e.hist[i].corrected, e.hist[i].mixed = e.hist[i].lang.Other(), true, false
	}
	if len(e.cur.keys) > 0 {
		e.cur.lang, e.cur.corrected, e.cur.mixed = e.cur.lang.Other(), true, false
	}
	e.last, e.fresh = c, true
	return Result{Actions: []Action{act}}
}

// HotkeyAction — имена действий горячих клавиш.
type HotkeyAction int

const (
	HKConvertWord HotkeyAction = iota + 1
	HKConvertPhrase
	HKConvertSelection
	HKUndo
	HKToggleEnabled
	HKSwitchLayout
	HKToggleApp
)

func (e *Engine) runHotkey(a HotkeyAction, ctx *Context) Result {
	switch a {
	case HKToggleEnabled:
		e.enabled = !e.enabled
		e.resetAll()
		return Result{Swallow: true, Actions: []Action{{Kind: ActEnabledChanged}}}
	case HKToggleApp:
		e.resetAll()
		return Result{Swallow: true, Actions: []Action{{Kind: ActToggleApp}}}
	case HKSwitchLayout:
		l := e.effectiveLayout(ctx)
		e.resetWord()
		if l == keymap.Unknown {
			l = keymap.EN
		}
		return Result{Swallow: true, Actions: []Action{{Kind: ActSwitchLayout, Target: l.Other()}}}
	}
	if !e.enabled || ctx.Rule.Mode == "off" || (ctx.Password && e.cfg.SkipPasswords) {
		return Result{}
	}
	var res Result
	switch a {
	case HKConvertWord:
		res = e.convertWord(ctx)
	case HKConvertPhrase:
		res = e.convertPhrase(ctx)
	case HKUndo:
		if e.fresh && e.last != nil {
			res = e.undo(ctx, false)
		} else {
			res = e.convertWord(ctx)
		}
	case HKConvertSelection:
		// Если выделения нет, а слово только что набрано — конвертируем слово,
		// так что одну клавишу можно назначить на оба действия.
		if !e.selection && (len(e.cur.keys) > 0 || len(e.hist) > 0 || (e.fresh && e.last != nil)) {
			res = e.convertWord(ctx)
			break
		}
		if !ctx.Rule.SelectionAllowed {
			return Result{Swallow: true}
		}
		e.resetAll()
		res = Result{Actions: []Action{{Kind: ActConvertSelection}}}
	}
	res.Swallow = true
	return res
}
