// Package config — все настройки программы: глобальные, правила по приложениям,
// горячие клавиши. Хранятся в config.json рядом с данными пользователя.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rule — параметры распознавания и ввода. В глобальных настройках заданы все поля,
// в правиле приложения — только те, что нужно переопределить (остальные null).
type Rule struct {
	// "auto" — исправлять автоматически; "manual" — только по горячим клавишам; "off" — не трогать.
	Mode *string `json:"mode,omitempty"`

	// Минимальная длина слова (в буквах) для автоисправления по словарю.
	MinLen *int `json:"min_len,omitempty"`
	// Только по словарю: не исправлять слова, которых нет в словаре второго языка.
	DictionaryOnly *bool `json:"dictionary_only,omitempty"`
	// Для коротких слов (до 3 букв) — минимальная частотность (zipf) слова во втором языке.
	ShortWordMinZipf *float64 `json:"short_word_min_zipf,omitempty"`
	// Не исправлять, если текущее слово похоже на настоящее (оценка выше этой), даже если второй вариант — словарное слово.
	ShortWordCurMax *float64 `json:"short_word_cur_max,omitempty"`
	// Для слов от 4 букв: не исправлять, если текущее слово выглядит естественнее этой оценки.
	DictCurMax *float64 `json:"dict_cur_max,omitempty"`

	// Статистика букв (для слов, которых нет в словаре).
	NgramMinLen *int     `json:"ngram_min_len,omitempty"` // минимальная длина
	NgramCurMax *float64 `json:"ngram_cur_max,omitempty"` // текущее слово должно выглядеть хуже этого
	NgramAltMin *float64 `json:"ngram_alt_min,omitempty"` // второй вариант должен выглядеть лучше этого
	NgramMargin *float64 `json:"ngram_margin,omitempty"`  // и лучше текущего хотя бы на столько

	// Переключать, не дожидаясь конца слова.
	EarlySwitch *bool `json:"early_switch,omitempty"`
	// После скольких букв можно переключать на лету.
	EarlyMinLen *int `json:"early_min_len,omitempty"`

	// Какие клавиши завершают слово и запускают проверку.
	CorrectOnSpace *bool `json:"correct_on_space,omitempty"`
	CorrectOnEnter *bool `json:"correct_on_enter,omitempty"`
	CorrectOnTab   *bool `json:"correct_on_tab,omitempty"`

	// Не исправлять слова с цифрами.
	SkipWithDigits *bool `json:"skip_with_digits,omitempty"`
	// Не исправлять, если внутри слова (как оно видно на экране) есть эти символы, например "@/\\$=".
	// Не добавляйте сюда . , ; ' [ ] ` — в русской раскладке это буквы ю б ж э х ъ ё.
	SkipChars *string `json:"skip_chars,omitempty"`
	// Не исправлять первое слово строки (полезно для команд в терминале).
	SkipFirstWord *bool `json:"skip_first_word,omitempty"`

	// Backspace сразу после исправления отменяет его (и запоминает исключение).
	UndoByBackspace *bool `json:"undo_by_backspace,omitempty"`

	// Как печатать исправленный текст: "unicode" (символы напрямую, не зависит от раскладки)
	// или "keys" (повтор тех же клавиш после переключения раскладки).
	Inject *string `json:"inject,omitempty"`
	// Паузы при вводе, мс.
	BackspaceDelayMs *int `json:"backspace_delay_ms,omitempty"`
	KeyDelayMs       *int `json:"key_delay_ms,omitempty"`
	SwitchDelayMs    *int `json:"switch_delay_ms,omitempty"`

	// Переключать системную раскладку после исправления.
	SwitchLayout *bool `json:"switch_layout,omitempty"`
	// Как узнавать текущую раскладку: "auto", "focus" (поток окна), "console" (API консоли), "tracked" (запоминать самому).
	LayoutSource *string `json:"layout_source,omitempty"`

	// Конвертация выделенного текста: разрешена ли и какой комбинацией копировать.
	SelectionAllowed *bool   `json:"selection_allowed,omitempty"`
	CopyCombo        *string `json:"copy_combo,omitempty"`
	// После конвертации выделения снова выделить результат (тогда повторное нажатие вернёт как было).
	ReselectAfterConvert *bool `json:"reselect_after_convert,omitempty"`

	// Собирать слова, которые вы набираете и не исправляете.
	LearnPassive *bool `json:"learn_passive,omitempty"`

	// Слова через пробел, которые в этом приложении всегда правильные (например, команды
	// терминала: git cd ls). Набранные не в той раскладке, они исправляются всегда.
	PreferWords *string `json:"prefer_words,omitempty"`

	// Исправлять однобуквенные слова (z → я), если предыдущее слово фразы на другом языке.
	SingleLetterContext *bool `json:"single_letter_context,omitempty"`

	// Исправление опечаток: "auto" — исправлять при пробеле, "suggest" — только подсказывать, "off".
	Autocorrect *string `json:"autocorrect,omitempty"`
	// Не исправлять слова короче.
	AutocorrectMinLen *int `json:"autocorrect_min_len,omitempty"`
	// Насколько далеко может быть исправление (1 — одна обычная ошибка; соседняя клавиша стоит 0,6).
	AutocorrectMaxDistShort *float64 `json:"autocorrect_max_dist_short,omitempty"` // для слов до 5 букв
	AutocorrectMaxDistLong  *float64 `json:"autocorrect_max_dist_long,omitempty"`  // от 6 букв
	// Исправление должно быть не реже этой частотности (шкала 1–7).
	AutocorrectMinZipf *float64 `json:"autocorrect_min_zipf,omitempty"`
	// И заметно лучше второго варианта.
	AutocorrectMargin *float64 `json:"autocorrect_margin,omitempty"`
	// Осторожно с окончаниями (не исправлять, если отличается только окончание).
	AutocorrectEndingCaution *bool `json:"autocorrect_ending_caution,omitempty"`
	// Исправлять слова с заглавной буквы не в начале предложения (обычно это имена).
	AutocorrectCapitalized *bool `json:"autocorrect_capitalized,omitempty"`
	// Исправлять частые ошибки написания, которые сами есть в словаре (recieve, извените).
	AutocorrectMisspellings *bool `json:"autocorrect_misspellings,omitempty"`

	// Подсказки: показывать варианты при наборе.
	Predict *bool `json:"predict,omitempty"`
	// После скольких букв показывать варианты.
	PredictMinPrefix *int `json:"predict_min_prefix,omitempty"`
	// Предлагать следующее слово после пробела (по вашей истории).
	PredictNextWord *bool `json:"predict_next_word,omitempty"`
	// Запоминать, какие слова вы набираете и в каком порядке (только известные слова).
	LearnTyping *bool `json:"learn_typing,omitempty"`
}

// AppRule — правило для приложения (процесса).
type AppRule struct {
	Name    string `json:"name"`            // подпись в настройках
	Enabled bool   `json:"enabled"`         // правило действует
	Process string `json:"process"`         // имя процесса, можно со * и через ; (например "cmd.exe;pwsh.exe")
	Class   string `json:"class,omitempty"` // класс окна (необязательно, тоже с *)
	Title   string `json:"title,omitempty"` // часть заголовка окна (необязательно)
	Rule
}

// Hotkeys — строки вида "DoubleShift", "DoubleLShift", "Pause", "Shift+Pause", "Ctrl+Alt+Z", "" (выкл).
type Hotkeys struct {
	ConvertWord      string `json:"convert_word"`
	ConvertPhrase    string `json:"convert_phrase"`
	ConvertSelection string `json:"convert_selection"`
	Undo             string `json:"undo"`
	ToggleEnabled    string `json:"toggle_enabled"`
	SwitchLayout     string `json:"switch_layout"`
	ToggleApp        string `json:"toggle_app"`
	// Принять подсказку: первый вариант и варианты по номеру (работают, только пока видна полоска).
	AcceptFirst string `json:"accept_first"`
	Accept1     string `json:"accept_1"`
	Accept2     string `json:"accept_2"`
	Accept3     string `json:"accept_3"`
	// Скрыть полоску подсказок (клавиша всё равно доходит до программы).
	Dismiss string `json:"dismiss"`
}

// Learn — автоматический сбор слов.
type Learn struct {
	// Сколько раз нужно отменить исправление слова, чтобы оно попало в исключения.
	ExceptionAfter int `json:"exception_after"`
	// Сколько раз нужно вручную сконвертировать слово, чтобы оно стало «своим» словом.
	WordAfter int `json:"word_after"`
	// Сколько раз нужно набрать незнакомое слово без исправления, чтобы запомнить его.
	PassiveAfter int `json:"passive_after"`
	// Минимальная длина слова для пассивного сбора.
	PassiveMinLen int `json:"passive_min_len"`
	// Не добавлять сразу, а оставлять кандидатов на проверку в настройках.
	Review bool `json:"review"`
	// Предел числа кандидатов (старые и редкие вытесняются).
	MaxCandidates int `json:"max_candidates"`
	// Собирать вообще.
	Enabled bool `json:"enabled"`
}

// Update — обновления с GitHub.
type Update struct {
	// Проверять, есть ли новая версия.
	Check bool `json:"check"`
	// Автообновление: скачивать и ставить самому (когда вы ничего не печатаете).
	Auto bool `json:"auto"`
	// Как часто проверять, часов.
	IntervalHours int `json:"interval_hours"`
	// Предлагать предварительные (бета) версии.
	Prerelease bool `json:"prerelease"`
	// Версия, которую вы решили пропустить.
	Skipped string `json:"skipped"`
	// Репозиторий на GitHub (владелец/название).
	Repo string `json:"repo"`
}

// Suggest — полоска подсказок.
type Suggest struct {
	// Где показывать: "caret" — у курсора ввода, "mouse" — у указателя мыши,
	// "bottom" — внизу экрана, "off" — не показывать (исправление опечаток работает и так).
	Position string `json:"position"`
	// Если курсор ввода не найден: "mouse" или "bottom".
	Fallback string `json:"fallback"`
	// Сколько вариантов показывать (1–5).
	Count int `json:"count"`
	// Добавлять пробел после принятого слова.
	AddSpace bool `json:"add_space"`
	// Подписи клавиш (Tab, Ctrl+2…) под вариантами.
	ShowHints bool `json:"show_hints"`
	// Принимать первый вариант стрелкой вправо (сразу после набора слова).
	AcceptRight bool `json:"accept_right"`
	// Принимать вариант щелчком мыши.
	AcceptClick bool `json:"accept_click"`
	// Tab принимает и следующее слово (когда слово ещё не начато). Иначе Tab там работает как обычно.
	TabNextWord bool `json:"tab_next_word"`
	// Следующее слово предлагать, если вы набирали эту пару хотя бы столько раз.
	NextMinCount int `json:"next_min_count"`
	// Размер шрифта полоски (1 — обычный).
	Scale float64 `json:"scale"`
	// Искать курсор ввода через специальные возможности Windows (нужно для Chrome, Edge, Office).
	UseAccessibility bool `json:"use_accessibility"`
	// Пока ваша история набора пуста — подсказывать частые сочетания слов из встроенной базы.
	BaseNextWords bool `json:"base_next_words"`
	// Предел пар слов в истории набора.
	MaxPairs int `json:"max_pairs"`
}

// Config — файл настроек целиком.
type Config struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled"`
	// Окно двойного нажатия модификатора, мс.
	DoubleTapMs int `json:"double_tap_ms"`
	// Обрабатывать ли нажатия, сымитированные другими программами (AutoHotkey и т.п.).
	ProcessInjected bool `json:"process_injected"`
	// Не трогать поля ввода пароля.
	SkipPasswords bool `json:"skip_passwords"`
	// Явно заданные раскладки (HKL/KLID, например "00000419"); пусто — найти автоматически.
	LayoutRU string `json:"layout_ru"`
	LayoutEN string `json:"layout_en"`
	LayoutUK string `json:"layout_uk"`
	// Языки: "auto" — по установленным раскладкам, или список: "en,ru", "en,uk", "en,ru,uk".
	Languages string `json:"languages"`
	// Где открывать настройки: "window" — отдельное окно программы, "browser" — в браузере.
	UIMode string `json:"ui_mode"`
	// Установку уже предлагали (больше не спрашивать).
	InstallPrompted bool `json:"install_prompted"`
	// Мастер первого запуска пройден.
	WizardDone bool `json:"wizard_done"`
	// Папка для резервной копии настроек (например, в OneDrive) — пусто, если не нужна.
	BackupDir string `json:"backup_dir"`
	// Порт окна настроек (0 — случайный).
	UIPort int `json:"ui_port"`
	// Показывать раскладку в трее.
	TrayShowsLayout bool `json:"tray_shows_layout"`
	// Журнал решений (только в памяти, для настройки).
	LogDecisions bool `json:"log_decisions"`
	LogSize      int  `json:"log_size"`
	// Автозапуск с Windows.
	Autostart bool `json:"autostart"`

	Suggest Suggest `json:"suggest"`
	Update  Update  `json:"update"`

	Defaults Rule      `json:"defaults"`
	Apps     []AppRule `json:"apps"`
	Hotkeys  Hotkeys   `json:"hotkeys"`
	Learn    Learn     `json:"learn"`
}

func ptr[T any](v T) *T { return &v }

// Default — настройки по умолчанию.
func Default() *Config {
	return &Config{
		Version:         1,
		Enabled:         true,
		DoubleTapMs:     400,
		SkipPasswords:   true,
		TrayShowsLayout: true,
		UIMode:          "window",
		Languages:       "auto",
		LogSize:         200,
		Defaults: Rule{
			Mode:                     ptr("auto"),
			MinLen:                   ptr(2),
			DictionaryOnly:           ptr(false),
			ShortWordMinZipf:         ptr(3.5),
			ShortWordCurMax:          ptr(-1.6),
			DictCurMax:               ptr(-1.2),
			NgramMinLen:              ptr(4),
			NgramCurMax:              ptr(-1.8),
			NgramAltMin:              ptr(-1.5),
			NgramMargin:              ptr(0.8),
			EarlySwitch:              ptr(true),
			EarlyMinLen:              ptr(4),
			CorrectOnSpace:           ptr(true),
			CorrectOnEnter:           ptr(true),
			CorrectOnTab:             ptr(false),
			SkipWithDigits:           ptr(true),
			SkipChars:                ptr("@#$\\_=|"),
			SkipFirstWord:            ptr(false),
			UndoByBackspace:          ptr(true),
			Inject:                   ptr("unicode"),
			BackspaceDelayMs:         ptr(0),
			KeyDelayMs:               ptr(0),
			SwitchDelayMs:            ptr(15),
			SwitchLayout:             ptr(true),
			LayoutSource:             ptr("auto"),
			SelectionAllowed:         ptr(true),
			CopyCombo:                ptr("Ctrl+C"),
			ReselectAfterConvert:     ptr(true),
			LearnPassive:             ptr(true),
			PreferWords:              ptr(""),
			SingleLetterContext:      ptr(true),
			Autocorrect:              ptr("auto"),
			AutocorrectMinLen:        ptr(4),
			AutocorrectMaxDistShort:  ptr(1.0),
			AutocorrectMaxDistLong:   ptr(1.6),
			AutocorrectMinZipf:       ptr(2.5),
			AutocorrectMargin:        ptr(1.0),
			AutocorrectEndingCaution: ptr(false),
			AutocorrectCapitalized:   ptr(false),
			AutocorrectMisspellings:  ptr(true),
			Predict:                  ptr(true),
			PredictMinPrefix:         ptr(2),
			PredictNextWord:          ptr(true),
			LearnTyping:              ptr(true),
		},
		Apps: []AppRule{
			{
				Name: "Терминалы", Enabled: true,
				Process: "windowsterminal.exe;cmd.exe;powershell.exe;pwsh.exe;conhost.exe;openconsole.exe;wsl.exe;bash.exe;mintty.exe;alacritty.exe;wezterm-gui.exe;putty.exe;kitty.exe;tabby.exe;hyper.exe;conemu64.exe;conemu.exe",
				Rule: Rule{
					DictionaryOnly:       ptr(true),
					MinLen:               ptr(2),
					EarlySwitch:          ptr(false),
					SkipChars:            ptr("@#$/\\_=|:~"),
					BackspaceDelayMs:     ptr(2),
					KeyDelayMs:           ptr(1),
					SwitchDelayMs:        ptr(30),
					LearnPassive:         ptr(true),
					CopyCombo:            ptr("Ctrl+Shift+C"),
					ReselectAfterConvert: ptr(false),
					Autocorrect:          ptr("off"),
					Predict:              ptr(false),
					PreferWords: ptr("cd ls ll la pwd dir cls git gh ssh scp vim vi nvim nano cat less more grep rg find fd sed awk " +
						"npm npx pnpm yarn node deno bun python py pip uv go cargo rustc make cmake gcc dotnet " +
						"docker kubectl helm terraform az aws gcloud curl wget echo exit clear code sudo apt brew winget choco scoop " +
						"mkdir rmdir rm cp mv touch chmod chown ln tar zip unzip ps kill top htop man history which where whoami " +
						"ping ipconfig ifconfig netstat tracert nslookup set export source start explorer tasklist taskkill systemctl " +
						"journalctl claude status commit push pull fetch merge rebase checkout switch branch log diff add clone init run build test install"),
				},
			},
			{
				Name: "Редакторы кода", Enabled: true,
				Process: "code.exe;cursor.exe;devenv.exe;idea64.exe;pycharm64.exe;webstorm64.exe;clion64.exe;rider64.exe;goland64.exe;sublime_text.exe;notepad++.exe",
				Rule: Rule{
					DictionaryOnly: ptr(true),
					SkipChars:      ptr("@#$/\\_=|:~(){}<>"),
					Autocorrect:    ptr("off"),
					Predict:        ptr(false),
				},
			},
			{
				Name: "Игры и удалённый доступ (выключено)", Enabled: true,
				Process: "mstsc.exe;vmconnect.exe;steam.exe;*game*.exe",
				Rule:    Rule{Mode: ptr("off")},
			},
		},
		Hotkeys: Hotkeys{
			ConvertWord:      "DoubleShift",
			ConvertPhrase:    "Shift+Pause",
			ConvertSelection: "Ctrl+Shift+Pause",
			Undo:             "Pause",
			ToggleEnabled:    "Ctrl+Alt+Pause",
			SwitchLayout:     "",
			ToggleApp:        "",
			AcceptFirst:      "Tab",
			Accept1:          "Ctrl+1",
			Accept2:          "Ctrl+2",
			Accept3:          "Ctrl+3",
			Dismiss:          "Esc",
		},
		Update: Update{
			Check:         true,
			Auto:          true,
			IntervalHours: 12,
			Repo:          "NickolasFleim/ErisedSwitcher",
		},
		Suggest: Suggest{
			Position:         "caret",
			Fallback:         "mouse",
			Count:            3,
			AddSpace:         true,
			ShowHints:        true,
			AcceptRight:      false,
			AcceptClick:      true,
			TabNextWord:      false,
			NextMinCount:     2,
			Scale:            1.0,
			UseAccessibility: true,
			BaseNextWords:    true,
			MaxPairs:         60000,
		},
		Learn: Learn{
			Enabled:        true,
			ExceptionAfter: 1,
			WordAfter:      1,
			PassiveAfter:   3,
			PassiveMinLen:  3,
			Review:         false,
			MaxCandidates:  5000,
		},
	}
}

// Effective — правило со всеми заполненными полями (глобальные + правило приложения).
type Effective struct {
	RuleName                 string
	Mode                     string
	MinLen                   int
	DictionaryOnly           bool
	ShortWordMinZipf         float64
	ShortWordCurMax          float64
	DictCurMax               float64
	NgramMinLen              int
	NgramCurMax              float64
	NgramAltMin              float64
	NgramMargin              float64
	EarlySwitch              bool
	EarlyMinLen              int
	CorrectOnSpace           bool
	CorrectOnEnter           bool
	CorrectOnTab             bool
	SkipWithDigits           bool
	SkipChars                string
	SkipFirstWord            bool
	UndoByBackspace          bool
	Inject                   string
	BackspaceDelayMs         int
	KeyDelayMs               int
	SwitchDelayMs            int
	SwitchLayout             bool
	LayoutSource             string
	SelectionAllowed         bool
	CopyCombo                string
	ReselectAfterConvert     bool
	LearnPassive             bool
	PreferWords              string
	SingleLetterContext      bool
	Autocorrect              string
	AutocorrectMinLen        int
	AutocorrectMaxDistShort  float64
	AutocorrectMaxDistLong   float64
	AutocorrectMinZipf       float64
	AutocorrectMargin        float64
	AutocorrectEndingCaution bool
	AutocorrectCapitalized   bool
	AutocorrectMisspellings  bool
	Predict                  bool
	PredictMinPrefix         int
	PredictNextWord          bool
	LearnTyping              bool
	Prefer                   map[string]bool `json:"-"`
}

func pick[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func apply(e *Effective, r *Rule) {
	pick(&e.Mode, r.Mode)
	pick(&e.MinLen, r.MinLen)
	pick(&e.DictionaryOnly, r.DictionaryOnly)
	pick(&e.ShortWordMinZipf, r.ShortWordMinZipf)
	pick(&e.ShortWordCurMax, r.ShortWordCurMax)
	pick(&e.DictCurMax, r.DictCurMax)
	pick(&e.NgramMinLen, r.NgramMinLen)
	pick(&e.NgramCurMax, r.NgramCurMax)
	pick(&e.NgramAltMin, r.NgramAltMin)
	pick(&e.NgramMargin, r.NgramMargin)
	pick(&e.EarlySwitch, r.EarlySwitch)
	pick(&e.EarlyMinLen, r.EarlyMinLen)
	pick(&e.CorrectOnSpace, r.CorrectOnSpace)
	pick(&e.CorrectOnEnter, r.CorrectOnEnter)
	pick(&e.CorrectOnTab, r.CorrectOnTab)
	pick(&e.SkipWithDigits, r.SkipWithDigits)
	pick(&e.SkipChars, r.SkipChars)
	pick(&e.SkipFirstWord, r.SkipFirstWord)
	pick(&e.UndoByBackspace, r.UndoByBackspace)
	pick(&e.Inject, r.Inject)
	pick(&e.BackspaceDelayMs, r.BackspaceDelayMs)
	pick(&e.KeyDelayMs, r.KeyDelayMs)
	pick(&e.SwitchDelayMs, r.SwitchDelayMs)
	pick(&e.SwitchLayout, r.SwitchLayout)
	pick(&e.LayoutSource, r.LayoutSource)
	pick(&e.SelectionAllowed, r.SelectionAllowed)
	pick(&e.CopyCombo, r.CopyCombo)
	pick(&e.ReselectAfterConvert, r.ReselectAfterConvert)
	pick(&e.LearnPassive, r.LearnPassive)
	pick(&e.PreferWords, r.PreferWords)
	pick(&e.SingleLetterContext, r.SingleLetterContext)
	pick(&e.Autocorrect, r.Autocorrect)
	pick(&e.AutocorrectMinLen, r.AutocorrectMinLen)
	pick(&e.AutocorrectMaxDistShort, r.AutocorrectMaxDistShort)
	pick(&e.AutocorrectMaxDistLong, r.AutocorrectMaxDistLong)
	pick(&e.AutocorrectMinZipf, r.AutocorrectMinZipf)
	pick(&e.AutocorrectMargin, r.AutocorrectMargin)
	pick(&e.AutocorrectEndingCaution, r.AutocorrectEndingCaution)
	pick(&e.AutocorrectCapitalized, r.AutocorrectCapitalized)
	pick(&e.AutocorrectMisspellings, r.AutocorrectMisspellings)
	pick(&e.Predict, r.Predict)
	pick(&e.PredictMinPrefix, r.PredictMinPrefix)
	pick(&e.PredictNextWord, r.PredictNextWord)
	pick(&e.LearnTyping, r.LearnTyping)
}

// Resolve собирает действующие параметры для окна. Правила применяются по порядку,
// подходящие более поздние правила перекрывают ранние.
func (c *Config) Resolve(process, class, title string) Effective {
	base := Default().Defaults
	e := Effective{RuleName: "Общие"}
	apply(&e, &base)
	apply(&e, &c.Defaults)
	process = strings.ToLower(process)
	for i := range c.Apps {
		a := &c.Apps[i]
		if !a.Enabled || !a.Matches(process, class, title) {
			continue
		}
		apply(&e, &a.Rule)
		e.RuleName = a.Name
	}
	e.Prefer = map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(e.PreferWords)) {
		e.Prefer[w] = true
	}
	return e
}

// Matches проверяет, подходит ли правило окну.
func (a *AppRule) Matches(process, class, title string) bool {
	if strings.TrimSpace(a.Process) == "" && a.Class == "" && a.Title == "" {
		return false
	}
	if strings.TrimSpace(a.Process) != "" && !matchList(a.Process, strings.ToLower(process)) {
		return false
	}
	if a.Class != "" && !matchList(a.Class, class) {
		return false
	}
	if a.Title != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(a.Title)) {
		return false
	}
	return true
}

func matchList(list, s string) bool {
	for _, p := range strings.Split(list, ";") {
		p = strings.TrimSpace(p)
		if p != "" && Glob(strings.ToLower(p), strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// Glob — сопоставление с шаблоном, где * — любая последовательность, ? — один символ.
func Glob(p, s string) bool {
	pr, sr := []rune(p), []rune(s)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		for i < len(pr) {
			switch pr[i] {
			case '*':
				for k := j; k <= len(sr); k++ {
					if match(i+1, k) {
						return true
					}
				}
				return false
			case '?':
				if j >= len(sr) {
					return false
				}
			default:
				if j >= len(sr) || sr[j] != pr[i] {
					return false
				}
			}
			i++
			j++
		}
		return j == len(sr)
	}
	return match(0, 0)
}

// Validate проверяет значения и возвращает список проблем (пусто — всё хорошо).
func (c *Config) Validate() []string {
	var errs []string
	e := c.Resolve("", "", "")
	check := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, msg)
		}
	}
	modes := map[string]bool{"auto": true, "manual": true, "off": true}
	injects := map[string]bool{"unicode": true, "keys": true}
	sources := map[string]bool{"auto": true, "focus": true, "console": true, "tracked": true}
	checkRule := func(name string, r *Rule) {
		if r.Mode != nil {
			check(modes[*r.Mode], name+": режим должен быть auto, manual или off")
		}
		if r.Inject != nil {
			check(injects[*r.Inject], name+": способ ввода должен быть unicode или keys")
		}
		if r.Autocorrect != nil {
			check(*r.Autocorrect == "auto" || *r.Autocorrect == "suggest" || *r.Autocorrect == "off", name+": исправление опечаток должно быть auto, suggest или off")
		}
		for _, v := range []*float64{r.AutocorrectMaxDistShort, r.AutocorrectMaxDistLong} {
			if v != nil {
				check(*v >= 0 && *v <= 2, name+": расстояние исправления — от 0 до 2")
			}
		}
		if r.LayoutSource != nil {
			check(sources[*r.LayoutSource], name+": источник раскладки должен быть auto, focus, console или tracked")
		}
		for _, v := range []*int{r.MinLen, r.NgramMinLen, r.EarlyMinLen, r.BackspaceDelayMs, r.KeyDelayMs, r.SwitchDelayMs} {
			if v != nil {
				check(*v >= 0 && *v <= 1000, name+": числа длины и пауз должны быть от 0 до 1000")
			}
		}
		if r.CopyCombo != nil && *r.CopyCombo != "" {
			_, err := ParseHotkey(*r.CopyCombo)
			check(err == nil, fmt.Sprintf("%s: комбинация копирования: %v", name, err))
		}
	}
	checkRule("Общие", &c.Defaults)
	for i := range c.Apps {
		checkRule("Правило «"+c.Apps[i].Name+"»", &c.Apps[i].Rule)
	}
	_ = e
	for name, h := range map[string]string{
		"Конвертировать слово": c.Hotkeys.ConvertWord, "Конвертировать фразу": c.Hotkeys.ConvertPhrase,
		"Конвертировать выделение": c.Hotkeys.ConvertSelection, "Отменить": c.Hotkeys.Undo,
		"Вкл/выкл": c.Hotkeys.ToggleEnabled, "Переключить раскладку": c.Hotkeys.SwitchLayout,
		"Вкл/выкл для приложения": c.Hotkeys.ToggleApp,
		"Принять подсказку":       c.Hotkeys.AcceptFirst, "Вариант 1": c.Hotkeys.Accept1,
		"Вариант 2": c.Hotkeys.Accept2, "Вариант 3": c.Hotkeys.Accept3, "Скрыть подсказки": c.Hotkeys.Dismiss,
	} {
		if _, err := ParseHotkey(h); err != nil {
			errs = append(errs, fmt.Sprintf("Горячая клавиша «%s»: %v", name, err))
		}
	}
	errs = append(errs, c.hotkeyConflicts()...)
	if c.Languages != "auto" && strings.TrimSpace(c.Languages) != "" {
		for _, part := range strings.Split(c.Languages, ",") {
			p := strings.TrimSpace(strings.ToLower(part))
			check(p == "en" || p == "ru" || p == "uk" || p == "ua", "Языки: auto или список из en, ru, uk через запятую")
		}
	}
	check(c.UIMode == "window" || c.UIMode == "browser", "Где открывать настройки: window или browser")
	check(c.Update.IntervalHours >= 1 && c.Update.IntervalHours <= 720, "Проверять обновления: раз в 1–720 часов")
	if parts := strings.Split(c.Update.Repo, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		errs = append(errs, "Репозиторий обновлений должен быть вида владелец/название")
	}
	sg := &c.Suggest
	check(map[string]bool{"caret": true, "mouse": true, "bottom": true, "off": true}[sg.Position], "Положение подсказок: caret, mouse, bottom или off")
	check(sg.Fallback == "mouse" || sg.Fallback == "bottom", "Если курсор не найден: mouse или bottom")
	check(sg.Count >= 1 && sg.Count <= 5, "Число вариантов: от 1 до 5")
	check(sg.Scale >= 0.6 && sg.Scale <= 3, "Размер шрифта подсказок: от 0,6 до 3")
	check(c.DoubleTapMs >= 100 && c.DoubleTapMs <= 2000, "Окно двойного нажатия: от 100 до 2000 мс")
	check(c.Learn.ExceptionAfter >= 1 && c.Learn.WordAfter >= 1 && c.Learn.PassiveAfter >= 1, "Счётчики сбора слов должны быть не меньше 1")
	return errs
}

// Dir — папка данных (%APPDATA%\Erised Switcher). Можно переопределить переменной ERISED_HOME
// или положив рядом с exe файл portable.txt.
func Dir() string {
	if d := os.Getenv("ERISED_HOME"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(d, "portable.txt")); err == nil {
			return filepath.Join(d, "data")
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Erised Switcher")
	}
	return "."
}

// Load читает config.json; если файла нет — создаёт с настройками по умолчанию.
// Отсутствующие в файле поля берутся из значений по умолчанию.
func Load(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, Save(path, c)
	}
	if err != nil {
		return c, err
	}
	b = stripBOM(b)
	c.WizardDone = true // у тех, кто уже пользовался программой, мастер не показываем
	if err := json.Unmarshal(b, c); err != nil {
		return Default(), fmt.Errorf("config.json: %w", err)
	}
	upgrade(c)
	if errs := c.Validate(); len(errs) > 0 {
		return c, fmt.Errorf("config.json: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// Save записывает файл атомарно (через временный файл).
func Save(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, b)
}

// WriteAtomic пишет данные во временный файл и переименовывает его.
func WriteAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Clone — глубокая копия через JSON.
func (c *Config) Clone() *Config {
	b, _ := json.Marshal(c)
	n := &Config{}
	_ = json.Unmarshal(b, n)
	return n
}

// hotkeyConflicts — одно сочетание на два действия. Исключение: «Конвертировать слово»
// и «Конвертировать выделение» можно повесить на одну клавишу — тогда она конвертирует
// выделение, если оно есть, а иначе последнее слово.
func (c *Config) hotkeyConflicts() []string {
	items := []struct{ name, val string }{
		{"Конвертировать слово", c.Hotkeys.ConvertWord},
		{"Конвертировать выделение", c.Hotkeys.ConvertSelection},
		{"Конвертировать фразу", c.Hotkeys.ConvertPhrase},
		{"Отменить", c.Hotkeys.Undo},
		{"Вкл/выкл", c.Hotkeys.ToggleEnabled},
		{"Переключить раскладку", c.Hotkeys.SwitchLayout},
		{"Вкл/выкл для приложения", c.Hotkeys.ToggleApp},
		{"Вариант 1", c.Hotkeys.Accept1},
		{"Вариант 2", c.Hotkeys.Accept2},
		{"Вариант 3", c.Hotkeys.Accept3},
	}
	seen := map[string]string{}
	var errs []string
	for _, it := range items {
		h, err := ParseHotkey(it.val)
		if err != nil || h.Empty {
			continue
		}
		k := h.String()
		if prev, ok := seen[k]; ok {
			pair := prev + "|" + it.name
			if pair == "Конвертировать слово|Конвертировать выделение" {
				continue
			}
			errs = append(errs, fmt.Sprintf("Сочетание %s назначено и на «%s», и на «%s»", it.val, prev, it.name))
			continue
		}
		seen[k] = it.name
	}
	return errs
}

// upgrade дополняет настройки из прошлых версий тем, что появилось позже.
func upgrade(c *Config) {
	for i := range c.Apps {
		a := &c.Apps[i]
		// 1.1: в терминалах не выделять результат конвертации (Shift+стрелки там ведут себя иначе).
		proc := strings.ToLower(a.Process)
		if a.ReselectAfterConvert == nil && strings.Contains(proc, "windowsterminal.exe") {
			a.ReselectAfterConvert = ptr(false)
		}
		// 1.2: в терминалах и редакторах кода подсказки и исправление опечаток выключены.
		if strings.Contains(proc, "windowsterminal.exe") || strings.Contains(proc, "code.exe") {
			if a.Autocorrect == nil {
				a.Autocorrect = ptr("off")
			}
			if a.Predict == nil {
				a.Predict = ptr(false)
			}
		}
	}
	if c.UIMode == "" {
		c.UIMode = "window"
	}
}
