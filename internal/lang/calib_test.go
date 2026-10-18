package lang

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"erised/internal/keymap"
)

// toKeys: слово в языке l -> нажатия
func toKeys(w string, l keymap.Lang) ([]keymap.Key, bool) {
	var ks []keymap.Key
	for _, r := range w {
		k, ok := keymap.KeyForRune(r, l)
		if !ok {
			return nil, false
		}
		ks = append(ks, k)
	}
	return ks, true
}

func TestCalibrate(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	for _, l := range []keymap.Lang{keymap.RU, keymap.EN} {
		m, o := Get(l), Get(l.Other())
		var good, bad, margin []float64
		collide, total := 0, 0
		rng := rand.New(rand.NewSource(1))
		for i, w := range m.sorted {
			_ = i
			if rng.Intn(10) != 0 {
				continue
			}
			z := m.words[w]
			if z < 2.0 || len([]rune(w)) < 4 {
				continue
			}
			ks, ok := toKeys(w, l)
			if !ok {
				continue
			}
			wrong := keymap.RenderAll(ks, l.Other())
			total++
			if c, ok := Core(wrong, l.Other()); ok {
				if _, in := o.words[c]; in {
					collide++
				}
				g := m.CharScore(w, true)
				b := o.CharScore(c, true)
				good = append(good, g)
				bad = append(bad, b)
				margin = append(margin, g-b)
			}
		}
		pct := func(a []float64, p float64) float64 {
			s := append([]float64(nil), a...)
			sort.Float64s(s)
			return s[int(p*float64(len(s)-1))]
		}
		fmt.Printf("%s: n=%d collide=%d\n good p5=%.2f p50=%.2f p95=%.2f\n wrong p5=%.2f p50=%.2f p95=%.2f p99=%.2f\n margin p1=%.2f p5=%.2f p50=%.2f\n", l, total, collide,
			pct(good, .05), pct(good, .5), pct(good, .95), pct(bad, .05), pct(bad, .5), pct(bad, .95), pct(bad, .99), pct(margin, .01), pct(margin, .05), pct(margin, .5))
	}
}
