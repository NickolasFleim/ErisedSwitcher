package stats

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stats.json")
	s := Load(p)
	day := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return day }
	s.Add("layout", "a.exe", "ghbdtn", "привет", "en", true)
	s.Add("undo_layout", "a.exe", "ghbdtn", "привет", "en", true)
	s.Add("would_layout", "a.exe", "rjl", "код", "en", true)
	s.Add("manual", "a.exe", "qwe", "йцу", "en", false)
	sum := s.Summary(7)
	w := sum["week"].(Counts)
	if w.Layout != 1 || w.UndoLayout != 1 || w.WouldLayout != 1 || w.Manual != 1 {
		t.Fatalf("счётчики %+v", w)
	}
	ex := sum["examples"].([]Example)
	if len(ex) != 2 || ex[0].Kind != "would_layout" {
		t.Fatalf("примеры %+v", ex)
	}
	if e, ok := s.Mark(ex[0].ID, "wrong"); !ok || e.Verdict != "wrong" {
		t.Fatal("отметка")
	}
	s.Mark(ex[0].ID, "ok") // передумали
	if err := s.SaveIfDirty(); err != nil {
		t.Fatal(err)
	}
	s2 := Load(p)
	s2.now = s.now
	w2 := s2.Summary(7)["week"].(Counts)
	if w2.CheckedOK != 1 || w2.CheckedWrong != 0 || w2.Layout != 1 {
		t.Fatalf("после загрузки %+v", w2)
	}
}
