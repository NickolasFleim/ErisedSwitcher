// Package stats — замер точности на вашем наборе: сколько исправлено, сколько вы отменили
// (ошибки программы), сколько исправили вручную (пропуски). Счётчики по дням — без текста;
// примеры слов записываются, только если вы это включили (или в режиме «только наблюдать»).
package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Counts — счётчики за день.
type Counts struct {
	Layout       int `json:"layout"`        // исправлена раскладка
	Typo         int `json:"typo"`          // исправлена опечатка
	UndoLayout   int `json:"undo_layout"`   // вы отменили исправление раскладки
	UndoTypo     int `json:"undo_typo"`     // вы отменили исправление опечатки
	Manual       int `json:"manual"`        // вы сами сконвертировали слово, которое программа пропустила
	WouldLayout  int `json:"would_layout"`  // «только наблюдать»: исправила бы раскладку
	WouldTypo    int `json:"would_typo"`    // «только наблюдать»: исправила бы опечатку
	CheckedOK    int `json:"checked_ok"`    // при разборе вы отметили «верно»
	CheckedWrong int `json:"checked_wrong"` // … и «ошибка»
}

func (c *Counts) add(o Counts) {
	c.Layout += o.Layout
	c.Typo += o.Typo
	c.UndoLayout += o.UndoLayout
	c.UndoTypo += o.UndoTypo
	c.Manual += o.Manual
	c.WouldLayout += o.WouldLayout
	c.WouldTypo += o.WouldTypo
	c.CheckedOK += o.CheckedOK
	c.CheckedWrong += o.CheckedWrong
}

// Example — пример для разбора.
type Example struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	App     string    `json:"app"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Lang    string    `json:"lang"`
	Verdict string    `json:"verdict,omitempty"` // "", ok, wrong
}

const (
	keepDays     = 60
	maxExamples  = 300
	dayLayout    = "2006-01-02"
	saveInterval = 10 * time.Second
)

// Store — статистика в памяти с сохранением в файл.
type Store struct {
	mu       sync.Mutex
	path     string
	Days     map[string]*Counts `json:"days"`
	Examples []Example          `json:"examples"`
	NextID   int64              `json:"next_id"`
	dirty    bool
	now      func() time.Time
}

// Load читает файл (если его нет — пустая статистика).
func Load(path string) *Store {
	s := &Store{path: path, Days: map[string]*Counts{}, now: time.Now}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, s)
		if s.Days == nil {
			s.Days = map[string]*Counts{}
		}
	}
	return s
}

// Add учитывает событие; withExample — записать слово для разбора.
func (s *Store) Add(kind, app, from, to, lang string, withExample bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	day := now.Format(dayLayout)
	c := s.Days[day]
	if c == nil {
		c = &Counts{}
		s.Days[day] = c
	}
	switch kind {
	case "layout":
		c.Layout++
	case "typo":
		c.Typo++
	case "undo_layout":
		c.UndoLayout++
	case "undo_typo":
		c.UndoTypo++
	case "manual":
		c.Manual++
	case "would_layout":
		c.WouldLayout++
	case "would_typo":
		c.WouldTypo++
	default:
		return
	}
	if withExample && kind != "layout" && kind != "typo" { // обычные исправления не записываем — их слишком много
		s.NextID++
		s.Examples = append(s.Examples, Example{ID: s.NextID, Time: now, Kind: kind, App: app, From: from, To: to, Lang: lang})
		if len(s.Examples) > maxExamples {
			s.Examples = s.Examples[len(s.Examples)-maxExamples:]
		}
	}
	s.dirty = true
}

// Mark отмечает пример при разборе («верно» или «ошибка») и возвращает его.
func (s *Store) Mark(id int64, verdict string) (Example, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Examples {
		e := &s.Examples[i]
		if e.ID != id {
			continue
		}
		day := s.now().Format(dayLayout)
		c := s.Days[day]
		if c == nil {
			c = &Counts{}
			s.Days[day] = c
		}
		// Повторная отметка заменяет прежнюю.
		switch e.Verdict {
		case "ok":
			c.CheckedOK--
		case "wrong":
			c.CheckedWrong--
		}
		switch verdict {
		case "ok":
			c.CheckedOK++
		case "wrong":
			c.CheckedWrong++
		}
		e.Verdict = verdict
		s.dirty = true
		return *e, true
	}
	return Example{}, false
}

// Clear стирает всё.
func (s *Store) Clear() {
	s.mu.Lock()
	s.Days, s.Examples, s.dirty = map[string]*Counts{}, nil, true
	s.mu.Unlock()
}

// Day — счётчики одного дня.
type Day struct {
	Date string `json:"date"`
	Counts
}

// Summary — для окна настроек: по дням за последние n дней, итоги за 7 и 30 дней, примеры.
func (s *Store) Summary(n int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var days []Day
	var t7, t30 Counts
	for i := n - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format(dayLayout)
		c := Counts{}
		if p := s.Days[d]; p != nil {
			c = *p
		}
		days = append(days, Day{Date: d, Counts: c})
		if i < 7 {
			t7.add(c)
		}
		if i < 30 {
			t30.add(c)
		}
	}
	ex := make([]Example, len(s.Examples))
	copy(ex, s.Examples)
	sort.Slice(ex, func(i, j int) bool { return ex[i].ID > ex[j].ID })
	return map[string]any{"days": days, "week": t7, "month": t30, "examples": ex}
}

// SaveIfDirty сохраняет файл, если были изменения; заодно выбрасывает старые дни.
func (s *Store) SaveIfDirty() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	cut := s.now().AddDate(0, 0, -keepDays).Format(dayLayout)
	for d := range s.Days {
		if d < cut {
			delete(s.Days, d)
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
