package predict

import (
	"strings"
	"unicode/utf8"

	"erised/internal/keymap"
)

// Гласные, которые часто путают на письме: извЕните/извИните, сепЕрейт/сепАрейт, дефинАтли/дефинИтли.
var vowelConfusions = map[keymap.Lang][]string{
	keymap.RU: {"еи", "оа", "ея"},
	keymap.UK: {"еи", "иі", "оа", "еє"},
	keymap.EN: {"ae", "ai", "ei"},
}

// MisspellLike — похоже ли отличие словарного слова typed от частого слова best на ошибку
// написания (recieve → receive, извените → извините, goverment → government), а не на
// другое слово или другую форму (висели/видели, упрямо/прямо, начато/начать, welcomes/welcome).
// Замер на настоящих текстах показал, что без этой проверки исправлялись правильные редкие слова.
func MisspellLike(typed, best string, l keymap.Lang) bool {
	a, b := []rune(typed), []rune(best)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	da, db := a[p:len(a)-s], b[p:len(b)-s]
	transposed := len(da) == 2 && len(db) == 2 && da[0] == db[1] && da[1] == db[0]
	// Первая буква и конец слова: там отличия — это уже другое слово или другая форма
	// (кроме перестановки букв: teh → the).
	if p == 0 || (s == 0 && !transposed) {
		return false
	}
	switch {
	case len(da) == 1 && len(db) == 1: // замена буквы
		x, y := da[0], db[0]
		// Соседняя клавиша в словарном слове — скорее другое слово (суете/свете), не ошибка.
		if (x == 'е' && y == 'ё') || (x == 'ё' && y == 'е') {
			return true
		}
		for _, pair := range vowelConfusions[l] {
			if strings.ContainsRune(pair, x) && strings.ContainsRune(pair, y) {
				return true
			}
		}
		return false
	case transposed:
		return true
	case len(da)+len(db) == 1: // лишняя или пропущенная буква посреди слова
		return true
	}
	// Несколько удвоений: tommorow → tomorrow.
	return undouble(typed) == undouble(best)
}

func undouble(s string) string {
	var out strings.Builder
	var prev rune = utf8.RuneError
	for _, r := range s {
		if r != prev {
			out.WriteRune(r)
		}
		prev = r
	}
	return out.String()
}
