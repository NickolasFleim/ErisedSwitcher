package engine

import (
	"testing"

	"erised/internal/keymap"
)

// seed — как будто щёлкнули мышью после уже написанного текста.
func (s *sim) seed(text string, afterWord bool) bool {
	s.e.Reset()
	s.screen = []rune(text)
	return s.e.SeedContext(s.e.Epoch(), 1, text, afterWord)
}

func TestSeedConvertWord(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	if !s.seed("Написал ghbdtn", false) {
		t.Fatal("контекст не принят")
	}
	s.doubleShift()
	expect(t, s.text(), "Написал привет")
}

func TestSeedConvertPhrase(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.seed("ok  ghbdtn rfr ltkf", false)
	s.press(keymap.VK_PAUSE, Mods{LShift: true})
	expect(t, s.text(), "ok  привет как дела")
}

func TestSeedNotAutoCorrected(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.seed("ghbdtn", false)
	s.press(keymap.VK_SPACE, Mods{})
	expect(t, s.text(), "ghbdtn ") // написанное раньше само не исправляем
}

func TestSeedAfterSpaceGivesPrevWord(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.seed("ghbdtn ", false)
	s.doubleShift() // последнее слово — до пробела
	expect(t, s.text(), "привет ")
}

func TestSeedRejected(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	if s.seed("ghbd", true) {
		t.Fatal("курсор посреди слова — нельзя")
	}
	s.e.Reset()
	ep := s.e.Epoch()
	s.typeAs("a", keymap.EN)
	if s.e.SeedContext(ep, 1, "ghbdtn", false) {
		t.Fatal("после набора контекст устарел")
	}
}
