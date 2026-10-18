package predict

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"erised/internal/keymap"
	"erised/internal/lang"
)

func neighbors(r rune, l keymap.Lang) []rune {
	var out []rune
	for q := range runePos[l] {
		if q != r && adjacent(r, q, l) && lang.Get(l) != nil {
			if _, ok := lang.Core(string(q), l); ok {
				out = append(out, q)
			}
		}
	}
	return out
}

func typo(w string, l keymap.Lang, rng *rand.Rand) string {
	rs := []rune(w)
	i := rng.Intn(len(rs))
	switch rng.Intn(5) {
	case 0, 1: // соседняя клавиша
		nb := neighbors(rs[i], l)
		if len(nb) == 0 {
			return w
		}
		rs[i] = nb[rng.Intn(len(nb))]
	case 2: // пропуск
		rs = append(rs[:i], rs[i+1:]...)
	case 3: // перестановка
		if i == len(rs)-1 {
			i--
		}
		rs[i], rs[i+1] = rs[i+1], rs[i]
	case 4: // лишняя соседняя
		nb := neighbors(rs[i], l)
		if len(nb) == 0 {
			return w
		}
		rs = append(rs[:i+1], append([]rune{nb[rng.Intn(len(nb))]}, rs[i+1:]...)...)
	}
	return string(rs)
}

// Калибровка порогов на синтетических опечатках. Запуск:
//
//	ERISED_CALIBRATE=/путь/к/FrequencyWords/content/2018 go test ./internal/predict -run Calibrate -v
func TestCalibrateCorrection(t *testing.T) {
	data := os.Getenv("ERISED_CALIBRATE")
	if data == "" {
		t.Skip("задайте ERISED_CALIBRATE")
	}
	p := &Predictor{}
	for _, l := range []keymap.Lang{keymap.RU, keymap.EN} {
		m := lang.Get(l)
		m.PrepareSimilar()
		rng := rand.New(rand.NewSource(7))
		type res struct{ total, top1, auto, autoWrong int }
		var r res
		// Частые слова длиной от 4 букв (выборка).
		var pool []string
		for i := 0; i < m.Size(); i++ {
			w, z := m.At(i)
			if z >= 3.0 && len([]rune(w)) >= 4 {
				pool = append(pool, w)
			}
		}
		for k := 0; k < 3000; k++ {
			w := pool[rng.Intn(len(pool))]
			ty := typo(w, l, rng)
			if ty == w || p.Exists(l, ty) {
				continue
			}
			r.total++
			c := p.Correct(l, ty, "", 3)
			if len(c) > 0 && c[0].Word == w {
				r.top1++
			}
			d := DecideAuto(ty, c, DefaultOptions)
			if d.OK {
				r.auto++
				if d.Best.Word != w {
					r.autoWrong++
				}
			}
		}
		// Редкие правильные слова вне словаря.
		f, err := os.Open(data + "/" + l.String() + "/" + l.String() + "_full.txt")
		if err != nil {
			t.Skip("нет данных")
		}
		sc := bufio.NewScanner(f)
		line, n, fp := 0, 0, 0
		var ex []string
		for sc.Scan() {
			line++
			if line < 100000 {
				continue
			}
			w := strings.Fields(sc.Text())[0]
			if c, ok := lang.Core(w, l); !ok || c != w || len([]rune(w)) < 4 || p.Exists(l, w) {
				continue
			}
			n++
			d := DecideAuto(w, p.Correct(l, w, "", 3), DefaultOptions)
			if d.OK {
				fp++
				if len(ex) < 15 {
					ex = append(ex, w+"→"+d.Best.Word)
				}
			}
			if n >= 5000 {
				break
			}
		}
		fmt.Printf("%s: опечаток %d, верный первый вариант %.1f%%, исправлено само %.1f%% (из них неверно %.1f%%); редкие правильные слова: испорчено %d из %d (%.1f%%) %v\n",
			l, r.total, 100*float64(r.top1)/float64(r.total), 100*float64(r.auto)/float64(r.total), 100*float64(r.autoWrong)/float64(max(r.auto, 1)), fp, n, 100*float64(fp)/float64(n), ex)
	}
}
