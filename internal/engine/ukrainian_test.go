package engine

import (
	"testing"

	"erised/internal/keymap"
)

// withLanguages включает языки на время теста.
func withLanguages(t *testing.T, pref keymap.Lang, ls ...keymap.Lang) {
	keymap.SetLanguages(ls)
	keymap.SetPreferredCyrillic(pref)
	t.Cleanup(func() {
		keymap.SetLanguages([]keymap.Lang{keymap.RU})
		keymap.SetPreferredCyrillic(keymap.RU)
	})
}

func noEarly(s *sim) {
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.e.SetConfig(s.cfg)
}

func TestUkrainianTypedInRussianLayout(t *testing.T) {
	withLanguages(t, keymap.RU, keymap.RU, keymap.UK)
	s := newSimPred(t, "notepad.exe", keymap.RU)
	noEarly(s)
	s.typeAs("дівчина ", keymap.UK) // клавиши украинского слова, раскладка русская: «дывчина»
	expect(t, s.text(), "дівчина ")
	if s.layout != keymap.UK {
		t.Fatalf("раскладка должна стать украинской, а она %v", s.layout)
	}
}

func TestUkrainianTypedInEnglishLayout(t *testing.T) {
	withLanguages(t, keymap.RU, keymap.RU, keymap.UK)
	s := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s)
	s.typeAs("дівчина ", keymap.UK)
	expect(t, s.text(), "дівчина ")
	if s.layout != keymap.UK {
		t.Fatalf("раскладка %v", s.layout)
	}
	// А русское — в русскую.
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s2)
	s2.typeAs("привет ", keymap.RU)
	expect(t, s2.text(), "привет ")
	if s2.layout != keymap.RU {
		t.Fatalf("раскладка %v", s2.layout)
	}
}

func TestEnglishTypedInUkrainianLayout(t *testing.T) {
	withLanguages(t, keymap.UK, keymap.UK)
	s := newSimPred(t, "notepad.exe", keymap.UK)
	noEarly(s)
	s.typeAs("hello world ", keymap.EN)
	expect(t, s.text(), "hello world ")
}

func TestUkrainianApostrophe(t *testing.T) {
	withLanguages(t, keymap.RU, keymap.RU, keymap.UK)
	s := newSimPred(t, "notepad.exe", keymap.RU)
	noEarly(s)
	s.typeAs("п'ять ", keymap.UK) // в русской раскладке: «пёять»
	expect(t, s.text(), "п'ять ")
}

func TestSameWordInBothCyrillic(t *testing.T) {
	for _, pref := range []keymap.Lang{keymap.RU, keymap.UK} {
		withLanguages(t, pref, keymap.RU, keymap.UK)
		s := newSimPred(t, "notepad.exe", keymap.EN)
		noEarly(s)
		s.typeAs("мама ", keymap.RU) // «vfvf»
		expect(t, s.text(), "мама ")
		if s.layout != pref {
			t.Fatalf("одинаковое слово — в ту кириллицу, которой пользуетесь (%v), а стало %v", pref, s.layout)
		}
	}
}

func TestCorrectTextUntouchedWithThreeLanguages(t *testing.T) {
	withLanguages(t, keymap.RU, keymap.RU, keymap.UK)
	ru := "съешь же ещё этих мягких французских булок да выпей чаю "
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs(ru, keymap.RU)
	expect(t, s.text(), ru)
	uk := "я люблю свою країну і рідну мову, це наше життя "
	s2 := newSimPred(t, "notepad.exe", keymap.UK)
	s2.typeAs(uk, keymap.UK)
	expect(t, s2.text(), uk)
	en := "the quick brown fox jumps over the lazy dog "
	s3 := newSimPred(t, "notepad.exe", keymap.EN)
	s3.typeAs(en, keymap.EN)
	expect(t, s3.text(), en)
}

func TestManualCycleThreeLanguages(t *testing.T) {
	withLanguages(t, keymap.RU, keymap.RU, keymap.UK)
	s := newSimPred(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.Mode = ptrS("manual")
	s.e.SetConfig(s.cfg)
	// «вылез»: в русской — «вылез», в украинской — «вілез» (ы/і на одной клавише).
	s.typeAs("dsktp", keymap.EN)
	s.doubleShift()
	expect(t, s.text(), "вылез")
	s.doubleShift()
	expect(t, s.text(), "вілез")
	s.doubleShift()
	expect(t, s.text(), "dsktp")
	// Если в украинской слово выглядит так же — она пропускается.
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	s2.cfg.Defaults.Mode = ptrS("manual")
	s2.e.SetConfig(s2.cfg)
	s2.typeAs("ghbdtn", keymap.EN)
	s2.doubleShift()
	expect(t, s2.text(), "привет")
	s2.doubleShift()
	expect(t, s2.text(), "ghbdtn")
}

func TestConvertTextThreeLanguages(t *testing.T) {
	withLanguages(t, keymap.UK, keymap.RU, keymap.UK)
	if got := keymap.ConvertTextTo("lsdxbyf", keymap.EN, keymap.UK); got != "дівчина" {
		t.Fatalf("got %q", got)
	}
	if got := keymap.DetectTextLang("дівчина"); got != keymap.UK {
		t.Fatalf("язык %v", got)
	}
	if got := keymap.ConvertText("lsdxbyf", keymap.EN); got != "дівчина" {
		t.Fatalf("с английского — в основную кириллицу (UK): %q", got)
	}
}
