package engine

import (
	"strings"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/words"
)

// AnalyzeResult — разбор одного слова для вкладки «Проверка».
type AnalyzeResult struct {
	Word    string   `json:"word"`
	Typed   string   `json:"typed_in"` // в какой раскладке слово считается набранным
	Final   Verdict  `json:"final"`
	Early   *Verdict `json:"early,omitempty"` // на какой букве сработало бы «на лету»
	EarlyAt int      `json:"early_at,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Analyze показывает, что программа сделала бы с каждым словом текста,
// если бы оно было набрано так, как написано, в окне процесса process.
func Analyze(text, process string, cfg *config.Config, st *words.Store) []AnalyzeResult {
	r := cfg.Resolve(process, "", "")
	var out []AnalyzeResult
	for _, w := range strings.Fields(text) {
		res := AnalyzeResult{Word: w}
		l := keymap.DetectTextLang(w)
		if l == keymap.Unknown {
			l = keymap.EN
		}
		res.Typed = l.String()
		var keys []keymap.Key
		for _, ch := range w {
			k, ok := keymap.KeyForRune(ch, l)
			if !ok {
				res.Error = "символ «" + string(ch) + "» нельзя набрать в раскладке " + l.String()
				break
			}
			keys = append(keys, k)
		}
		if res.Error == "" {
			res.Final = Decide(keys, l, &r, st, true, false)
			if r.EarlySwitch {
				for i := r.EarlyMinLen; i < len(keys); i++ {
					if i < 1 {
						continue
					}
					v := Decide(keys[:i], l, &r, st, false, false)
					if v.Switch {
						res.Early, res.EarlyAt = &v, i
						break
					}
				}
			}
		}
		out = append(out, res)
	}
	return out
}
