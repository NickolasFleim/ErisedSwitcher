// Предпросмотр окна настроек без Windows: go run ./tools/uipreview
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"erised/internal/config"
	"erised/internal/engine"
	"erised/internal/keymap"
	"erised/internal/predict"
	"erised/internal/webui"
	"erised/internal/words"
)

type host struct {
	cfg  *config.Config
	st   *words.Store
	pred *predict.Predictor
}

func (h *host) Config() *config.Config             { return h.cfg }
func (h *host) ApplyConfig(c *config.Config) error { h.cfg = c; return nil }
func (h *host) Words() *words.Store                { return h.st }
func (h *host) SaveWords() error                   { return nil }
func (h *host) Status() map[string]any {
	return map[string]any{"enabled": true, "admin": false, "layouts": "RU 04190419, EN 04090409", "dict": "149968 RU, 89934 EN слов", "data_dir": `C:\Users\you\AppData\Roaming\Smena`}
}
func (h *host) Log() []engine.LogEntry {
	return []engine.LogEntry{{Time: time.Now(), App: "windowsterminal.exe", Rule: "Терминалы", Event: "конец слова", V: engine.Verdict{Switch: true, Cur: "пше", Alt: "git", Reason: "второй вариант — слово из списка приложения"}}}
}
func (h *host) ClearLog() {}
func (h *host) RecentApps() []string {
	return []string{"windowsterminal.exe", "telegram.exe", "chrome.exe"}
}
func (h *host) Action(string) error           { return nil }
func (h *host) Predictor() *predict.Predictor { return h.pred }
func (h *host) HistoryStats() predict.Stats   { return h.pred.Hist.Stats(200) }
func (h *host) HistoryClear() error           { h.pred.Hist.Clear(); return nil }
func (h *host) UpdateStatus() map[string]any {
	return map[string]any{"current": "1.3.0", "status": "idle", "last_check": time.Now(),
		"latest": map[string]any{"version": "1.4.0", "name": "Erised Switcher 1.4.0", "notes": "- Новое: пример заметок к выпуску\n- Исправлено: что-то важное", "url": "https://github.com/NickolasFleim/ErisedSwitcher/releases"}}
}
func (h *host) UpdateAction(string) error { return nil }
func (h *host) HistoryForget(l, p, n string) error {
	h.pred.Hist.Forget(keymap.ParseLang(l), p, n)
	return nil
}

func main() {
	st := words.New(filepath.Join(os.TempDir(), "w.json"))
	for i := 0; i < 2; i++ {
		st.Observe(words.KindException, keymap.EN, "ghbdtn", "telegram.exe", 1, false, 100)
		st.Observe(words.KindManual, keymap.EN, "kubectl", "windowsterminal.exe", 1, false, 100)
		st.Observe(words.KindPassive, keymap.RU, "смена", "notepad.exe", 3, false, 100)
	}
	cfg := config.Default()
	cfg.LogDecisions = true
	hist := predict.NewHistory(filepath.Join(os.TempDir(), "typing.json"))
	for i := 0; i < 5; i++ {
		hist.Add(keymap.RU, "доброе", "утро")
		hist.Add(keymap.RU, "спасибо", "большое")
		hist.Add(keymap.EN, "thank", "you")
	}
	s, err := webui.Start(&host{cfg, st, &predict.Predictor{Hist: hist, Store: st}}, 0)
	if err != nil {
		panic(err)
	}
	fmt.Println(s.URL())
	select {}
}
