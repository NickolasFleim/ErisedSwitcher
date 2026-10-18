package predict

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"

	"erised/internal/config"
	"erised/internal/keymap"
)

// History — ваша история набора: сколько раз встречалось слово и какие слова идут друг за другом.
// Хранятся только известные слова (из словаря или ваши), поэтому случайные строки,
// пароли и т.п. сюда не попадают. Только локально, в typing.json.
type History struct {
	mu    sync.RWMutex
	path  string
	uni   map[string]uint32            // "ru:слово" -> раз
	bi    map[string]map[string]uint32 // "ru:пред" -> следующее -> раз
	pairs int
	dirty bool
	// MaxPairs — предел числа пар слов (при превышении забываются самые редкие).
	MaxPairs int
}

type historyFile struct {
	Version int                          `json:"version"`
	Uni     map[string]uint32            `json:"uni"`
	Bi      map[string]map[string]uint32 `json:"bi"`
}

// NewHistory — пустая история, связанная с файлом.
func NewHistory(path string) *History {
	return &History{path: path, uni: map[string]uint32{}, bi: map[string]map[string]uint32{}, MaxPairs: 60000}
}

// LoadHistory читает typing.json (отсутствие файла — не ошибка).
func LoadHistory(path string) (*History, error) {
	h := NewHistory(path)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	var f historyFile
	if err := json.Unmarshal(b, &f); err != nil {
		return h, err
	}
	if f.Uni != nil {
		h.uni = f.Uni
	}
	if f.Bi != nil {
		h.bi = f.Bi
	}
	for _, m := range h.bi {
		h.pairs += len(m)
	}
	return h, nil
}

func hkey(l keymap.Lang, w string) string { return l.String() + ":" + w }

// Add учитывает набранное слово w (в нижнем регистре) и предыдущее prev (может быть пустым).
func (h *History) Add(l keymap.Lang, prev, w string) {
	if w == "" || l == keymap.Unknown {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.uni[hkey(l, w)]++
	if prev != "" {
		k := hkey(l, prev)
		m := h.bi[k]
		if m == nil {
			m = map[string]uint32{}
			h.bi[k] = m
		}
		if m[w] == 0 {
			h.pairs++
		}
		m[w]++
	}
	h.dirty = true
	if h.MaxPairs > 0 && h.pairs > h.MaxPairs {
		h.pruneLocked()
	}
}

// Удаляем самые редкие пары, пока не останется 90% предела.
func (h *History) pruneLocked() {
	target := h.MaxPairs * 9 / 10
	for limit := uint32(1); h.pairs > target && limit < 1<<20; limit++ {
		for k, m := range h.bi {
			for w, c := range m {
				if c <= limit {
					delete(m, w)
					h.pairs--
				}
			}
			if len(m) == 0 {
				delete(h.bi, k)
			}
			if h.pairs <= target {
				break
			}
		}
	}
	for k, c := range h.uni {
		if c <= 1 && len(h.uni) > h.MaxPairs {
			delete(h.uni, k)
		}
	}
}

// Count — сколько раз вы набирали слово.
func (h *History) Count(l keymap.Lang, w string) uint32 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.uni[hkey(l, w)]
}

// Pair — сколько раз после prev шло w.
func (h *History) Pair(l keymap.Lang, prev, w string) uint32 {
	if prev == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.bi[hkey(l, prev)][w]
}

// Next — слова, которые чаще всего шли после prev (не меньше minCount раз).
func (h *History) Next(l keymap.Lang, prev string, n int, minCount uint32) []WordCount {
	h.mu.RLock()
	m := h.bi[hkey(l, prev)]
	out := make([]WordCount, 0, len(m))
	for w, c := range m {
		if c >= minCount {
			out = append(out, WordCount{w, c})
		}
	}
	h.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// WordCount — слово и счётчик.
type WordCount struct {
	Word  string `json:"w"`
	Count uint32 `json:"n"`
}

// Stats — для окна настроек.
type Stats struct {
	Words    int         `json:"words"`
	Pairs    int         `json:"pairs"`
	TopPairs []PairCount `json:"top_pairs"`
}

// PairCount — пара слов и счётчик.
type PairCount struct {
	Lang  string `json:"lang"`
	Prev  string `json:"prev"`
	Next  string `json:"next"`
	Count uint32 `json:"n"`
}

// Stats собирает сводку и самые частые пары.
func (h *History) Stats(top int) Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := Stats{Words: len(h.uni), Pairs: h.pairs}
	for k, m := range h.bi {
		l, prev, _ := strings.Cut(k, ":")
		for w, c := range m {
			s.TopPairs = append(s.TopPairs, PairCount{l, prev, w, c})
		}
	}
	sort.Slice(s.TopPairs, func(i, j int) bool { return s.TopPairs[i].Count > s.TopPairs[j].Count })
	if len(s.TopPairs) > top {
		s.TopPairs = s.TopPairs[:top]
	}
	return s
}

// Forget удаляет пару (или все пары со словом prev, если next пустое).
func (h *History) Forget(l keymap.Lang, prev, next string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := hkey(l, prev)
	if next == "" {
		h.pairs -= len(h.bi[k])
		delete(h.bi, k)
	} else if m := h.bi[k]; m != nil {
		if _, ok := m[next]; ok {
			delete(m, next)
			h.pairs--
		}
	}
	h.dirty = true
}

// Clear стирает всю историю.
func (h *History) Clear() {
	h.mu.Lock()
	h.uni, h.bi, h.pairs, h.dirty = map[string]uint32{}, map[string]map[string]uint32{}, 0, true
	h.mu.Unlock()
}

// SaveIfDirty записывает файл, если были изменения.
func (h *History) SaveIfDirty() error {
	h.mu.Lock()
	if !h.dirty {
		h.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(historyFile{Version: 1, Uni: h.uni, Bi: h.bi})
	h.dirty = false
	h.mu.Unlock()
	if err != nil {
		return err
	}
	return config.WriteAtomic(h.path, b)
}
