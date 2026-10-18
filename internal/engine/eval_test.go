package engine

// Замер на настоящих предложениях (Universal Dependencies): ERISED_EVAL=<папка с .conllu> go test -run TestEval -v
// Печатает, сколько слов программа испортила в правильно набранном тексте и сколько восстановила
// в тексте, набранном не в той раскладке.

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"erised/internal/keymap"
)

func evalSentences(t *testing.T, glob string, l keymap.Lang, max int) []string {
	files, _ := filepath.Glob(glob)
	var out []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() && len(out) < max {
			line := sc.Text()
			if !strings.HasPrefix(line, "# text = ") {
				continue
			}
			s := strings.TrimSpace(strings.TrimPrefix(line, "# text = "))
			ok := len([]rune(s)) >= 10
			for _, r := range s {
				if _, k := keymap.KeyForRune(r, l); !k {
					ok = false
					break
				}
			}
			if ok && !strings.Contains(s, "  ") {
				out = append(out, s)
			}
		}
		fh.Close()
	}
	return out
}

var evalDump *os.File

func wordDiff(want, got string) (bad, total int) {
	w, g := strings.Fields(want), strings.Fields(got)
	total = len(w)
	for i := range w {
		if i >= len(g) || g[i] != w[i] {
			bad++
			if evalDump != nil {
				gi := ""
				if i < len(g) {
					gi = g[i]
				}
				evalDump.WriteString(w[i] + "\t" + gi + "\n")
			}
		}
	}
	return
}

func TestEval(t *testing.T) {
	dir := os.Getenv("ERISED_EVAL")
	if dir == "" {
		t.Skip("ERISED_EVAL не задан")
	}
	sets := []struct {
		name string
		glob string
		l    keymap.Lang
	}{
		{"RU", filepath.Join(dir, "UD_Russian-SynTagRus", "*-test.conllu"), keymap.RU},
		{"EN", filepath.Join(dir, "UD_English-GUM", "*-test.conllu"), keymap.EN},
	}
	if p := os.Getenv("ERISED_EVAL_OUT"); p != "" {
		evalDump, _ = os.Create(p)
		defer evalDump.Close()
	}
	for _, set := range sets {
		sents := evalSentences(t, set.glob, set.l, 1500)
		var fpBad, fpTot, fixBad, fixTot, fpSent, fixSent int
		var fpEx, fixEx []string
		for _, s := range sents {
			// 1. Правильная раскладка.
			sm := newSimPred(t, "notepad.exe", set.l)
			sm.typeAs(s+" ", set.l)
			b, n := wordDiff(s, sm.text())
			fpBad, fpTot = fpBad+b, fpTot+n
			if b > 0 {
				fpSent++
				if len(fpEx) < 15 {
					fpEx = append(fpEx, s+"  ⇒  "+sm.text())
				}
			}
			// 2. Не та раскладка с начала фразы.
			other := keymap.EN
			if set.l == keymap.EN {
				other = keymap.RU
			}
			sm2 := newSimPred(t, "notepad.exe", other)
			sm2.typeAs(s+" ", set.l)
			b, n = wordDiff(s, sm2.text())
			fixBad, fixTot = fixBad+b, fixTot+n
			if b > 0 {
				fixSent++
				if len(fixEx) < 15 {
					fixEx = append(fixEx, s+"  ⇒  "+sm2.text())
				}
			}
		}
		t.Logf("%s: предложений %d", set.name, len(sents))
		t.Logf("  правильная раскладка: испорчено слов %d из %d (%.2f%%), предложений с ошибкой %d", fpBad, fpTot, 100*float64(fpBad)/float64(fpTot), fpSent)
		t.Logf("  не та раскладка: не восстановлено слов %d из %d (%.2f%%), предложений с ошибкой %d", fixBad, fixTot, 100*float64(fixBad)/float64(fixTot), fixSent)
		for _, e := range fpEx {
			t.Logf("    испорчено: %s", e)
		}
		for _, e := range fixEx {
			t.Logf("    не восстановлено: %s", e)
		}
	}
}
