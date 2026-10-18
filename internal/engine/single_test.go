package engine

import (
	"testing"

	"erised/internal/keymap"
)

func TestSingleLetterWords(t *testing.T) {
	cases := []struct {
		typed, want, app string
		start            keymap.Lang
	}{
		{"z nt,z ktyz ", "я тебя леня ", "notepad.exe", keymap.EN}, // я в начале
		{"d ujhjl ", "в город ", "notepad.exe", keymap.EN},
		{"c ytq ", "с ней ", "notepad.exe", keymap.EN},
		{"ghbdtn b gjrf ", "привет и пока ", "notepad.exe", keymap.EN},
		{"I am ", "I am ", "notepad.exe", keymap.EN},     // английское I не трогаем
		{"a cat ", "a cat ", "notepad.exe", keymap.EN},   // и a
		{"plan b ", "plan b ", "notepad.exe", keymap.EN}, // после английского слова
		{"vitamin c ", "vitamin c ", "notepad.exe", keymap.EN},
		{"z ", "z ", "windowsterminal.exe", keymap.EN}, // в терминале одна буква в начале — команда
	}
	for _, c := range cases {
		s := newSimPred(t, c.app, c.start)
		s.typeAs(c.typed, keymap.EN)
		if got := s.text(); got != c.want {
			t.Errorf("%q в %s: %q, ожидалось %q", c.typed, c.app, got, c.want)
		}
	}
	// В русской раскладке: ш → I в начале фразы, в → остаётся.
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("ш ", keymap.RU)
	expect(t, s.text(), "i ")
	s2 := newSimPred(t, "notepad.exe", keymap.RU)
	s2.typeAs("в доме ", keymap.RU)
	expect(t, s2.text(), "в доме ")
}
