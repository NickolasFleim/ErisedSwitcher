package predict

import (
	"testing"

	"erised/internal/keymap"
)

func TestMisspellLike(t *testing.T) {
	yes := [][3]string{{"recieve", "receive", "en"}, {"definately", "definitely", "en"}, {"seperate", "separate", "en"},
		{"occured", "occurred", "en"}, {"wich", "which", "en"}, {"goverment", "government", "en"}, {"tommorow", "tomorrow", "en"},
		{"thier", "their", "en"}, {"becuase", "because", "en"}, {"извените", "извините", "ru"}, {"чесно", "честно", "ru"}}
	no := [][3]string{{"висели", "видели", "ru"}, {"упрямо", "прямо", "ru"}, {"начато", "начать", "ru"}, {"обязательное", "обязательно", "ru"},
		{"суете", "свете", "ru"}, {"путался", "пытался", "ru"}, {"столпилось", "столпились", "ru"}, {"welcomes", "welcome", "en"},
		{"leans", "means", "en"}, {"potting", "putting", "en"}, {"fueling", "feeling", "en"}, {"охватит", "хватит", "ru"}}
	for _, c := range yes {
		if !MisspellLike(c[0], c[1], keymap.ParseLang(c[2])) {
			t.Errorf("%s → %s должно считаться ошибкой написания", c[0], c[1])
		}
	}
	for _, c := range no {
		if MisspellLike(c[0], c[1], keymap.ParseLang(c[2])) {
			t.Errorf("%s → %s не должно считаться ошибкой написания", c[0], c[1])
		}
	}
}
