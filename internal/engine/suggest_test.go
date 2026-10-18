package engine

import (
	"path/filepath"
	"testing"
	"time"

	"erised/internal/keymap"
	"erised/internal/predict"
)

func newSimPred(t *testing.T, app string, start keymap.Lang) *sim {
	s := newSim(t, app, start)
	s.e.SetPredictor(&predict.Predictor{Hist: predict.NewHistory(filepath.Join(t.TempDir(), "typing.json")), Store: s.st})
	return s
}

func (s *sim) items() []string {
	var out []string
	for _, it := range s.e.Suggestions().Items {
		out = append(out, it.Text)
	}
	return out
}

func TestAutocorrectTypo(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("пртвет как дела ", keymap.RU) // т вместо и: соседние клавиши
	expect(t, s.text(), "привет как дела ")
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	s2.typeAs("I recieve teh letter ", keymap.EN)
	expect(t, s2.text(), "I receive the letter ")
}

func TestAutocorrectKeepsPunctuationAndCase(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("Пртвет, мир ", keymap.RU)
	expect(t, s.text(), "Привет, мир ")
}

func TestAutocorrectUndoByBackspace(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("пртвет ", keymap.RU)
	expect(t, s.text(), "привет ")
	s.press(keymap.VK_BACK, Mods{})
	expect(t, s.text(), "пртвет ")
	if s.layout != keymap.RU {
		t.Fatal("отмена исправления опечатки не должна менять раскладку")
	}
	s.typeAs("пртвет ", keymap.RU)
	expect(t, s.text(), "пртвет пртвет ")
}

func TestAutocorrectLeavesRealWords(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	in := "мы диктуем отважную историю и получаешься ты "
	s.typeAs(in, keymap.RU)
	expect(t, s.text(), in)
	s2 := newSimPred(t, "notepad.exe", keymap.RU)
	s2.typeAs("вчера видел Пртвета ", keymap.RU) // имя посреди предложения не трогаем
	expect(t, s2.text(), "вчера видел Пртвета ")
}

func TestSuggestOnlyMode(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.cfg.Defaults.Autocorrect = ptrS("suggest")
	s.e.SetConfig(s.cfg)
	s.typeAs("пртвет", keymap.RU)
	if it := s.items(); len(it) == 0 || it[0] != "привет" {
		t.Fatalf("первым должно быть исправление: %v", it)
	}
	if s.e.Suggestions().AutoFirst {
		t.Fatal("в режиме «только подсказывать» исправление не применяется само")
	}
	s.typeAs(" ", keymap.RU)
	expect(t, s.text(), "пртвет ")
}

func TestCompletionTab(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("здравс", keymap.RU)
	it := s.items()
	if len(it) == 0 || it[0] != "здравствуйте" {
		t.Fatalf("варианты: %v", it)
	}
	s.press(keymap.VK_TAB, Mods{})
	expect(t, s.text(), "здравствуйте ")
	// Регистр как у набранного.
	s.typeAs("Спас", keymap.RU)
	s.press(keymap.VK_TAB, Mods{})
	expect(t, s.text(), "здравствуйте Спасибо ")
}

func TestTabWithoutSuggestionsIsTab(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.press(keymap.VK_TAB, Mods{})
	expect(t, s.text(), "\t")
}

func TestNextWordPrediction(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	for i := 0; i < 2; i++ {
		s.typeAs("доброе утро\n", keymap.RU)
	}
	s.typeAs("доброе ", keymap.RU)
	it := s.items()
	if len(it) == 0 || it[0] != "утро" {
		t.Fatalf("следующее слово: %v", it)
	}
	// Tab для следующего слова по умолчанию не перехватывается…
	if !s.e.Suggestions().NextWord {
		t.Fatal("должно быть отмечено как следующее слово")
	}
	// …а Ctrl+1 принимает.
	s.press('1', Mods{LCtrl: true})
	expect(t, s.text(), "доброе утро\nдоброе утро\nдоброе утро ")
}

func TestAcceptByClick(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	s.typeAs("tomor", keymap.EN)
	v := s.e.Suggestions()
	if len(v.Items) == 0 {
		t.Fatal("нет вариантов")
	}
	if res := s.e.AcceptSuggestion(0, v.Version+1, s.ctx()); len(res.Actions) != 0 {
		t.Fatal("устаревший набор вариантов нельзя принять")
	}
	s.apply(s.e.AcceptSuggestion(0, v.Version, s.ctx()), nil, 0)
	expect(t, s.text(), v.Items[0].Text+" ")
}

func TestEscHidesSuggestions(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("здравс", keymap.RU)
	s.press(keymap.VK_ESCAPE, Mods{})
	if len(s.items()) != 0 {
		t.Fatal("Esc должен прятать подсказки")
	}
}

func TestNoSuggestionsInTerminal(t *testing.T) {
	s := newSimPred(t, "WindowsTerminal.exe", keymap.EN)
	s.typeAs("teh recieve ", keymap.EN)
	expect(t, s.text(), "teh recieve ")
	s.typeAs("comm", keymap.EN)
	if len(s.items()) != 0 {
		t.Fatal("в терминале подсказок по умолчанию нет")
	}
}

func TestLayoutFixStillWorksWithPredictor(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.e.SetConfig(s.cfg)
	s.typeAs("ghbdtn rfr ltkf ", keymap.EN)
	expect(t, s.text(), "привет как дела ")
}

// Подсказки считаются в потоке перехвата клавиш — это должно быть быстро.
func TestSuggestSpeed(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("прогрев ", keymap.RU)
	start := time.Now()
	n := 0
	for i := 0; i < 20; i++ {
		for _, w := range []string{"по", "пос", "посм", "посмо", "пртвет", "здравствуйте", "сеглдня"} {
			s.e.Reset()
			s.typeAs(w, keymap.RU)
			n += len([]rune(w))
		}
	}
	per := time.Since(start) / time.Duration(n)
	t.Logf("в среднем %v на нажатие", per)
	if per > 3*time.Millisecond {
		t.Fatalf("слишком медленно: %v на нажатие", per)
	}
}

func TestBaseNextWords(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.e.pred.SetUseBase(true)
	s.typeAs("может ", keymap.RU)
	it := s.items()
	if len(it) == 0 || it[0] != "быть" {
		t.Fatalf("после «может» из стартовой базы ожидалось «быть»: %v", it)
	}
	// Ваша история важнее базы.
	for i := 0; i < 3; i++ {
		s.typeAs("может пригодиться\n", keymap.RU)
	}
	s.typeAs("может ", keymap.RU)
	if it := s.items(); len(it) == 0 || it[0] != "пригодиться" {
		t.Fatalf("ваша пара должна быть первой: %v", it)
	}
}

// Автоматические исправления сообщают, что было набрано, — для вспышки у курсора.
func TestFlashFrom(t *testing.T) {
	s := newSimPred(t, "notepad.exe", keymap.RU)
	s.typeAs("пртвет ", keymap.RU)
	s2 := newSimPred(t, "notepad.exe", keymap.EN)
	s2.typeAs("ghbdtn ", keymap.EN)
	var got []string
	for _, a := range append(s.actions, s2.actions...) {
		if a.Kind == ActReplace && a.From != "" {
			got = append(got, a.From+"→"+a.Text)
		}
	}
	if len(got) < 2 || got[0] != "пртвет→привет" {
		t.Fatalf("вспышки: %q", got)
	}
	// Отмена (Backspace сразу после) — без вспышки.
	n := len(s.actions)
	s.press(keymap.VK_BACK, Mods{})
	for _, a := range s.actions[n:] {
		if a.From != "" {
			t.Fatalf("у отмены не должно быть вспышки: %+v", a)
		}
	}
}
