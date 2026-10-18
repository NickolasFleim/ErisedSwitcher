// Package lang — словари и статистическая модель букв для русского и английского.
package lang

import (
	"bufio"
	"compress/gzip"
	"embed"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"erised/internal/keymap"
)

//go:embed data/ru.txt.gz data/en.txt.gz data/uk.txt.gz data/ru.known data/en.known data/uk.known
var dataFS embed.FS

// Model — словарь одного языка и триграммная модель букв.
type Model struct {
	Lang   keymap.Lang
	words  map[string]float32 // слово -> zipf (log10 частоты на миллиард)
	sorted []string           // для поиска по префиксу
	tri    map[string]float32 // log10 P(c | две предыдущие)
	bi     map[string]float32 // log10 P(c | предыдущая)
	floor  float32

	sortedZ []float32 // zipf слов в порядке sorted (для быстрого дополнения)

	simOnce   sync.Once
	simReady  atomic.Bool
	known     []uint64 // фильтр Блума: все встречавшиеся формы слов
	knownBits uint64
	knownK    int
	simKeys   []uint64 // хеши «удалений» (SymSpell), отсортированы
	simIDs    []int32  // индекс слова в sorted для каждого хеша
}

var (
	once   sync.Once
	models [3]*Model
	ready  atomic.Bool
)

// Ready — словари уже загружены (до этого поток перехвата клавиш не должен их ждать).
func Ready() bool { return ready.Load() }

// Get возвращает модель языка (загружается один раз при первом обращении).
func Get(l keymap.Lang) *Model {
	once.Do(load)
	if l < keymap.EN || l > keymap.UK {
		return nil
	}
	return models[l]
}

// Preload загружает словари заранее, чтобы первое слово не ждало.
func Preload() { once.Do(load) }

func load() {
	var wg sync.WaitGroup
	for _, l := range keymap.All {
		wg.Add(1)
		go func(l keymap.Lang) {
			defer wg.Done()
			models[l] = loadModel(l)
		}(l)
	}
	wg.Wait()
	ready.Store(true)
}

func loadModel(l keymap.Lang) *Model {
	f, err := dataFS.Open("data/" + l.String() + ".txt.gz")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		panic(err)
	}
	m := &Model{Lang: l, words: make(map[string]float32, 160000)}
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		line := sc.Text()
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		z, err := strconv.ParseFloat(line[tab+1:], 32)
		if err != nil {
			continue
		}
		m.words[line[:tab]] = float32(z)
	}
	m.sorted = make([]string, 0, len(m.words))
	for w := range m.words {
		m.sorted = append(m.sorted, w)
	}
	sort.Strings(m.sorted)
	m.sortedZ = make([]float32, len(m.sorted))
	for i, w := range m.sorted {
		m.sortedZ[i] = m.words[w]
	}
	m.train()
	m.loadKnown()
	return m
}

// train строит модель букв. Вес слова — его zipf (а не сырая частота),
// чтобы модель описывала «как выглядят слова», а не «как выглядят 100 самых частых».
func (m *Model) train() {
	triC := map[string]float64{}
	ctx2 := map[string]float64{}
	biC := map[string]float64{}
	ctx1 := map[string]float64{}
	alphabet := map[rune]bool{}
	for w, z := range m.words {
		wt := float64(z)
		if wt < 0.5 {
			wt = 0.5
		}
		rs := append([]rune{'^', '^'}, []rune(w)...)
		rs = append(rs, '$')
		for i := 2; i < len(rs); i++ {
			alphabet[rs[i]] = true
			c2 := string(rs[i-2 : i])
			triC[string(rs[i-2:i+1])] += wt
			ctx2[c2] += wt
			biC[string(rs[i-1:i+1])] += wt
			ctx1[string(rs[i-1:i])] += wt
		}
	}
	v := float64(len(alphabet) + 1)
	const k = 0.05
	m.tri = make(map[string]float32, len(triC))
	for t, c := range triC {
		pre := string([]rune(t)[:2])
		m.tri[t] = float32(math.Log10((c + k) / (ctx2[pre] + k*v)))
	}
	m.bi = make(map[string]float32, len(biC))
	for b, c := range biC {
		pre := string([]rune(b)[:1])
		m.bi[b] = float32(math.Log10((c + k) / (ctx1[pre] + k*v)))
	}
	m.floor = float32(math.Log10(k / (1000 * v)))
}

// Zipf возвращает частотность слова (0, false — нет в словаре).
func (m *Model) Zipf(w string) (float32, bool) {
	z, ok := m.words[w]
	if !ok && strings.ContainsRune(w, '\'') {
		// В английском словаре слова записаны без апострофа: i'm → im, don't → dont.
		z, ok = m.words[strings.ReplaceAll(w, "'", "")]
	}
	return z, ok
}

// HasPrefix сообщает, начинается ли с p хотя бы одно слово словаря с частотой не ниже minZipf.
func (m *Model) HasPrefix(p string, minZipf float32) bool {
	i := sort.SearchStrings(m.sorted, p)
	for n := 0; i < len(m.sorted) && n < 400; i, n = i+1, n+1 {
		w := m.sorted[i]
		if !strings.HasPrefix(w, p) {
			return false
		}
		if m.words[w] >= minZipf {
			return true
		}
	}
	return false
}

// CharScore — средний log10 вероятности каждой буквы (чем ближе к 0, тем «естественнее»).
// complete=true добавляет маркер конца слова.
func (m *Model) CharScore(w string, complete bool) float64 {
	if strings.ContainsRune(w, '\'') {
		if _, ok := m.words[w]; !ok {
			w = strings.ReplaceAll(w, "'", "") // модель букв обучена без апострофов
		}
	}
	rs := append([]rune{'^', '^'}, []rune(w)...)
	if complete {
		rs = append(rs, '$')
	}
	n := len(rs) - 2
	if n <= 0 {
		return 0
	}
	sum := 0.0
	for i := 2; i < len(rs); i++ {
		if p, ok := m.tri[string(rs[i-2:i+1])]; ok {
			sum += float64(p)
			continue
		}
		// Откат к биграмме со штрафом.
		if p, ok := m.bi[string(rs[i-1:i+1])]; ok {
			sum += float64(p) - 1.0
			continue
		}
		sum += float64(m.floor)
	}
	return sum / float64(n)
}

// Size — число слов в словаре.
func (m *Model) Size() int { return len(m.words) }

// Core выделяет «ядро» слова: убирает знаки по краям, приводит к нижнему регистру.
// ok=false, если внутри ядра есть не-буквы (кроме дефиса и апострофа в английском и украинском).
// Украинский апостроф (’ или ʼ) приводится к обычному «'».
func Core(s string, l keymap.Lang) (string, bool) {
	rs := []rune(s)
	i, j := 0, len(rs)
	for i < j && !unicode.IsLetter(rs[i]) {
		i++
	}
	for j > i && !unicode.IsLetter(rs[j-1]) {
		j--
	}
	if i == j {
		return "", false
	}
	core := append([]rune(nil), rs[i:j]...)
	for k, r := range core {
		if unicode.IsLetter(r) {
			if l.Cyrillic() && !unicode.Is(unicode.Cyrillic, r) {
				return "", false
			}
			if l == keymap.EN && !unicode.Is(unicode.Latin, r) {
				return "", false
			}
			continue
		}
		if l == keymap.UK && (r == '’' || r == 'ʼ') {
			core[k] = '\''
			continue
		}
		if r == '-' || (r == '\'' && (l == keymap.EN || l == keymap.UK)) {
			continue
		}
		return "", false
	}
	return strings.ToLower(string(core)), true
}
