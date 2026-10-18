// Package webui — окно настроек: локальная страница (только 127.0.0.1, с секретным ключом в адресе).
package webui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"erised/internal/config"
	"erised/internal/engine"
	"erised/internal/predict"
	"erised/internal/words"
)

//go:embed index.html
var static embed.FS

// Host — то, что окно настроек может делать с программой.
type Host interface {
	Config() *config.Config
	ApplyConfig(c *config.Config) error
	Words() *words.Store
	SaveWords() error
	Status() map[string]any
	Log() []engine.LogEntry
	ClearLog()
	RecentApps() []string
	Action(name string) error
	Predictor() *predict.Predictor
	HistoryStats() predict.Stats
	HistoryClear() error
	HistoryForget(lang, prev, next string) error
	UpdateStatus() map[string]any
	UpdateAction(op string) error
	// Отчёты о сбоях и обращение в GitHub.
	Diagnostics() map[string]any
	DiagnosticsAction(op string) error
	// Установка в систему.
	InstallStatus() map[string]any
	InstallAction(op string) error
}

// Server — HTTP-сервер настроек.
type Server struct {
	host  Host
	token string
	ln    net.Listener
	srv   *http.Server
}

// Start запускает сервер на 127.0.0.1:port (0 — случайный порт).
func Start(host Host, port int) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil && port != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // порт занят — берём любой
	}
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := &Server{host: host, token: hex.EncodeToString(b), ln: ln}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/api/state", s.api(s.state))
	mux.HandleFunc("/api/config", s.api(s.saveConfig))
	mux.HandleFunc("/api/words", s.api(s.saveWords))
	mux.HandleFunc("/api/analyze", s.api(s.analyze))
	mux.HandleFunc("/api/log", s.api(s.log))
	mux.HandleFunc("/api/action", s.api(s.action))
	mux.HandleFunc("/api/history", s.api(s.history))
	mux.HandleFunc("/api/update", s.api(s.update))
	mux.HandleFunc("/api/diag", s.api(s.diag))
	mux.HandleFunc("/api/install", s.api(s.install))
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	return s, nil
}

// URL — адрес окна настроек.
func (s *Server) URL() string {
	return "http://" + s.ln.Addr().String() + "/?t=" + s.token
}

// Close останавливает сервер.
func (s *Server) Close() { _ = s.srv.Close() }

// Защита от чужих страниц в браузере: адрес только наш, ключ обязателен.
func (s *Server) allowed(r *http.Request) bool {
	host := r.Host
	if host != s.ln.Addr().String() && !strings.HasPrefix(host, "localhost:"+portOf(s.ln.Addr().String())) {
		return false
	}
	t := r.Header.Get("X-Token")
	if t == "" {
		t = r.URL.Query().Get("t")
	}
	return t == s.token
}

func portOf(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[i+1:]
	}
	return ""
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.allowed(r) {
		http.Error(w, "Откройте настройки из меню значка Erised Switcher в трее.", http.StatusForbidden)
		return
	}
	b, _ := static.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
	_, _ = w.Write(b)
}

type apiFunc func(r *http.Request) (any, error)

func (s *Server) api(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.allowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "нужен JSON", http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		res, err := f(r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

func (s *Server) state(r *http.Request) (any, error) {
	return map[string]any{
		"config":   s.host.Config(),
		"defaults": config.Default(),
		"words":    s.host.Words().Snapshot(),
		"status":   s.host.Status(),
		"apps":     s.host.RecentApps(),
	}, nil
}

func (s *Server) saveConfig(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, errMethod
	}
	c := config.Default()
	if err := json.NewDecoder(r.Body).Decode(c); err != nil {
		return nil, err
	}
	if errs := c.Validate(); len(errs) > 0 {
		return nil, &validationError{errs}
	}
	if err := s.host.ApplyConfig(c); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "config": s.host.Config()}, nil
}

func (s *Server) saveWords(r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, errMethod
	}
	var d words.Data
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		return nil, err
	}
	s.host.Words().SetData(d)
	if err := s.host.SaveWords(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "words": s.host.Words().Snapshot()}, nil
}

func (s *Server) analyze(r *http.Request) (any, error) {
	var q struct {
		Text string `json:"text"`
		App  string `json:"app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		return nil, err
	}
	if len(q.Text) > 4000 {
		q.Text = q.Text[:4000]
	}
	return engine.Analyze(q.Text, q.App, s.host.Config(), s.host.Words(), s.host.Predictor()), nil
}

func (s *Server) log(r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		s.host.ClearLog()
	}
	return s.host.Log(), nil
}

func (s *Server) action(r *http.Request) (any, error) {
	var q struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		return nil, err
	}
	if err := s.host.Action(q.Name); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "status": s.host.Status()}, nil
}

type validationError struct{ errs []string }

func (v *validationError) Error() string { return strings.Join(v.errs, "\n") }

type constError string

func (e constError) Error() string { return string(e) }

const errMethod = constError("нужен POST")

func (s *Server) history(r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var q struct {
			Op   string `json:"op"` // clear | forget
			Lang string `json:"lang"`
			Prev string `json:"prev"`
			Next string `json:"next"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			return nil, err
		}
		var err error
		switch q.Op {
		case "clear":
			err = s.host.HistoryClear()
		case "forget":
			err = s.host.HistoryForget(q.Lang, q.Prev, q.Next)
		default:
			return nil, constError("неизвестная операция")
		}
		if err != nil {
			return nil, err
		}
	}
	return s.host.HistoryStats(), nil
}

func (s *Server) update(r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var q struct {
			Op string `json:"op"` // check | install | skip
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			return nil, err
		}
		if err := s.host.UpdateAction(q.Op); err != nil {
			return nil, err
		}
	}
	return s.host.UpdateStatus(), nil
}

func (s *Server) diag(r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var q struct {
			Op string `json:"op"` // issue | clear
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			return nil, err
		}
		if err := s.host.DiagnosticsAction(q.Op); err != nil {
			return nil, err
		}
	}
	return s.host.Diagnostics(), nil
}

func (s *Server) install(r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var q struct {
			Op string `json:"op"` // install | uninstall
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			return nil, err
		}
		if err := s.host.InstallAction(q.Op); err != nil {
			return nil, err
		}
	}
	return s.host.InstallStatus(), nil
}
