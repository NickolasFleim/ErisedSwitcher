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
	"erised/internal/webui"
	"erised/internal/words"
)

type host struct {
	cfg *config.Config
	st  *words.Store
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
func (h *host) Action(string) error { return nil }

func main() {
	st := words.New(filepath.Join(os.TempDir(), "w.json"))
	for i := 0; i < 2; i++ {
		st.Observe(words.KindException, keymap.EN, "ghbdtn", "telegram.exe", 1, false, 100)
		st.Observe(words.KindManual, keymap.EN, "kubectl", "windowsterminal.exe", 1, false, 100)
		st.Observe(words.KindPassive, keymap.RU, "смена", "notepad.exe", 3, false, 100)
	}
	cfg := config.Default()
	cfg.LogDecisions = true
	s, err := webui.Start(&host{cfg, st}, 0)
	if err != nil {
		panic(err)
	}
	fmt.Println(s.URL())
	select {}
}
