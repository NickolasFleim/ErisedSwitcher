package predict

import (
	"bufio"
	"compress/gzip"
	"embed"
	"math"
	"strconv"
	"strings"
	"sync"

	"erised/internal/keymap"
)

// Стартовая база пар слов (из корпусов Universal Dependencies под CC BY-SA 4.0, см. data/ATTRIBUTION.md):
// чтобы «следующее слово» подсказывалось с первого дня, пока ваша история набора пуста.
//
//go:embed data/en.pairs.gz data/ru.pairs.gz data/uk.pairs.gz
var baseFS embed.FS

type basePairs struct {
	once sync.Once
	next map[string][]WordCount // предыдущее -> следующие (по убыванию)
	pair map[string]uint32      // "пред\x00след" -> счётчик
}

var bases [3]basePairs

func base(l keymap.Lang) *basePairs {
	if l < keymap.EN || l > keymap.UK {
		return nil
	}
	b := &bases[l]
	b.once.Do(func() { b.load(l) })
	return b
}

func (b *basePairs) load(l keymap.Lang) {
	b.next, b.pair = map[string][]WordCount{}, map[string]uint32{}
	f, err := baseFS.Open("data/" + l.String() + ".pairs.gz")
	if err != nil {
		return
	}
	defer f.Close()
	g, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	sc := bufio.NewScanner(g)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) != 3 {
			continue
		}
		c, _ := strconv.Atoi(p[2])
		b.next[p[0]] = append(b.next[p[0]], WordCount{p[1], uint32(c)}) // файл уже по убыванию
		b.pair[p[0]+"\x00"+p[1]] = uint32(c)
	}
}

// basePair — сколько раз пара встретилась в стартовой базе.
func basePair(l keymap.Lang, prev, w string) uint32 {
	if b := base(l); b != nil {
		return b.pair[prev+"\x00"+w]
	}
	return 0
}

// baseBoost — небольшая прибавка к оценке за частое сочетание (меньше, чем за вашу историю).
func baseBoost(l keymap.Lang, prev, w string) float64 {
	if prev == "" {
		return 0
	}
	c := basePair(l, prev, w)
	if c == 0 {
		return 0
	}
	return math.Min(1.5, 0.25*math.Log2(1+float64(c)))
}
