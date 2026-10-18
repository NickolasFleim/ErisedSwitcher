// Package predict — подсказки при наборе: дополнение слова, исправление опечаток
// и предсказание следующего слова по вашей истории набора.
package predict

import (
	"math"

	"erised/internal/keymap"
)

// Положение клавиш на клавиатуре (ряд, колонка с учётом сдвига рядов).
type pos struct{ x, y float64 }

var keyPos = map[uint16]pos{}

func init() {
	rows := []struct {
		shift float64
		keys  []uint16
	}{
		{0, []uint16{keymap.VK_OEM_3, '1', '2', '3', '4', '5', '6', '7', '8', '9', '0', keymap.VK_OEM_MINUS, keymap.VK_OEM_PLUS}},
		{1.5, []uint16{'Q', 'W', 'E', 'R', 'T', 'Y', 'U', 'I', 'O', 'P', keymap.VK_OEM_4, keymap.VK_OEM_6}},
		{1.75, []uint16{'A', 'S', 'D', 'F', 'G', 'H', 'J', 'K', 'L', keymap.VK_OEM_1, keymap.VK_OEM_7}},
		{2.25, []uint16{'Z', 'X', 'C', 'V', 'B', 'N', 'M', keymap.VK_OEM_COMMA, keymap.VK_OEM_PERIOD, keymap.VK_OEM_2}},
	}
	for y, r := range rows {
		for i, vk := range r.keys {
			keyPos[vk] = pos{r.shift + float64(i), float64(y)}
		}
	}
}

// runePos — где на клавиатуре буква r в раскладке l.
var runePos = [3]map[rune]pos{{}, {}, {}}

func init() {
	for _, l := range keymap.All {
		for vk, p := range keyPos {
			k := keymap.Key{VK: vk}
			if r := keymap.Render(k, l); r != 0 {
				runePos[l][r] = p
			}
		}
	}
}

func adjacent(a, b rune, l keymap.Lang) bool {
	if l < keymap.EN || l > keymap.UK {
		return false
	}
	pa, ok1 := runePos[l][a]
	pb, ok2 := runePos[l][b]
	if !ok1 || !ok2 {
		return false
	}
	dx, dy := pa.x-pb.x, pa.y-pb.y
	return dx*dx+dy*dy <= 1.6*1.6
}

// Стоимости правок. Ошибки, которые легко сделать пальцами, стоят дешевле.
const (
	costSub         = 1.0
	costSubAdjacent = 0.6  // соседняя клавиша
	costSubYo       = 0.1  // е вместо ё и наоборот
	costDel         = 1.0  // лишняя буква в набранном
	costDelDouble   = 0.5  // лишняя буква — повтор предыдущей (дребезг)
	costDelAdjacent = 0.6  // лишняя буква — задели соседнюю клавишу
	costIns         = 1.0  // пропущена буква
	costInsDouble   = 0.5  // пропущена удвоенная буква («клас» вместо «класс»)
	costTranspose   = 0.6  // переставлены соседние буквы
	maxDistance     = 2.01 // дальше не считаем
)

func subCost(a, b rune, l keymap.Lang) float64 {
	switch {
	case a == b:
		return 0
	case (a == 'е' && b == 'ё') || (a == 'ё' && b == 'е'):
		return costSubYo
	case l == keymap.UK && ((a == 'и' && b == 'і') || (a == 'і' && b == 'и') || (a == 'е' && b == 'є') || (a == 'є' && b == 'е')):
		return 0.4 // частая путаница в украинском
	case adjacent(a, b, l):
		return costSubAdjacent
	}
	return costSub
}

// Distance — взвешенное расстояние Дамерау–Левенштейна (оптимальное выравнивание строк)
// от набранного typed до слова target. Возвращает значение > 2, если слова далеки.
func Distance(typed, target string, l keymap.Lang) float64 {
	s, t := []rune(typed), []rune(target)
	n, m := len(s), len(t)
	if d := n - m; d > 2 || d < -2 {
		return math.Inf(1)
	}
	// Три строки таблицы: i-2, i-1, i.
	prev2 := make([]float64, m+1)
	prev := make([]float64, m+1)
	cur := make([]float64, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = insPrefix(t, j)
	}
	for i := 1; i <= n; i++ {
		del := costDel
		if i >= 2 && s[i-1] == s[i-2] {
			del = costDelDouble
		} else if (i >= 2 && adjacent(s[i-1], s[i-2], l)) || (i < n && adjacent(s[i-1], s[i], l)) {
			del = costDelAdjacent
		}
		cur[0] = prev[0] + del
		rowMin := cur[0]
		for j := 1; j <= m; j++ {
			ins := costIns
			if j >= 2 && t[j-1] == t[j-2] {
				ins = costInsDouble
			}
			v := prev[j] + del
			if x := cur[j-1] + ins; x < v {
				v = x
			}
			if x := prev[j-1] + subCost(s[i-1], t[j-1], l); x < v {
				v = x
			}
			if i >= 2 && j >= 2 && s[i-1] == t[j-2] && s[i-2] == t[j-1] && s[i-1] != s[i-2] {
				if x := prev2[j-2] + costTranspose; x < v {
					v = x
				}
			}
			cur[j] = v
			if v < rowMin {
				rowMin = v
			}
		}
		if rowMin > maxDistance {
			return math.Inf(1)
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[m]
}

func insPrefix(t []rune, j int) float64 {
	c := 0.0
	for k := 1; k <= j; k++ {
		if k >= 2 && t[k-1] == t[k-2] {
			c += costInsDouble
		} else {
			c += costIns
		}
	}
	return c
}
