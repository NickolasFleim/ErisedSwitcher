//go:build windows

package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"erised/internal/config"
	"erised/internal/engine"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
	"erised/internal/webui"
	"erised/internal/win"
	"erised/internal/words"
)

// SelfTest — ErisedSwitcher.exe --selftest <файл отчёта>. Проверяет на настоящей Windows то,
// что нельзя проверить обычными тестами: перехват клавиш, окна, буфер обмена, установку.
// Работает во временных папках; реестр и ярлык после проверки удаляются. Код выхода 0 — всё хорошо.
func SelfTest(reportPath string) int {
	runtime.LockOSThread()
	var out strings.Builder
	failed := 0
	check := func(name string, err error) {
		if err != nil {
			failed++
			fmt.Fprintf(&out, "FAIL  %s: %v\n", name, err)
		} else {
			fmt.Fprintf(&out, "ok    %s\n", name)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			failed++
			fmt.Fprintf(&out, "FAIL  сбой: %v\n", r)
		}
		fmt.Fprintf(&out, "\n%d ошибок\n", failed)
		if reportPath != "" {
			_ = os.WriteFile(reportPath, []byte(out.String()), 0o644)
		}
		fmt.Print(out.String())
	}()

	tmp, err := os.MkdirTemp("", "erised-selftest-")
	if err != nil {
		check("временная папка", err)
		return 1
	}
	defer os.RemoveAll(tmp)
	// Всё «пользовательское» — во временной папке.
	os.Setenv("ERISED_HOME", filepath.Join(tmp, "data"))
	os.Setenv("LOCALAPPDATA", filepath.Join(tmp, "local"))
	os.Setenv("APPDATA", filepath.Join(tmp, "roaming"))
	fmt.Fprintf(&out, "Erised Switcher %s, %s, %s\n\n", Version, win.WindowsVersion(), runtime.GOARCH)

	// 1. Настройки и словари.
	cfg, err := config.Load(filepath.Join(config.Dir(), "config.json"))
	check("настройки по умолчанию", err)
	cfg.ProcessInjected = true // в проверке нажатия посылаем сами
	st := words.New(filepath.Join(config.Dir(), "words.json"))
	t0 := time.Now()
	lang.Preload()
	for _, l := range keymap.All {
		lang.Get(l).PrepareSimilar()
	}
	fmt.Fprintf(&out, "      словари загружены за %v\n", time.Since(t0).Round(time.Millisecond))
	eng := engine.New(cfg, st)
	pred := &predict.Predictor{Hist: predict.NewHistory(filepath.Join(tmp, "typing.json")), Store: st}
	eng.SetPredictor(pred)
	res := engine.Analyze("ghbdtn пртвет", "notepad.exe", cfg, st, pred)
	if len(res) != 2 || !res[0].Final.Switch || res[1].Typo == nil || !res[1].Typo.Applied {
		check("распознавание раскладки и опечаток", fmt.Errorf("неожиданный результат: %+v", res))
	} else {
		check("распознавание раскладки и опечаток", nil)
	}

	// 2. Перехват клавиатуры: посылаем нажатия и ждём, что они дойдут до обработчика.
	var seen, sent atomic.Int32
	hooks := &win.Hooks{OnKey: func(ev win.KeyHookEvent) bool {
		if ev.VK == 0x87 /*F24*/ {
			seen.Add(1)
		}
		return false
	}, OnMouse: func(uint32, win.POINT, uint32) {}}
	if err := hooks.Install(); err != nil {
		check("перехват клавиатуры", err)
	} else {
		quit := win.CreateMessageWindow("ErisedSelfTest", syscallCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
			if msg == win.WM_TIMER {
				win.PostQuitMessage()
				return 0
			}
			return win.DefWindowProc(h, msg, w, l)
		}))
		go func() {
			time.Sleep(200 * time.Millisecond)
			sent.Store(int32(win.SendInputs([]win.INPUT{win.VKInput(0x87, false, 0), win.VKInput(0x87, true, 0)})))
		}()
		win.SetTimer(quit, 1, 1500)
		win.MessageLoop()
		hooks.Uninstall()
		win.DestroyWindow(quit)
		if seen.Load() >= 2 {
			check("перехват клавиатуры", nil)
		} else if sent.Load() == 0 {
			fmt.Fprintf(&out, "skip  перехват клавиатуры: система не принимает имитацию нажатий (нет интерактивного рабочего стола)\n")
		} else {
			check("перехват клавиатуры", fmt.Errorf("событий получено: %d из 2", seen.Load()))
		}
	}

	// 3. Полоска подсказок.
	p := win.NewPopup(nil)
	p.Show(win.PopupState{Items: []win.PopupItem{{Text: "привет", Hint: "Tab", Bold: true}, {Text: "приветствую"}},
		Position: "bottom", Fallback: "bottom", Scale: 1, Version: 1})
	ok := false
	for i := 0; i < 30 && !ok; i++ {
		time.Sleep(50 * time.Millisecond)
		ok = p.Visible()
	}
	if ok {
		check("полоска подсказок", nil)
	} else {
		check("полоска подсказок", fmt.Errorf("окно не появилось"))
	}
	p.Hide()

	// 3а. Чтение текста у курсора (специальные возможности): должно отвечать быстро и не падать.
	t1 := time.Now()
	if _, ok := win.ReadCaretText(time.Second); ok {
		check("чтение текста у курсора", nil)
	} else if time.Since(t1) >= time.Second {
		check("чтение текста у курсора", fmt.Errorf("нет ответа за секунду"))
	} else {
		fmt.Fprintf(&out, "skip  чтение текста у курсора: нет поля ввода с фокусом\n")
	}

	// 4. Сервер настроек.
	srv, err := webui.Start(selfTestHost{cfg: cfg, st: st, pred: pred}, 0)
	if err != nil {
		check("сервер настроек", err)
	} else {
		resp, err := http.Get(srv.URL())
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || !strings.Contains(string(b), "Erised Switcher") {
				err = fmt.Errorf("ответ %d", resp.StatusCode)
			}
		}
		check("сервер настроек", err)
		srv.Close()
	}

	// 5. Буфер обмена.
	const sample = "Erised проверка ✓"
	if err := win.SetClipboardText(0, sample); err != nil {
		check("буфер обмена", err)
	} else if got, _ := win.GetClipboardText(0); got != sample {
		check("буфер обмена", fmt.Errorf("прочитано %q", got))
	} else {
		check("буфер обмена", nil)
	}

	// 6. Установка и удаление (во временные папки; запись в реестре удаляется).
	// Если программа на этом компьютере уже установлена — её запись не трогаем, проверку пропускаем.
	if win.RegKeyExistsHKCU(uninstallKey) {
		fmt.Fprintf(&out, "skip  установка: программа уже установлена на этом компьютере\n")
		if failed > 0 {
			return 1
		}
		return 0
	}
	dst, err := installTo()
	if err == nil {
		switch {
		case !fileExists(dst):
			err = fmt.Errorf("нет файла %s", dst)
		case !fileExists(startMenuShortcut()):
			err = fmt.Errorf("нет ярлыка")
		case !win.RegKeyExistsHKCU(uninstallKey):
			err = fmt.Errorf("нет записи в «Приложениях»")
		}
	}
	check("установка", err)
	win.RegDeleteTreeHKCU(uninstallKey)
	if win.RegKeyExistsHKCU(uninstallKey) {
		check("удаление записи", fmt.Errorf("запись осталась"))
	} else {
		check("удаление записи", nil)
	}

	if failed > 0 {
		return 1
	}
	return 0
}

// selfTestHost — минимальный Host для проверки сервера настроек.
type selfTestHost struct {
	cfg  *config.Config
	st   *words.Store
	pred *predict.Predictor
}

func (h selfTestHost) Config() *config.Config              { return h.cfg }
func (h selfTestHost) ApplyConfig(*config.Config) error    { return nil }
func (h selfTestHost) Words() *words.Store                 { return h.st }
func (h selfTestHost) SaveWords() error                    { return nil }
func (h selfTestHost) Status() map[string]any              { return map[string]any{"version": Version} }
func (h selfTestHost) Log() []engine.LogEntry              { return nil }
func (h selfTestHost) ClearLog()                           {}
func (h selfTestHost) RecentApps() []string                { return nil }
func (h selfTestHost) Action(string) error                 { return nil }
func (h selfTestHost) Predictor() *predict.Predictor       { return h.pred }
func (h selfTestHost) HistoryStats() predict.Stats         { return predict.Stats{} }
func (h selfTestHost) HistoryClear() error                 { return nil }
func (h selfTestHost) HistoryForget(_, _, _ string) error  { return nil }
func (h selfTestHost) UpdateStatus() map[string]any        { return map[string]any{"current": Version} }
func (h selfTestHost) UpdateAction(string) error           { return nil }
func (h selfTestHost) Diagnostics() map[string]any         { return map[string]any{} }
func (h selfTestHost) DiagnosticsAction(string) error      { return nil }
func (h selfTestHost) InstallStatus() map[string]any       { return map[string]any{} }
func (h selfTestHost) InstallAction(string) error          { return nil }
func (h selfTestHost) ExportSettings(bool) ([]byte, error) { return nil, nil }
func (h selfTestHost) ImportSettings([]byte) error         { return nil }
func (h selfTestHost) BackupNow() error                    { return nil }
func (h selfTestHost) PickFolder() string                  { return "" }

var _ webui.Host = selfTestHost{}
