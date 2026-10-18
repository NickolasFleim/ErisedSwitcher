package engine

import (
	"testing"

	"erised/internal/keymap"
)

func collect(s *sim) *[]StatEvent {
	var ev []StatEvent
	s.e.SetStats(func(x StatEvent) { ev = append(ev, x) })
	return &ev
}

func kinds(ev []StatEvent) []string {
	var out []string
	for _, x := range ev {
		out = append(out, x.Kind+":"+x.From+">"+x.To)
	}
	return out
}

func TestStatsEvents(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	noEarly(s)
	ev := collect(s)
	s.typeAs("ghbdtn ", keymap.EN)  // исправлено
	s.press(keymap.VK_BACK, Mods{}) // отменено
	s.typeAs("vbh ", keymap.EN)     // «мир»: короткое после английского — не исправится
	got := kinds(*ev)
	if len(got) < 2 || got[0] != "layout:ghbdtn>привет" || got[1] != "undo_layout:ghbdtn>привет" {
		t.Fatalf("события: %q", got)
	}
	// Пропуск: слово, которое вы сами сконвертировали.
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	ev2 := collect(s2)
	s2.typeAs("qwrt", keymap.EN)
	s2.doubleShift()
	if g := kinds(*ev2); len(g) != 1 || g[0] != "manual:qwrt>йцке" {
		t.Fatalf("ручная: %q", g)
	}
}

func TestObserveMode(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	mode := "observe"
	s.cfg.Defaults.Mode = &mode
	s.e.SetConfig(s.cfg)
	ev := collect(s)
	s.typeAs("ghbdtn ", keymap.EN)
	expect(t, s.text(), "ghbdtn ") // ничего не меняется
	s2 := newSimPred(t, "notepad.exe", keymap.RU)
	s2.cfg.Defaults.Mode = &mode
	s2.e.SetConfig(s2.cfg)
	ev2 := collect(s2)
	s2.typeAs("пртвет ", keymap.RU)
	expect(t, s2.text(), "пртвет ")
	g := append(kinds(*ev), kinds(*ev2)...)
	if len(g) != 2 || g[0] != "would_layout:ghbdtn>привет" || g[1] != "would_typo:пртвет>привет" {
		t.Fatalf("наблюдение: %q", g)
	}
}
