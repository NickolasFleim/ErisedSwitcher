package lang

import (
	"encoding/binary"
	"sort"
	"strings"
)

// PrefixRange — индексы [lo, hi) слов словаря, начинающихся с p (в алфавитном порядке).
func (m *Model) PrefixRange(p string) (lo, hi int) {
	lo = sort.SearchStrings(m.sorted, p)
	// Верхняя граница: первое слово, которое больше любой строки с префиксом p.
	hi = lo + sort.Search(len(m.sorted)-lo, func(i int) bool { return !strings.HasPrefix(m.sorted[lo+i], p) })
	return lo, hi
}

// At — слово и его частотность по индексу.
func (m *Model) At(i int) (string, float32) { return m.sorted[i], m.sortedZ[i] }

// SimIndexMinZipf — слова реже этого не предлагаются как исправление опечаток.
const SimIndexMinZipf = 1.8

func fnv(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// deletes — строка и все варианты с одной удалённой буквой (по рунам).
func deletes1(rs []rune, out []string) []string {
	out = append(out, string(rs))
	for i := range rs {
		out = append(out, string(rs[:i])+string(rs[i+1:]))
	}
	return out
}

// buildSim строит компактный индекс SymSpell: хеш каждого слова и его вариантов
// без одной буквы -> номер слова. Хеши в одном отсортированном массиве (~12 байт на запись).
func (m *Model) buildSim() {
	type pair struct {
		h  uint64
		id int32
	}
	pairs := make([]pair, 0, len(m.sorted)*6)
	var buf []string
	for i, w := range m.sorted {
		if m.sortedZ[i] < SimIndexMinZipf {
			continue
		}
		rs := []rune(w)
		if len(rs) < 2 {
			continue
		}
		buf = deletes1(rs, buf[:0])
		seen := map[uint64]bool{}
		for _, d := range buf {
			h := fnv(d)
			if !seen[h] {
				seen[h] = true
				pairs = append(pairs, pair{h, int32(i)})
			}
		}
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].h < pairs[b].h })
	m.simKeys = make([]uint64, len(pairs))
	m.simIDs = make([]int32, len(pairs))
	for i, p := range pairs {
		m.simKeys[i], m.simIDs[i] = p.h, p.id
	}
}

// PrepareSimilar строит индекс похожих слов (≈1 с). До этого Similar ничего не находит,
// чтобы не задерживать поток перехвата клавиш.
func (m *Model) PrepareSimilar() {
	m.simOnce.Do(m.buildSim)
	m.simReady.Store(true)
}

// Similar возвращает номера слов, которые могут отличаться от w на 1–2 правки:
// пересечение вариантов w без 0–2 букв с вариантами слов словаря без 0–1 буквы.
// Это кандидаты; настоящее расстояние нужно посчитать отдельно.
func (m *Model) Similar(w string) []int32 {
	if !m.simReady.Load() {
		return nil
	}
	rs := []rune(w)
	if len(rs) == 0 || len(rs) > 30 {
		return nil
	}
	vars := map[string]bool{w: true}
	for i := range rs {
		d1 := append(append([]rune{}, rs[:i]...), rs[i+1:]...)
		vars[string(d1)] = true
		for j := range d1 {
			vars[string(d1[:j])+string(d1[j+1:])] = true
		}
	}
	seen := map[int32]bool{}
	var out []int32
	for v := range vars {
		if v == "" {
			continue
		}
		h := fnv(v)
		k := sort.Search(len(m.simKeys), func(i int) bool { return m.simKeys[i] >= h })
		for ; k < len(m.simKeys) && m.simKeys[k] == h; k++ {
			if id := m.simIDs[k]; !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func (m *Model) loadKnown() {
	b, err := dataFS.ReadFile("data/" + m.Lang.String() + ".known")
	if err != nil || len(b) < 12 || string(b[:4]) != "EKB1" {
		return
	}
	bits := uint64(binary.LittleEndian.Uint32(b[4:8]))
	k := int(binary.LittleEndian.Uint32(b[8:12]))
	data := b[12:]
	if bits == 0 || uint64(len(data))*8 < bits {
		return
	}
	m.known = make([]uint64, (len(data)+7)/8)
	for i := range m.known {
		var chunk [8]byte
		copy(chunk[:], data[i*8:])
		m.known[i] = binary.LittleEndian.Uint64(chunk[:])
	}
	m.knownBits, m.knownK = bits, k
}

// Exists — встречалась ли такая форма слова в большом корпусе (словарь подсказок
// или список всех форм; ~1% ложных «да»). Нужна, чтобы не «исправлять» правильные слова.
func (m *Model) Exists(w string) bool {
	if _, ok := m.Zipf(w); ok {
		return true
	}
	if m.knownBits == 0 {
		return false
	}
	h1 := fnv(w)
	h2 := (h1 >> 33) | 1
	for i := 0; i < m.knownK; i++ {
		idx := (h1 + uint64(i)*h2) % m.knownBits
		if m.known[idx>>6]&(1<<(idx&63)) == 0 {
			return false
		}
	}
	return true
}
