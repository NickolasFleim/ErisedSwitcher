// Package words — пользовательские словари: свои слова, исключения, автозамены,
// автоматически собранные слова и кандидаты. Каждое слово хранится один раз:
// ключ — слово в нижнем регистре + язык, повторы при сборе только увеличивают счётчик.
package words

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"erised/internal/config"
	"erised/internal/keymap"
)

// Kind — откуда кандидат.
type Kind string

const (
	// KindException — вы отменили исправление этого слова.
	KindException Kind = "exception"
	// KindManual — вы вручную сконвертировали слово в этот вариант.
	KindManual Kind = "manual"
	// KindPassive — вы набирали это незнакомое слово и не исправляли.
	KindPassive Kind = "passive"
)

// Entry — собранное слово или кандидат.
type Entry struct {
	Word  string    `json:"w"`
	Lang  string    `json:"lang"`
	Kind  Kind      `json:"kind"`
	Count int       `json:"n"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
	Apps  []string  `json:"apps,omitempty"` // где встречалось (до 5)
}

// Replace — автозамена.
type Replace struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Учитывать регистр при сравнении.
	CaseSensitive bool `json:"case,omitempty"`
}

// Data — содержимое words.json.
type Data struct {
	// Свои слова: всегда считаются правильными в своём языке.
	User map[string][]string `json:"user"`
	// Исключения: это написание в этом языке никогда не исправлять.
	Exceptions map[string][]string `json:"exceptions"`
	// Автозамены.
	AutoReplace []Replace `json:"autoreplace"`
	// Собранные автоматически (уже действуют как свои слова / исключения).
	Learned []Entry `json:"learned"`
	// Кандидаты (ещё не набрали нужное число повторов или ждут проверки).
	Candidates []Entry `json:"candidates"`
	// Слова, которые не нужно собирать никогда (вы удалили их из собранных).
	Ignored map[string][]string `json:"ignored"`
}

// Store — потокобезопасное хранилище с индексами.
type Store struct {
	mu    sync.RWMutex
	path  string
	d     Data
	dirty bool

	user, exc, ign map[string]bool // ключ: lang + ":" + слово
	learnedWord    map[string]int  // индекс в Learned (только manual/passive)
	learnedExc     map[string]int  // индекс в Learned (exception)
	cand           map[string]int  // ключ: kind + ":" + lang + ":" + слово
}

func key(l keymap.Lang, w string) string { return l.String() + ":" + strings.ToLower(w) }

// New создаёт пустое хранилище, связанное с файлом.
func New(path string) *Store {
	s := &Store{path: path}
	s.d = emptyData()
	s.reindex()
	return s
}

func emptyData() Data {
	return Data{
		User:       map[string][]string{"ru": {}, "en": {}},
		Exceptions: map[string][]string{"ru": {}, "en": {}},
		Ignored:    map[string][]string{"ru": {}, "en": {}},
	}
}

// Load читает words.json (отсутствие файла — не ошибка).
func Load(path string) (*Store, error) {
	s := New(path)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	d := emptyData()
	if err := json.Unmarshal(b, &d); err != nil {
		return s, err
	}
	s.SetData(d)
	s.dirty = false
	return s, nil
}

// SetData заменяет всё содержимое (после правки в окне настроек или в файле),
// убирая повторы и нормализуя регистр.
func (s *Store) SetData(d Data) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.User == nil {
		d.User = map[string][]string{}
	}
	if d.Exceptions == nil {
		d.Exceptions = map[string][]string{}
	}
	if d.Ignored == nil {
		d.Ignored = map[string][]string{}
	}
	for _, m := range []map[string][]string{d.User, d.Exceptions, d.Ignored} {
		for _, l := range []string{"ru", "en"} {
			m[l] = dedupe(m[l])
		}
	}
	d.Learned = dedupeEntries(d.Learned)
	d.Candidates = dedupeEntries(d.Candidates)
	// Убираем повторы автозамен (по исходному слову).
	seen := map[string]bool{}
	var ar []Replace
	for _, r := range d.AutoReplace {
		f := strings.TrimSpace(r.From)
		if f == "" {
			continue
		}
		k := f
		if !r.CaseSensitive {
			k = strings.ToLower(f)
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		r.From = f
		ar = append(ar, r)
	}
	d.AutoReplace = ar
	s.d = d
	s.reindex()
	s.dirty = true
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range in {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

func dedupeEntries(in []Entry) []Entry {
	idx := map[string]int{}
	var out []Entry
	for _, e := range in {
		e.Word = strings.ToLower(strings.TrimSpace(e.Word))
		if e.Word == "" || keymap.ParseLang(e.Lang) == keymap.Unknown {
			continue
		}
		if e.Kind == "" {
			e.Kind = KindPassive
		}
		k := string(e.Kind) + ":" + e.Lang + ":" + e.Word
		if e.Kind != KindException {
			k = "word:" + e.Lang + ":" + e.Word
		}
		if i, ok := idx[k]; ok {
			out[i].Count += e.Count
			if e.Last.After(out[i].Last) {
				out[i].Last = e.Last
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, e)
	}
	return out
}

func (s *Store) reindex() {
	s.user, s.exc, s.ign = map[string]bool{}, map[string]bool{}, map[string]bool{}
	s.learnedWord, s.learnedExc, s.cand = map[string]int{}, map[string]int{}, map[string]int{}
	for l, ws := range s.d.User {
		for _, w := range ws {
			s.user[l+":"+w] = true
		}
	}
	for l, ws := range s.d.Exceptions {
		for _, w := range ws {
			s.exc[l+":"+w] = true
		}
	}
	for l, ws := range s.d.Ignored {
		for _, w := range ws {
			s.ign[l+":"+w] = true
		}
	}
	for i, e := range s.d.Learned {
		if e.Kind == KindException {
			s.learnedExc[e.Lang+":"+e.Word] = i
		} else {
			s.learnedWord[e.Lang+":"+e.Word] = i
		}
	}
	for i, e := range s.d.Candidates {
		s.cand[candKey(e.Kind, e.Lang, e.Word)] = i
	}
}

func candKey(k Kind, lang, w string) string {
	if k != KindException {
		k = "word"
	}
	return string(k) + ":" + lang + ":" + w
}

// IsWord — слово считается правильным в языке l (свои или собранные слова).
func (s *Store) IsWord(l keymap.Lang, w string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := key(l, w)
	if s.user[k] {
		return true
	}
	_, ok := s.learnedWord[k]
	return ok
}

// IsException — это написание в языке l исправлять нельзя.
func (s *Store) IsException(l keymap.Lang, w string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := key(l, w)
	if s.exc[k] {
		return true
	}
	_, ok := s.learnedExc[k]
	return ok
}

// AutoReplace ищет автозамену для набранного текста.
func (s *Store) AutoReplace(text string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.d.AutoReplace {
		if r.CaseSensitive && r.From == text {
			return r.To, true
		}
		if !r.CaseSensitive && strings.EqualFold(r.From, text) {
			return r.To, true
		}
	}
	return "", false
}

// Observe учитывает событие сбора. threshold — сколько повторов нужно,
// review — оставить кандидатом даже после порога. Возвращает true, если слово
// перешло в собранные.
func (s *Store) Observe(kind Kind, l keymap.Lang, w, app string, threshold int, review bool, maxCand int) bool {
	w = strings.ToLower(strings.TrimSpace(w))
	if w == "" || l == keymap.Unknown {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(l, w)
	if s.ign[k] {
		return false
	}
	// Уже известно — ничего не собираем (без повторов).
	if kind == KindException {
		if s.exc[k] {
			return false
		}
		if _, ok := s.learnedExc[k]; ok {
			return false
		}
	} else {
		if s.user[k] {
			return false
		}
		if _, ok := s.learnedWord[k]; ok {
			return false
		}
		// Если слово только что стало исключением в том же языке, оно и так не исправляется.
		if kind == KindPassive && (s.exc[k] || s.hasLearnedExc(k)) {
			return false
		}
	}
	now := time.Now()
	ck := candKey(kind, l.String(), w)
	i, ok := s.cand[ck]
	if !ok {
		s.d.Candidates = append(s.d.Candidates, Entry{Word: w, Lang: l.String(), Kind: kind, First: now})
		i = len(s.d.Candidates) - 1
		s.cand[ck] = i
	}
	e := &s.d.Candidates[i]
	e.Count++
	e.Last = now
	// Более «сильный» источник важнее: ручная конвертация главнее пассивного сбора.
	if kind == KindManual {
		e.Kind = KindManual
	}
	if app != "" && !contains(e.Apps, app) && len(e.Apps) < 5 {
		e.Apps = append(e.Apps, app)
	}
	s.dirty = true
	if e.Count >= threshold && !review {
		s.promoteLocked(i)
		return true
	}
	if len(s.d.Candidates) > maxCand && maxCand > 0 {
		s.trimLocked(maxCand)
	}
	return false
}

func (s *Store) hasLearnedExc(k string) bool { _, ok := s.learnedExc[k]; return ok }

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (s *Store) promoteLocked(i int) {
	e := s.d.Candidates[i]
	s.d.Candidates = append(s.d.Candidates[:i], s.d.Candidates[i+1:]...)
	s.d.Learned = append(s.d.Learned, e)
	s.reindex()
}

// Вытесняем кандидатов с наименьшим счётчиком и самых старых.
func (s *Store) trimLocked(max int) {
	c := s.d.Candidates
	sort.SliceStable(c, func(a, b int) bool {
		if c[a].Count != c[b].Count {
			return c[a].Count > c[b].Count
		}
		return c[a].Last.After(c[b].Last)
	})
	s.d.Candidates = c[:max]
	s.reindex()
}

// Forget убирает слово из собранных (например, при отмене ошибочного обучения).
func (s *Store) Forget(kind Kind, l keymap.Lang, w string) {
	w = strings.ToLower(w)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.d.Learned[:0]
	for _, e := range s.d.Learned {
		same := e.Word == w && e.Lang == l.String() && ((kind == KindException) == (e.Kind == KindException))
		if !same {
			out = append(out, e)
		}
	}
	s.d.Learned = out
	s.reindex()
	s.dirty = true
}

// Unlearn убирает слово (не исключение) из собранных и из кандидатов:
// вызывается, когда вы вручную конвертировали слово, которое программа считала правильным.
func (s *Store) Unlearn(l keymap.Lang, w string) {
	w = strings.ToLower(w)
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	ls := s.d.Learned[:0]
	for _, e := range s.d.Learned {
		if e.Kind != KindException && e.Word == w && e.Lang == l.String() {
			changed = true
			continue
		}
		ls = append(ls, e)
	}
	s.d.Learned = ls
	cs := s.d.Candidates[:0]
	for _, e := range s.d.Candidates {
		if e.Kind != KindException && e.Word == w && e.Lang == l.String() {
			changed = true
			continue
		}
		cs = append(cs, e)
	}
	s.d.Candidates = cs
	if changed {
		s.reindex()
		s.dirty = true
	}
}

// Snapshot — копия данных для окна настроек.
func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.d)
	var d Data
	_ = json.Unmarshal(b, &d)
	return d
}

// SaveIfDirty сохраняет файл, если были изменения.
func (s *Store) SaveIfDirty() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return config.WriteAtomic(s.path, b)
}

// Path — путь к файлу.
func (s *Store) Path() string { return s.path }

// EachUserWord вызывает f для каждого вашего слова языка l (свои и собранные, без исключений),
// начинающегося с prefix. f возвращает false, чтобы остановиться.
func (s *Store) EachUserWord(l keymap.Lang, prefix string, f func(w string) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ls := l.String()
	for _, w := range s.d.User[ls] {
		if strings.HasPrefix(w, prefix) && !f(w) {
			return
		}
	}
	for _, e := range s.d.Learned {
		if e.Kind != KindException && e.Lang == ls && strings.HasPrefix(e.Word, prefix) && !f(e.Word) {
			return
		}
	}
}
