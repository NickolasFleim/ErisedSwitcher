package engine

import (
	"os"
	"path/filepath"
	"testing"

	"erised/internal/keymap"
)

func TestEvalUK(t *testing.T) {
	dir := os.Getenv("ERISED_EVAL")
	if dir == "" {
		t.Skip()
	}
	withLanguages(t, keymap.RU, keymap.EN, keymap.RU, keymap.UK)
	sents := evalSentences(t, filepath.Join(dir, "UD_Ukrainian-IU", "*-test.conllu"), keymap.UK, 1000)
	var fb, ft, xb, xt int
	for _, s := range sents {
		sm := newSimPred(t, "notepad.exe", keymap.UK)
		sm.typeAs(s+" ", keymap.UK)
		b, n := wordDiff(s, sm.text())
		fb, ft = fb+b, ft+n
		sm2 := newSimPred(t, "notepad.exe", keymap.EN)
		sm2.typeAs(s+" ", keymap.UK)
		b, n = wordDiff(s, sm2.text())
		xb, xt = xb+b, xt+n
	}
	t.Logf("UK %d: правильная раскладка испорчено %d/%d (%.2f%%), не та раскладка не восстановлено %d/%d (%.2f%%)", len(sents), fb, ft, 100*float64(fb)/float64(ft), xb, xt, 100*float64(xb)/float64(xt))
}
