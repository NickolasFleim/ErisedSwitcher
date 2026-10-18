package engine

import (
	"strings"

	"erised/internal/config"
	"erised/internal/keymap"
	"erised/internal/lang"
	"erised/internal/predict"
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
	// Исправление опечатки (если раскладка верная).
	Typo *TypoResult `json:"typo,omitempty"`
	// Что было бы в полоске подсказок, если набрать начало этого слова.
	Completions []string `json:"completions,omitempty"`
}

// TypoResult — что сделало бы исправление опечаток.
type TypoResult struct {
	Applied    bool     `json:"applied"` // исправит само при пробеле
	Best       string   `json:"best"`
	Reason     string   `json:"reason"`
	Candidates []string `json:"candidates"`
	Dist       float64  `json:"dist"`
}

// Analyze показывает, что программа сделала бы с каждым словом текста,
// если бы оно было набрано так, как написано, в окне процесса process.
func Analyze(text, process string, cfg *config.Config, st *words.Store, pred *predict.Predictor) []AnalyzeResult {
	r := cfg.Resolve(process, "", "")
	// Временный движок с теми же настройками — чтобы проверка шла тем же кодом, что и при наборе.
	tmp := &Engine{cfg: cfg, store: st, pred: pred, forced: keymap.Unknown}
	var out []AnalyzeResult
	prev := keymap.Unknown // язык предыдущего слова — как при наборе
	lang.Preload()
	for _, l := range keymap.All {
		lang.Get(l).PrepareSimilar() // здесь можно и подождать
	}
	for wi, w := range strings.Fields(text) {
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
			res.Final = Decide(keys, l, &r, st, true, prev)
			prev = l
			if res.Final.Switch {
				prev = res.Final.AltLang
			}
			if r.EarlySwitch {
				for i := r.EarlyMinLen; i < len(keys); i++ {
					if i < 1 {
						continue
					}
					v := Decide(keys[:i], l, &r, st, false, keymap.Unknown)
					if v.Switch {
						res.Early, res.EarlyAt = &v, i
						break
					}
				}
			}
			if !res.Final.Switch && pred != nil && r.Autocorrect != "off" {
				tmp.hist = nil
				if wi > 0 {
					tmp.hist = []token{{lang: l, keys: []keymap.Key{{VK: 'X'}}, trail: keymap.VK_SPACE}} // «не начало предложения»
				}
				t := token{keys: keys, lang: l}
				if low, tz, ok := tmp.canAutocorrect(&t, &r); ok {
					cands := pred.Correct(l, low, "", 3)
					d := predict.DecideAuto(low, cands, tmp.autoOptions(&r, low, l, tz))
					tr := &TypoResult{Applied: d.OK && r.Autocorrect == "auto", Reason: d.Reason, Dist: d.Best.Dist}
					if d.Best.Word != "" {
						tr.Best = predict.MatchCase(w, d.Best.Word)
					}
					for _, c := range cands {
						tr.Candidates = append(tr.Candidates, c.Word)
					}
					res.Typo = tr
				}
			}
			if pred != nil && r.Predict {
				lw := strings.ToLower(w)
				rs := []rune(lw)
				if n := r.PredictMinPrefix; len(rs) > n {
					for _, c := range pred.Complete(l, string(rs[:n+1]), "", cfg.Suggest.Count) {
						res.Completions = append(res.Completions, c.Word)
					}
				}
			}
		}
		out = append(out, res)
	}
	return out
}
