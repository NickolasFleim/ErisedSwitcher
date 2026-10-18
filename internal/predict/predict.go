package predict

import (
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/words"
)

// Kind — что за подсказка.
type Kind int

const (
	// Completion — дополнение набираемого слова.
	Completion Kind = iota
	// Correction — исправление опечатки.
	Correction
	// Next — следующее слово (по вашей истории).
	Next
)

func (k Kind) String() string {
	switch k {
	case Correction:
		return "correction"
	case Next:
		return "next"
	}
	return "completion"
}

// Suggestion — вариант слова (в нижнем регистре; регистр подстраивается при вставке).
type Suggestion struct {
	Word  string  `json:"word"`
	Kind  Kind    `json:"-"`
	Type  string  `json:"kind"`
	Score float64 `json:"score"`
	Zipf  float64 `json:"zipf"`
	Dist  float64 `json:"dist,omitempty"`
	User  bool    `json:"user,omitempty"` // ваше слово
}

// Predictor — подсказки по словарю, вашим словам и истории набора.
type Predictor struct {
	Hist  *History
	Store *words.Store
	// useBase — учитывать стартовую базу частых сочетаний слов (меняется из окна настроек).
	useBase atomic.Bool
}

// SetUseBase включает стартовую базу частых сочетаний слов.
func (p *Predictor) SetUseBase(v bool) { p.useBase.Store(v) }

// Вес вашей истории в оценке.
const (
	userWeight   = 0.6 // за каждое удвоение числа наборов слова
	pairWeight   = 1.0 // за каждое удвоение числа раз, когда слово шло после предыдущего
	userWordZipf = 3.5 // частотность, которую считаем у ваших слов, которых нет в словаре
	distWeight   = 3.0 // штраф за единицу расстояния правки
)

func (p *Predictor) boost(l keymap.Lang, prev, w string) float64 {
	b := 0.0
	if p.useBase.Load() {
		b += baseBoost(l, prev, w)
	}
	if p.Hist == nil {
		return b
	}
	b += userWeight * math.Log2(1+float64(p.Hist.Count(l, w)))
	if prev != "" {
		b += pairWeight * math.Log2(1+float64(p.Hist.Pair(l, prev, w)))
	}
	return b
}

// Exists — слово существует (в большом списке форм или среди ваших слов): такое не исправляем.
func (p *Predictor) Exists(l keymap.Lang, w string) bool {
	if m := lang.Get(l); m != nil && m.Exists(w) {
		return true
	}
	return p.Store != nil && p.Store.IsWord(l, w)
}

// Known — слово есть в словаре или среди ваших слов.
func (p *Predictor) Known(l keymap.Lang, w string) bool {
	if m := lang.Get(l); m != nil {
		if _, ok := m.Zipf(w); ok {
			return true
		}
	}
	return p.Store != nil && p.Store.IsWord(l, w)
}

type topN struct {
	n    int
	list []Suggestion
}

func (t *topN) add(s Suggestion) {
	for i := range t.list {
		if t.list[i].Word == s.Word {
			if s.Score > t.list[i].Score {
				t.list[i] = s
				sort.Slice(t.list, func(a, b int) bool { return t.list[a].Score > t.list[b].Score })
			}
			return
		}
	}
	if len(t.list) < t.n {
		t.list = append(t.list, s)
	} else if s.Score > t.list[len(t.list)-1].Score {
		t.list[len(t.list)-1] = s
	} else {
		return
	}
	sort.Slice(t.list, func(a, b int) bool { return t.list[a].Score > t.list[b].Score })
}

// Complete — самые вероятные слова, начинающиеся с prefix (без самого prefix).
func (p *Predictor) Complete(l keymap.Lang, prefix, prev string, n int) []Suggestion {
	m := lang.Get(l)
	if m == nil || prefix == "" || n <= 0 {
		return nil
	}
	t := topN{n: n}
	lo, hi := m.PrefixRange(prefix)
	// Для коротких префиксов слов тысячи: смотрим только частые, но историю учитываем всегда.
	for i := lo; i < hi; i++ {
		w, z := m.At(i)
		if w == prefix {
			continue
		}
		s := float64(z)
		if len(t.list) == t.n && s+6 < t.list[len(t.list)-1].Score {
			continue // даже с историей не догонит
		}
		s += p.boost(l, prev, w)
		t.add(Suggestion{Word: w, Kind: Completion, Score: s, Zipf: float64(z)})
	}
	if p.Store != nil {
		p.Store.EachUserWord(l, prefix, func(w string) bool {
			if w != prefix {
				z := userWordZipf
				if zz, ok := m.Zipf(w); ok {
					z = float64(zz)
				}
				t.add(Suggestion{Word: w, Kind: Completion, Score: z + p.boost(l, prev, w), Zipf: z, User: true})
			}
			return true
		})
	}
	return t.list
}

// Correct — словарные слова, похожие на набранное word (с учётом соседних клавиш),
// лучшие первыми. Пусто, если ничего похожего нет.
func (p *Predictor) Correct(l keymap.Lang, word, prev string, n int) []Suggestion {
	m := lang.Get(l)
	if m == nil || word == "" || n <= 0 {
		return nil
	}
	t := topN{n: n}
	consider := func(w string, z float64, user bool) {
		if w == word {
			return
		}
		d := Distance(word, w, l)
		if d > maxDistance {
			return
		}
		t.add(Suggestion{Word: w, Kind: Correction, Score: z - distWeight*d + p.boost(l, prev, w), Zipf: z, Dist: d, User: user})
	}
	for _, id := range m.Similar(word) {
		w, z := m.At(int(id))
		consider(w, float64(z), false)
	}
	if p.Store != nil {
		wl := len([]rune(word))
		p.Store.EachUserWord(l, "", func(w string) bool {
			if d := len([]rune(w)) - wl; d >= -2 && d <= 2 {
				z := userWordZipf
				if zz, ok := m.Zipf(w); ok {
					z = float64(zz)
				}
				consider(w, z, true)
			}
			return true
		})
	}
	return t.list
}

// NextWords — слова, которые вы чаще всего набирали после prev; если ваших мало —
// добавляются частые сочетания из стартовой базы (ваши всегда первыми).
func (p *Predictor) NextWords(l keymap.Lang, prev string, n int, minCount uint32) []Suggestion {
	if prev == "" || n <= 0 {
		return nil
	}
	var out []Suggestion
	seen := map[string]bool{}
	if p.Hist != nil {
		for _, wc := range p.Hist.Next(l, prev, n, minCount) {
			out = append(out, Suggestion{Word: wc.Word, Kind: Next, Score: 1000 + float64(wc.Count)})
			seen[wc.Word] = true
		}
	}
	if p.useBase.Load() && len(out) < n {
		if b := base(l); b != nil {
			for _, wc := range b.next[prev] {
				if len(out) >= n {
					break
				}
				if !seen[wc.Word] {
					out = append(out, Suggestion{Word: wc.Word, Kind: Next, Score: float64(wc.Count)})
				}
			}
		}
	}
	return out
}

// CorrectOptions — когда исправлять автоматически.
type CorrectOptions struct {
	MinLen       int     // не исправлять слова короче
	MaxDistShort float64 // наибольшее расстояние для слов до 5 букв
	MaxDistLong  float64 // для слов от 6 букв
	MinZipf      float64 // исправление должно быть не реже этого
	Margin       float64 // и лучше второго варианта хотя бы на столько
	// Осторожно с окончаниями: не исправлять, если слово отличается только последними
	// двумя буквами и само выглядит естественно (скорее всего, это другая форма слова).
	EndingCaution bool
	// Оценка «естественности» набранного слова (CharScore); нужна для EndingCaution.
	TypedNaturalness float64
	// Набранное слово есть в словаре, но редкое (частая ошибка написания: recieve, извените).
	// Тогда исправляем, только если правильный вариант частотнее хотя бы на MisspellGap
	// и отличается не больше чем на одну ошибку.
	TypedZipf   float64
	MisspellGap float64
	Lang        keymap.Lang // язык слова (для проверки, похоже ли отличие на ошибку написания)
}

// MisspellMaxZipf — слово из словаря с такой или меньшей частотностью может быть частой ошибкой.
const MisspellMaxZipf = 3.0

// AutoDecision — решение об автоисправлении с объяснением.
type AutoDecision struct {
	OK     bool       `json:"ok"`
	Best   Suggestion `json:"best"`
	Reason string     `json:"reason"`
}

// DecideAuto решает, достаточно ли уверенно исправление, чтобы применить его без спроса.
func DecideAuto(word string, cands []Suggestion, o CorrectOptions) AutoDecision {
	if len(cands) == 0 {
		return AutoDecision{Reason: "похожих слов нет"}
	}
	best := cands[0]
	d := AutoDecision{Best: best}
	n := len([]rune(word))
	maxD := o.MaxDistShort
	if n >= 6 {
		maxD = o.MaxDistLong
	}
	switch {
	case strings.ContainsAny(word, "-'"):
		d.Reason = "слово с дефисом или апострофом"
	case n < o.MinLen && !(n == 3 && o.MinLen == 4 && best.Dist <= 0.6+1e-9 && best.Zipf >= 5.5 &&
		(len(cands) < 2 || best.Score-cands[1].Score >= 2*o.Margin)):
		// Слова из трёх букв исправляем, только если это перестановка или соседняя клавиша
		// в очень частом слове (teh -> the).
		d.Reason = "слово короче минимальной длины"
	case best.Dist > maxD+1e-9:
		d.Reason = "лучший вариант слишком далёк"
	case best.Zipf < o.MinZipf && !best.User:
		d.Reason = "лучший вариант — редкое слово"
	case len(cands) > 1 && best.Score-cands[1].Score < o.Margin:
		d.Reason = "несколько похожих вариантов, выбрать уверенно нельзя"
	case o.TypedZipf > 0 && (best.Zipf-o.TypedZipf < o.MisspellGap || best.Dist > 1.0+1e-9):
		d.Reason = "набранное слово есть в словаре, а правильный вариант не намного частотнее"
	case extraEnding(word, best.Word):
		d.Reason = "набранное — это вариант с лишними буквами в конце: скорее всего, незнакомая форма слова"
	case firstLetterChanged(word, best.Word):
		d.Reason = "отличается первая буква: скорее другое слово, чем опечатка"
	case o.TypedZipf > 0 && !MisspellLike(word, best.Word, o.Lang):
		d.Reason = "набранное слово есть в словаре, и отличие не похоже на ошибку написания"
	case o.EndingCaution && endingOnly(word, best.Word) && o.TypedNaturalness > -1.6:
		d.Reason = "отличается только окончанием — возможно, другая форма слова"
	default:
		d.OK, d.Reason = true, "уверенное исправление опечатки"
	}
	return d
}

// MatchCase переносит регистр набранного слова на подсказку:
// «Прив» -> «Привет», «ПРИВ» -> «ПРИВЕТ».
func MatchCase(typed, word string) string {
	tr := []rune(typed)
	if len(tr) == 0 {
		return word
	}
	upper, letters := 0, 0
	for _, r := range tr {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters >= 2 && upper == letters {
		return strings.ToUpper(word)
	}
	if unicode.IsUpper(tr[0]) {
		return Capitalize(word)
	}
	return word
}

// Capitalize — первая буква заглавная.
func Capitalize(w string) string {
	rs := []rune(w)
	if len(rs) > 0 {
		rs[0] = unicode.ToUpper(rs[0])
	}
	return string(rs)
}

// DefaultOptions — значения по умолчанию (подобраны на синтетических опечатках).
var DefaultOptions = CorrectOptions{MinLen: 4, MaxDistShort: 1.0, MaxDistLong: 1.6, MinZipf: 2.5, Margin: 1.0}

// endingOnly — слова совпадают везде, кроме двух последних букв (и общая часть не короче 4 букв).
func endingOnly(typed, cand string) bool {
	a, b := []rune(typed), []rune(cand)
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i >= 4 && i >= len(a)-2 && i >= len(b)-2
}

// extraEnding — набранное слово = подсказка + 1–3 буквы (премьерами → премьера, удачах → удача):
// это скорее форма слова, которой нет в словаре, чем опечатка.
func extraEnding(typed, best string) bool {
	return len(typed) > len(best) && strings.HasPrefix(typed, best) && utf8.RuneCountInString(typed)-utf8.RuneCountInString(best) <= 3
}

// firstLetterChanged — отличается первая буква (хохоте → охоте, иррегулярных → регулярных).
// Перестановку двух первых букв (hte → the) опечаткой считаем.
func firstLetterChanged(typed, best string) bool {
	a, b := []rune(typed), []rune(best)
	if len(a) == 0 || len(b) == 0 || a[0] == b[0] {
		return false
	}
	return !(len(a) > 1 && len(b) > 1 && a[0] == b[1] && a[1] == b[0])
}
