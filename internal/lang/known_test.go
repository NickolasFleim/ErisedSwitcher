package lang

import (
	"testing"

	"erised/internal/keymap"
)

func TestExists(t *testing.T) {
	ru, en := Get(keymap.RU), Get(keymap.EN)
	for _, w := range []string{"диктуем", "отважную", "получаешься", "привет"} {
		if !ru.Exists(w) {
			t.Errorf("%s должно существовать", w)
		}
	}
	for _, w := range []string{"undescribable", "between"} {
		if !en.Exists(w) {
			t.Errorf("%s должно существовать", w)
		}
	}
	miss := 0
	for _, w := range []string{"првиетщ", "ывапролд", "qxzvbtyr", "ршщзхъ", "зщшгнек"} {
		if ru.Exists(w) || en.Exists(w) {
			miss++
		}
	}
	if miss > 1 {
		t.Errorf("слишком много ложных «существует»: %d", miss)
	}
}
