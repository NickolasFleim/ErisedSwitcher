package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"erised/internal/config"
	"erised/internal/engine"
	"erised/internal/words"
)

type fakeHost struct {
	cfg *config.Config
	st  *words.Store
}

func (f *fakeHost) Config() *config.Config             { return f.cfg }
func (f *fakeHost) ApplyConfig(c *config.Config) error { f.cfg = c; return nil }
func (f *fakeHost) Words() *words.Store                { return f.st }
func (f *fakeHost) SaveWords() error                   { return f.st.SaveIfDirty() }
func (f *fakeHost) Status() map[string]any             { return map[string]any{"enabled": true} }
func (f *fakeHost) Log() []engine.LogEntry             { return nil }
func (f *fakeHost) ClearLog()                          {}
func (f *fakeHost) RecentApps() []string               { return []string{"notepad.exe"} }
func (f *fakeHost) Action(string) error                { return nil }

func TestServer(t *testing.T) {
	h := &fakeHost{cfg: config.Default(), st: words.New(filepath.Join(t.TempDir(), "w.json"))}
	s, err := Start(h, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := strings.Split(s.URL(), "/?")[0]

	// Без ключа — запрет.
	if r, _ := http.Get(base + "/api/state"); r.StatusCode != 403 {
		t.Fatalf("без ключа: %d", r.StatusCode)
	}
	// Страница с ключом.
	r, err := http.Get(s.URL())
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("страница: %v %v", err, r.StatusCode)
	}
	post := func(path string, body any) (*http.Response, map[string]any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Token", s.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp, m
	}
	// Неверная горячая клавиша отклоняется.
	bad := config.Default()
	bad.Hotkeys.ConvertWord = "Ctrl+Nope"
	if resp, m := post("/api/config", bad); resp.StatusCode != 400 || !strings.Contains(m["error"].(string), "Nope") {
		t.Fatalf("ожидалась ошибка: %v %v", resp.StatusCode, m)
	}
	good := config.Default()
	good.DoubleTapMs = 333
	if resp, _ := post("/api/config", good); resp.StatusCode != 200 || h.cfg.DoubleTapMs != 333 {
		t.Fatalf("сохранение: %v", resp.StatusCode)
	}
	// Слова: повторы убираются.
	d := words.Data{User: map[string][]string{"en": {"Foo", "foo", "bar"}}}
	if resp, m := post("/api/words", d); resp.StatusCode != 200 {
		t.Fatalf("слова: %v", m)
	}
	if got := h.st.Snapshot().User["en"]; len(got) != 2 {
		t.Fatalf("повторы не убраны: %v", got)
	}
	// Проверка слова.
	b, _ := json.Marshal(map[string]string{"text": "ghbdtn"})
	req, _ := http.NewRequest("POST", base+"/api/analyze", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	resp, _ := http.DefaultClient.Do(req)
	var res []engine.AnalyzeResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res) != 1 || !res[0].Final.Switch || res[0].Final.Alt != "привет" {
		t.Fatalf("analyze: %+v", res)
	}
	// Чужой Host-заголовок (DNS rebinding) — запрет.
	req, _ = http.NewRequest("GET", base+"/api/state", nil)
	req.Host = "evil.example:80"
	req.Header.Set("X-Token", s.token)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Fatalf("чужой Host: %d", resp.StatusCode)
	}
}
