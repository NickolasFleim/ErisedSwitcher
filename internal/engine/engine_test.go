package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/words"
)

// sim — эмулятор «экрана»: применяет действия ядра к тексту, как это сделала бы Windows.
type sim struct {
	t      *testing.T
	e      *Engine
	st     *words.Store
	cfg    *config.Config
	screen []rune
	layout keymap.Lang
	app    string
	time   uint32
	caps   bool
	// все действия ядра (для проверки конвертации выделения)
	actions []Action
}

func newSim(t *testing.T, app string, start keymap.Lang) *sim {
	lang.Preload()
	for _, l := range []keymap.Lang{keymap.RU, keymap.EN} {
		lang.Get(l).PrepareSimilar()
	}
	cfg := config.Default()
	st := words.New(filepath.Join(t.TempDir(), "words.json"))
	return &sim{t: t, e: New(cfg, st), st: st, cfg: cfg, layout: start, app: app}
}

func (s *sim) ctx() *Context {
	return &Context{Window: 1, App: s.app, Layout: s.layout, Rule: s.cfg.Resolve(s.app, "", "")}
}

func (s *sim) apply(res Result, k *keymap.Key, vk uint16) {
	s.actions = append(s.actions, res.Actions...)
	for _, a := range res.Actions {
		switch a.Kind {
		case ActReplace:
			if a.Backspaces > len(s.screen) {
				s.t.Fatalf("стирается больше, чем напечатано: %d > %q", a.Backspaces, string(s.screen))
			}
			s.screen = s.screen[:len(s.screen)-a.Backspaces]
			if a.Target != keymap.Unknown {
				s.layout = a.Target
				s.e.NoteLayoutSwitch(a.Target)
			}
			s.screen = append(s.screen, []rune(a.Text)...)
			for _, t := range a.Trail {
				s.screen = append(s.screen, trailRune(t))
			}
		case ActSwitchLayout:
			s.layout = a.Target
		}
	}
	if res.Swallow {
		return
	}
	if vk == keymap.VK_BACK {
		if len(s.screen) > 0 {
			s.screen = s.screen[:len(s.screen)-1]
		}
		return
	}
	if vk == keymap.VK_SPACE || vk == keymap.VK_RETURN || vk == keymap.VK_TAB {
		s.screen = append(s.screen, trailRune(vk))
		return
	}
	if k != nil {
		if r := keymap.Render(*k, s.layout); r != 0 {
			s.screen = append(s.screen, r)
		}
	}
}

func trailRune(vk uint16) rune {
	switch vk {
	case keymap.VK_RETURN:
		return '\n'
	case keymap.VK_TAB:
		return '\t'
	}
	return ' '
}

func (s *sim) press(vk uint16, mods Mods) {
	s.time += 50
	ctx := s.ctx()
	res := s.e.HandleKey(KeyEvent{VK: vk, Down: true, Mods: mods, Caps: s.caps, Time: s.time}, ctx)
	var k *keymap.Key
	if keymap.IsPrintable(vk) {
		k = &keymap.Key{VK: vk, Shift: mods.Shift(), Caps: s.caps}
	}
	s.apply(res, k, vk)
	s.time += 10
	res = s.e.HandleKey(KeyEvent{VK: vk, Down: false, Mods: mods, Caps: s.caps, Time: s.time}, s.ctx())
	s.apply(res, nil, 0)
}

// typeAs печатает текст так, как его набрал бы человек, глядя на клавиши раскладки l
// (то есть физические клавиши для текста в раскладке l), независимо от того, какая раскладка включена.
func (s *sim) typeAs(text string, l keymap.Lang) {
	for _, r := range text {
		switch r {
		case ' ':
			s.press(keymap.VK_SPACE, Mods{})
			continue
		case '\n':
			s.press(keymap.VK_RETURN, Mods{})
			continue
		}
		k, ok := keymap.KeyForRune(r, l)
		if !ok {
			s.t.Fatalf("нельзя набрать %q в %v", r, l)
		}
		s.press(k.VK, Mods{LShift: k.Shift})
	}
}

func (s *sim) doubleShift() {
	for i := 0; i < 2; i++ {
		s.time += 40
		s.apply(s.e.HandleKey(KeyEvent{VK: keymap.VK_LSHIFT, Down: true, Time: s.time}, s.ctx()), nil, 0)
		s.time += 40
		s.apply(s.e.HandleKey(KeyEvent{VK: keymap.VK_LSHIFT, Down: false, Time: s.time}, s.ctx()), nil, 0)
	}
}

func (s *sim) text() string { return string(s.screen) }

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("на экране %q, ожидалось %q", got, want)
	}
}

func TestAutoFixRussianTypedInEnglish(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("привет как дела ", keymap.RU) // руки на русских буквах, раскладка английская
	expect(t, s.text(), "привет как дела ")
	if s.layout != keymap.RU {
		t.Fatal("раскладка должна стать русской")
	}
}

func TestAutoFixEnglishTypedInRussian(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.RU)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("hello world ", keymap.EN)
	expect(t, s.text(), "hello world ")
}

func TestEarlySwitch(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.typeAs("здравствуйте", keymap.RU)
	expect(t, s.text(), "здравствуйте")
	if s.layout != keymap.RU {
		t.Fatal("раскладка должна переключиться посреди слова")
	}
}

func TestCorrectWordsUntouched(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.typeAs("the quick brown fox jumps over the lazy dog ", keymap.EN)
	expect(t, s.text(), "the quick brown fox jumps over the lazy dog ")
	s2 := newSim(t, "notepad.exe", keymap.RU)
	s2.typeAs("съешь же ещё этих мягких французских булок да выпей чаю ", keymap.RU)
	expect(t, s2.text(), "съешь же ещё этих мягких французских булок да выпей чаю ")
}

func TestPunctuationKeysAreRussianLetters(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("любовь, жизнь и объявление. ", keymap.RU)
	expect(t, s.text(), "любовь, жизнь и объявление. ")
}

func TestCapitalLetters(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("Москва ", keymap.RU)
	expect(t, s.text(), "Москва ")
	s.caps = true
	s.typeAs("ghbdtn ", keymap.EN) // с CapsLock
	expect(t, s.text(), "Москва ПРИВЕТ ")
}

func TestUndoByBackspaceLearnsException(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("ghbdtn ", keymap.EN) // по-русски «привет», но допустим, вы хотели именно ghbdtn
	expect(t, s.text(), "привет ")
	s.press(keymap.VK_BACK, Mods{})
	expect(t, s.text(), "ghbdtn ")
	if s.layout != keymap.EN {
		t.Fatal("после отмены раскладка должна вернуться")
	}
	if !s.st.IsException(keymap.EN, "ghbdtn") {
		t.Fatal("слово должно попасть в исключения")
	}
	s.typeAs("ghbdtn ", keymap.EN)
	expect(t, s.text(), "ghbdtn ghbdtn ")
	// Повторного сбора нет.
	d := s.st.Snapshot()
	n := 0
	for _, e := range d.Learned {
		if e.Word == "ghbdtn" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("слово собрано %d раз", n)
	}
}

func TestBackspaceAfterTypingDoesNotUndo(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("ghbdtn ", keymap.EN)
	s.typeAs("rf", keymap.EN) // «ка»
	s.press(keymap.VK_BACK, Mods{})
	expect(t, s.text(), "привет к") // раскладка уже русская, «rf» напечаталось как «ка»
}

func TestManualDoubleShift(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.Mode = ptrS("manual")
	s.typeAs("ytn", keymap.EN)
	s.doubleShift()
	expect(t, s.text(), "нет")
	s.doubleShift() // повторно — вернуть
	expect(t, s.text(), "ytn")
	s.doubleShift()
	expect(t, s.text(), "нет")
}

func TestManualConvertAfterSpace(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.Mode = ptrS("manual")
	s.typeAs("ytn ", keymap.EN)
	s.doubleShift()
	expect(t, s.text(), "нет ")
}

func TestManualConvertLearnsWord(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.RU)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	// «kubectl» в русской раскладке: «лгиусед» — по словарю не исправится (DictionaryOnly в терминале).
	s.app = "WindowsTerminal.exe"
	s.typeAs("kubectl", keymap.EN)
	expect(t, s.text(), "лгиусед")
	s.doubleShift()
	expect(t, s.text(), "kubectl")
	if !s.st.IsWord(keymap.EN, "kubectl") {
		t.Fatal("kubectl должно стать своим словом")
	}
	// Теперь исправится само.
	s.typeAs(" kubectl ", keymap.EN)
	// раскладка после ручной конвертации — EN, так что второе слово уже правильное
	expect(t, s.text(), "kubectl kubectl ")
	s.layout = keymap.RU
	s.typeAs("kubectl ", keymap.EN)
	expect(t, s.text(), "kubectl kubectl kubectl ")
}

func TestPhraseConversion(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.Mode = ptrS("manual")
	s.typeAs("ghbdtn vbh", keymap.EN)
	s.press(0x13, Mods{LShift: true}) // Shift+Pause
	expect(t, s.text(), "привет мир")
}

func TestTerminalCommandsUntouched(t *testing.T) {
	s := newSim(t, "WindowsTerminal.exe", keymap.EN)
	s.typeAs("git commit -m fix\n", keymap.EN)
	s.typeAs("ls -la /usr/bin\n", keymap.EN)
	s.typeAs("cd ~/projects && npm run build\n", keymap.EN)
	expect(t, s.text(), "git commit -m fix\nls -la /usr/bin\ncd ~/projects && npm run build\n")
}

func TestTerminalFixesWrongLayout(t *testing.T) {
	s := newSim(t, "WindowsTerminal.exe", keymap.RU)
	s.typeAs("git status\n", keymap.EN) // набрано в русской раскладке: «пше ыефегы»
	expect(t, s.text(), "git status\n")
	s.layout = keymap.EN
	s.typeAs("echo ghbdtn\n", keymap.EN)
	expect(t, s.text(), "git status\necho привет\n")
}

func TestShortWords(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.cfg.Defaults.EarlySwitch = ptrB(false)
	s.typeAs("yt pyf. ", keymap.EN) // «не знаю»
	expect(t, s.text(), "не знаю ")
	s2 := newSim(t, "notepad.exe", keymap.EN)
	s2.typeAs("i am ok so it is ", keymap.EN)
	expect(t, s2.text(), "i am ok so it is ")
}

func TestDigitsAndEmailSkipped(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.RU)
	s.typeAs("ыуксуе123 ", keymap.RU)
	expect(t, s.text(), "ыуксуе123 ")
}

func TestHotkeyParse(t *testing.T) {
	for _, h := range []string{"DoubleShift", "DoubleRCtrl", "Pause", "Shift+Pause", "Ctrl+Alt+Z", "RCtrl", "F12", "Ctrl+Shift+C", "", "none"} {
		if _, err := config.ParseHotkey(h); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	for _, h := range []string{"Shift", "Ctrl+Foo", "DoubleQ", "Ctrl+"} {
		if _, err := config.ParseHotkey(h); err == nil {
			t.Errorf("%q должна быть ошибкой", h)
		}
	}
}

func TestAutoReplace(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.RU)
	d := s.st.Snapshot()
	d.AutoReplace = []words.Replace{{From: "спс", To: "спасибо"}, {From: "СПС", To: "дубль"}}
	s.st.SetData(d)
	if n := len(s.st.Snapshot().AutoReplace); n != 1 {
		t.Fatalf("повтор автозамены не убран: %d", n)
	}
	s.typeAs("спс ", keymap.RU)
	expect(t, s.text(), "спасибо ")
}

func TestPassiveLearning(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	for i := 0; i < 3; i++ {
		s.typeAs("zxqwv ", keymap.EN)
	}
	if !s.st.IsWord(keymap.EN, "zxqwv") {
		t.Fatal("после 3 повторов слово должно быть собрано")
	}
	for i := 0; i < 3; i++ {
		s.typeAs("zxqwv ", keymap.EN)
	}
	d := s.st.Snapshot()
	if len(d.Candidates) != 0 || len(d.Learned) != 1 {
		t.Fatalf("повторы при сборе: кандидатов %d, собранных %d", len(d.Candidates), len(d.Learned))
	}
}

func TestPerAppOff(t *testing.T) {
	s := newSim(t, "mstsc.exe", keymap.EN)
	s.typeAs("ghbdtn ", keymap.EN)
	expect(t, s.text(), "ghbdtn ")
}

func TestToggleEnabled(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.press(0x03, Mods{LCtrl: true, LAlt: true}) // Ctrl+Alt+Pause
	if s.e.Enabled() {
		t.Fatal("должно выключиться")
	}
	s.typeAs("ghbdtn ", keymap.EN)
	expect(t, s.text(), "ghbdtn ")
}

func TestConvertText(t *testing.T) {
	if got := keymap.ConvertText("Ghbdtn? rfr ltkf/", keymap.EN); got != "Привет, как дела." {
		t.Fatalf("got %q", got)
	}
	if got := keymap.ConvertText("руддщ цщкдв", keymap.RU); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestAnalyze(t *testing.T) {
	cfg := config.Default()
	res := Analyze("ghbdtn привет", "notepad.exe", cfg, nil, nil)
	if len(res) != 2 || !res[0].Final.Switch || res[1].Final.Switch {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res[0].Final.Alt, "привет") {
		t.Fatal(res[0].Final.Alt)
	}
}

func ptrB(b bool) *bool     { return &b }
func ptrS(s string) *string { return &s }

func (s *sim) selectionActions() []Action {
	var out []Action
	for _, a := range s.actions {
		if a.Kind == ActConvertSelection {
			out = append(out, a)
		}
	}
	return out
}

// Ваш случай: двойной Shift на «конвертировать выделение» (и он же на «конвертировать слово»).
func TestDoubleShiftConvertsSelection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		word, sel string
	}{
		{"по умолчанию (Shift дважды — слово)", "DoubleShift", "Ctrl+Shift+Pause"},
		{"обе на двойном Shift", "DoubleShift", "DoubleShift"},
		{"только выделение на двойном Shift", "", "DoubleShift"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSim(t, "notepad.exe", keymap.EN)
			s.cfg.Hotkeys.ConvertWord, s.cfg.Hotkeys.ConvertSelection = tc.word, tc.sel
			if errs := s.cfg.Validate(); len(errs) > 0 {
				t.Fatalf("настройки отклонены: %v", errs)
			}
			s.e.SetConfig(s.cfg)

			// Выделение мышью (протяжка) -> двойной Shift.
			s.e.Reset()
			s.e.NoteSelection(true)
			s.doubleShift()
			if n := len(s.selectionActions()); n != 1 {
				t.Fatalf("после выделения мышью: действий с выделением %d", n)
			}
			// Shift+стрелки -> двойной Shift.
			s.actions = nil
			s.typeAs("ghbdtn", keymap.EN)
			s.press(keymap.VK_LEFT, Mods{LShift: true})
			s.doubleShift()
			if n := len(s.selectionActions()); n != 1 {
				t.Fatalf("после Shift+стрелки: действий с выделением %d", n)
			}
			// Ctrl+A -> двойной Shift.
			s.actions = nil
			s.press('A', Mods{LCtrl: true})
			s.doubleShift()
			if n := len(s.selectionActions()); n != 1 {
				t.Fatalf("после Ctrl+A: действий с выделением %d", n)
			}
			// Просто набранное слово (выделения нет) -> конвертируется слово.
			s.actions, s.screen, s.layout = nil, nil, keymap.EN
			s.e.Reset()
			s.cfg.Defaults.Mode = ptrS("manual")
			s.e.SetConfig(s.cfg)
			s.typeAs("ghbdtn", keymap.EN)
			s.doubleShift()
			if n := len(s.selectionActions()); n != 0 {
				t.Fatalf("без выделения не должно быть конвертации выделения")
			}
			if tc.word != "" || tc.sel == "DoubleShift" {
				expect(t, s.text(), "привет")
			}
		})
	}
}

func TestSelectionClearedByTyping(t *testing.T) {
	s := newSim(t, "notepad.exe", keymap.EN)
	s.press('A', Mods{LCtrl: true})
	if !s.e.Selection() {
		t.Fatal("Ctrl+A должен считаться выделением")
	}
	s.typeAs("x", keymap.EN)
	if s.e.Selection() {
		t.Fatal("набор снимает выделение")
	}
	s.press(keymap.VK_RIGHT, Mods{LShift: true})
	s.press(keymap.VK_RIGHT, Mods{})
	if s.e.Selection() {
		t.Fatal("стрелка без Shift снимает выделение")
	}
}

func TestHotkeyConflicts(t *testing.T) {
	c := config.Default()
	c.Hotkeys.Undo = "DoubleShift" // уже занято «Конвертировать слово»
	errs := c.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0], "Отменить") {
		t.Fatalf("ожидался конфликт: %v", errs)
	}
	c = config.Default()
	c.Hotkeys.ConvertSelection = "DoubleShift" // разрешено: умная клавиша
	if errs := c.Validate(); len(errs) != 0 {
		t.Fatalf("слово+выделение на одной клавише должно быть можно: %v", errs)
	}
}
