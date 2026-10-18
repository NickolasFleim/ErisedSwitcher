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

func TestPunctuationEdges(t *testing.T) {
	cases := [][2]string{
		{"ghbdtn. ", "привет. "},
		{"ghbvth. ", "примерю "}, // «примерю» — тоже слово, клавиши важнее
		{"ghbdtn. ", "привет. "},
		{"ghbvth. ", "примерю "}, // «примерю» — тоже слово, клавиши важнее
		{"\"ghbvth\" ", "\"пример\" "},
		{"(ghbvth) ", "(пример) "},
		{"ghbdtn. ", "привет. "},
		{"ghbvth. ", "примерю "}, // «примерю» — тоже слово, клавиши важнее
		{"lf, ", "да, "},         // короткое слово — посреди слова не переключается
		{"ghbdtn. ", "привет. "},
		{"ghbvth. ", "примерю "}, // «примерю» — тоже слово, клавиши важнее // ? — это русская запятая на той же клавише
		{"hello, ", "hello, "},
	}
	for _, c := range cases {
		s := newSimPred(t, "notepad.exe", keymap.EN)
		noEarly(s) // посреди слова раскладка уже переключилась бы, и знак набрался бы в русской
		s.typeAs(c[0], keymap.EN)
		if got := s.text(); got != c[1] {
			t.Errorf("%q: %q, ожидалось %q", c[0], got, c[1])
		}
	}
	// Отмена возвращает как было.
	s := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s)
	s.typeAs("ghbvth, ", keymap.EN)
	s.press(keymap.VK_BACK, Mods{})
	expect(t, s.text(), "ghbvth, ")
}

func TestNumbersDontBlockContext(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	s.typeAs("10 z ", keymap.EN) // число — не английское слово: «я» в начале фразы
	expect(t, s.text(), "10 я ")
}

func TestRetroShortWord(t *testing.T) {
	cases := [][2]string{
		{"ye crfpfk ", "ну сказал "}, // «ye» само не исправилось — исправляется вместе со следующим
		{",sk ujhjl ", "был город "},
		{"hello ye crfpfk ", "hello ye сказал "}, // после английского слова — не трогаем
	}
	for _, c := range cases {
		s := newSimPred(t, "notepad.exe", keymap.EN)
		noEarly(s)
		s.typeAs(c[0], keymap.EN)
		if got := s.text(); got != c[1] {
			t.Errorf("%q: %q, ожидалось %q", c[0], got, c[1])
		}
	}
	// Backspace возвращает обе части.
	s := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s)
	s.typeAs("ye crfpfk ", keymap.EN)
	s.press(keymap.VK_BACK, Mods{})
	expect(t, s.text(), "ye crfpfk ")
}

func TestRetroWithEarlySwitch(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN) // посреди слова переключение включено
	s.typeAs("Ye? crfpfk ", keymap.EN)
	expect(t, s.text(), "Ну, сказал ")
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	s2.typeAs("ye crfp", keymap.EN) // «ye» исправилось вместе с началом следующего слова
	expect(t, s2.text(), "ну сказ")
	s2.press(keymap.VK_BACK, Mods{})
	expect(t, s2.text(), "ye crfp")
}

func TestEdgeLettersMakeWord(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s)
	s.typeAs("<sk ujhjl ", keymap.EN)
	expect(t, s.text(), "Был город ")
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	s2.typeAs("it. so, ", keymap.EN) // английские слова со знаками не трогаем
	expect(t, s2.text(), "it. so, ")
}
