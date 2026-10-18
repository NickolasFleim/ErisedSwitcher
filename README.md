# Erised Switcher

*Erised stra ehru oyt ube cafru oyt on wohsi.*

**[Русская версия →](README.ru.md)**

Automatic keyboard layout switcher for Windows 10/11 — English, Russian and Ukrainian. Like the Mirror of Erised, it shows not what you typed but what you meant to write: `ghbdtn` becomes «привет», `руддщ` becomes `hello`.

The program's interface is in Russian; the names of tabs and options below are translated.

It works like Caramba Switcher or Punto Switcher, with a few important differences:

- **Works in terminals** — Windows Terminal, cmd, PowerShell, WSL, mintty — and in any other program that doesn't expose its text. Erised Switcher remembers the keys you pressed, not the text in the window, so it doesn't care what the window can do.
- **Everything is tunable**: confidence thresholds, per-application rules, hotkeys, input delays, how the current layout is detected.
- **Phone-style suggestions and autocorrect**: a suggestion strip at the text cursor, typo correction on Space (Backspace undoes it), next-word prediction learned from your own typing.
- **Learns by itself**: undo a correction and the word becomes an exception; convert a word by hand and it will be corrected automatically next time; an unknown word you keep typing stops being treated as a mistake. Every word is stored once with a counter, and everything collected can be reviewed and edited.

![Settings window](docs/images/settings-general.png)

## Download and install

Get the latest version from [Releases](https://github.com/NickolasFleim/ErisedSwitcher/releases/latest): `ErisedSwitcher.exe` for regular PCs, `ErisedSwitcher-arm64.exe` for Windows on ARM.

On first launch the program offers to install itself — for the current user only, no administrator rights: into `%LOCALAPPDATA%\Programs\Erised Switcher`, with a Start menu shortcut and an entry in *Settings → Apps* (uninstall it from there like any other app). Answer *No* to keep running it from where it is.

A first-run wizard then asks three questions: which languages to switch between, which key converts a word by hand, and how typos are handled. Everything else can be changed later.

![First-run wizard](docs/images/wizard.png)

**winget** (once the package has been accepted into the winget catalog):

```
winget install NickolasFleim.ErisedSwitcher
```

A winget installation is updated by winget, so the built-in updater stays off.

**Portable mode**: put an empty `portable.txt` next to the exe — settings will live in a `data` folder beside it, and the program won't offer to install itself.

**Windows SmartScreen / antivirus.** The exe isn't code-signed yet, so SmartScreen may show "Windows protected your PC" (click *More info → Run anyway*), and some antivirus engines flag any program with a keyboard hook. The source is open and every release is built by GitHub Actions from a tagged commit; the SHA-256 digest of each file is shown on the release page. See [docs/SIGNING.md](docs/SIGNING.md) for how signing will be added.

## Basics

1. A tray icon shows the current layout (blue EN, red RU, amber UA, grey — disabled).
2. Click it to open the settings window; right-click for the menu.
3. To work in windows running as administrator (e.g. *Terminal (Admin)*), the program itself must run as administrator — use *Restart as administrator* in the menu. This is a Windows restriction for all such programs.

Settings and dictionaries live in `%APPDATA%\Erised Switcher`.

## Languages

English, Russian and Ukrainian. By default the languages are taken from the layouts installed in Windows; you can also choose a pair (EN+RU, EN+UK) or all three on the *General* tab.

With both Cyrillic layouts installed, the program picks the one that fits the word: `ghbdsn` becomes «привіт», `ghbdtn` becomes «привет». From Cyrillic the conversion goes to English; from English it goes to the Cyrillic layout you used last. Pressing the hotkey again cycles through the remaining layouts.

## Suggestions and typos

While you type, a strip with options appears at the text cursor: word completion (`здравс` → «здравствуйте»), a typo fix (`pritn` → `print`), and after a space — the word you usually type next («доброе» → «утро»).

| Key | Action |
|---|---|
| Tab | Accept the first option (only while the strip is visible; otherwise Tab works as usual) |
| Ctrl+1 / Ctrl+2 / Ctrl+3 | Accept an option by number |
| Click an option | Accept it (focus stays in your program) |
| Right arrow | Accept the first option — enable it in settings |
| Esc | Hide the strip |
| Backspace right after a correction | Undo it (and stop correcting that word) |

**Typo correction** knows the keyboard: hitting a neighbouring key, swapping two letters or missing a double letter are "cheap" mistakes. A fix is applied on Space or Enter only when the program is confident: on synthetic typos about 75% are fixed automatically with about 2% wrong fixes; in other cases the right word is simply first in the strip (correct first option 88–89% of the time). *Suggest only* mode never changes your text by itself.

After every automatic correction a short flash near the cursor shows what changed — `пртвет → привет` — so you notice it and can undo it with Backspace. It can be turned off on the *General* tab.

Not touched: existing words (checked against ~630k Russian, ~540k English and ~124k Ukrainian word forms, so rare but correct words aren't "fixed"), capitalized words mid-sentence (usually names), abbreviations, words with digits or hyphens, your own words and exceptions.

**Next word.** Until your own history has enough data, common word pairs from a built-in base are suggested («может быть», «будь ласка», "if you"); your own pairs always win. **Typing history** (`typing.json`) stores only known words and "word → next word" pairs with counters, on this computer only; password fields are never recorded. View, forget a pair or wipe it all on the *Suggestions* tab.

In terminals and code editors suggestions and typo correction are off by default — those have their own completion. Everything can be turned on and tuned per application.

![Suggestions tab](docs/images/settings-predict.png)

## Default hotkeys

| Keys | Action |
|---|---|
| Shift twice | If text is selected — convert the selection. Otherwise — the last word. Twice again — revert. With neither a word nor a selection — switch the layout |
| Shift+Pause | Convert the phrase (everything since the start of the line) |
| Ctrl+Shift+Pause | Convert the selected text |
| Pause | Undo the correction |
| Backspace right after a correction | Undo it and remember the word as an exception |
| Ctrl+Alt+Pause | Enable or disable |
| Tab, Ctrl+1/2/3, Esc | Suggestions (see above; only while the strip is visible) |

All of these can be changed on the *Hotkeys* tab: combinations (`Ctrl+Alt+Z`), double taps (`DoubleShift`, `DoubleRCtrl`) and single taps of left/right modifiers (`RCtrl`). *Convert word* and *Convert selection* may share a key; other conflicts are reported and can't be saved.

Selection is guessed from what you do: dragging or double-clicking with the mouse, Shift+click, Shift+arrows/Home/End, Ctrl+A. Any typing, Backspace or an arrow without Shift counts as removing the selection.

**Experimental: read text at the cursor.** Normally the program only knows what you typed since the last click. With *Read text at the cursor after a mouse click* enabled (*General* tab), it reads the text before the cursor through Windows accessibility (UI Automation) — click after an already written word or phrase and double Shift converts it. Works in Notepad, Word, browsers and many other programs, but not all. Text read this way is never corrected automatically.

## Terminals

There is a ready-made *Terminals* rule (*Applications* tab):

- only dictionary words and words from the "always correct" list are fixed — common commands (`git`, `cd`, `ls`, `docker`, `kubectl`…) are already there, so `пше ыефегы` becomes `git status`, while your paths and flags are left alone;
- words containing `/ \ $ = @ _ : ~` are never touched;
- a correction on Enter happens before the command runs;
- for classic console windows the layout is read through the console API — exactly where other switchers get it wrong;
- the correction is typed as Unicode characters, so it doesn't depend on the terminal's layout;
- the selection is copied with Ctrl+Shift+C and isn't re-selected.

If a command is corrected by mistake, undo with Backspace — it becomes an exception. If a word isn't corrected, convert it with double Shift and it will be corrected automatically from then on.

## Tuning

- **Check** tab — type words and see what will happen to them and why, with the scores. You can name a program to test its rule.
- **Log** tab — enable it on *General* and every decision is recorded with an explanation (in memory only, until exit).
- **Too many corrections** — enable *Dictionary only*, raise *Short words: minimum frequency* or lower *Current variant no better than*.
- **Missed corrections** — disable *Dictionary only* for that program, lower *Minimum word length*.
- **Leftover letters or garbled text in some program** — increase the input delays in its rule or switch *How to type the correction* to *Keys*.

![Check tab](docs/images/settings-probe.png)

`config.json` and `words.json` can also be edited by hand — the program re-reads them within a couple of seconds.

## Moving to another computer and backups

*General → Transfer and backup*:

- **Save to file…** — settings, application rules, your own and collected words in one `erised-settings.json` (optionally with the typing history). **Load from file…** on the other computer.
- **Backup folder** — e.g. a folder in OneDrive or on a USB stick. Whenever settings or the dictionary change, the program writes `erised-settings.json` there by itself.

## Updates

The program checks [GitHub releases](https://github.com/NickolasFleim/ErisedSwitcher/releases) 45 seconds after start and then every 12 hours.

- **Auto-update** (checkbox on *General*, on by default): the new version is downloaded and installed when you haven't typed for a minute. The program closes for a second, starts again and reports "Erised Switcher updated to …".
- Without the checkbox you get a notification; update with the button in settings or the tray menu item. There are also *Check now*, release notes and *Skip this version*.
- The downloaded file is verified against the SHA-256 digest GitHub publishes for every release asset; on mismatch the update is cancelled.
- **Beta channel**: tick *Offer beta versions* to receive pre-releases (tags like `v1.5.0-beta.1`).
- Settings, dictionaries and history are never touched by updates.

## Something went wrong?

If an error happens, the program keeps running, saves a report to `%APPDATA%\Erised Switcher\crash` and shows a notification; a hard crash is picked up on the next start. *Log → Report a bug on GitHub* opens a pre-filled issue form with the program and Windows versions, layouts, the last crash and the service log. Typed text and your user name are never included — check the text before sending anyway.

## How it works

- A low-level keyboard hook (`WH_KEYBOARD_LL`) keeps the current word as a sequence of key presses with Shift/CapsLock, so the word can be "read" in any layout without asking the window.
- Layout decisions: frequency dictionaries (~150k Russian, ~90k English, ~88k Ukrainian words) plus a letter trigram model for unknown words. About 99% of wrong-layout words among the 10,000 most frequent are caught; among 20,000 rare correctly typed words false switches are about 0.08%. Russian vs Ukrainian is decided by the dictionaries and letters unique to each (і ї є ґ / ы э ъ ё).
- Typos: a compact similar-words index (SymSpell, ~25 MB in memory, built in the background within a second) and a weighted Damerau–Levenshtein distance that knows neighbouring keys; a Bloom filter with all word forms ever seen. Suggestions take about 0.15 ms per key press.
- Correction: Backspaces, input via `SendInput` (Unicode or keys), layout switch via `WM_INPUTLANGCHANGEREQUEST`. Keys pressed during a correction are held and replayed afterwards, so fast typing isn't lost.
- The selection is converted through the clipboard; its previous text content is restored afterwards.
- The settings window is a separate process of the same program (`ErisedSwitcher.exe --settings`, WebView2), so a WebView2 failure can't stop layout correction. Settings are served on `127.0.0.1` only and require a secret token.
- Password fields are never processed. Typed text is never sent anywhere.

## Building from source

Go 1.23+. All dependencies are vendored, no network needed. On Windows: `build.bat`. Anywhere:

```
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ErisedSwitcher.exe ./cmd/erised
go test ./...
```

`ErisedSwitcher.exe --selftest report.txt` checks on a real Windows machine what unit tests can't: the keyboard hook, the suggestion window, the settings server, the clipboard and installation (in temporary folders). CI runs it on `windows-latest` for every push.

Layout: `internal/engine` — the logic (tested without Windows), `internal/predict` — suggestions and typos, `internal/lang` — dictionaries and the letter model, `internal/keymap` — layouts, `internal/config` — settings, `internal/words` — collected words, `internal/webui` — the settings page, `internal/settingswin` — its window, `internal/update` and `internal/app/updater.go` — updates, `internal/win` and `internal/app` — everything Windows-specific. Icon, version info and manifest: `cmd/erised/winres` (built with [go-winres](https://github.com/tc-hib/go-winres)). Dictionaries are rebuilt by `tools/gendict.py` from [FrequencyWords](https://github.com/hermitdave/FrequencyWords), the starter next-word base by `tools/genpairs.py` from [Universal Dependencies](https://universaldependencies.org/).

## Releasing

1. Set `Version` in `internal/app/app.go` and add a `## X.Y.Z` section to `CHANGELOG.md`.
2. Commit and push a tag: `git tag vX.Y.Z && git push origin main vX.Y.Z`.
3. GitHub Actions (`.github/workflows/release.yml`) tests, builds both exe files with version info, runs the self-test, signs them if signing secrets are set, and publishes the release with notes from `CHANGELOG.md` and winget manifests. A tag with a hyphen (`v1.5.0-beta.1`) is published as a pre-release for the beta channel.

## Known limitations

- Windows running as administrator are handled only if the program runs as administrator too.
- In programs that don't report the text cursor position, the strip appears at the mouse pointer (or at the bottom of the screen — your choice).
- If the clipboard held an image or files, after converting a selection it will contain the previous text instead.
- In a terminal, the selection is text on the screen, not in the input line; for the input line, *Convert phrase* works better.

## License

Erised Switcher is free software: you can redistribute it and/or modify it under the terms of the **GNU General Public License v3.0 or later** — see [LICENSE](LICENSE). Third-party data and libraries are listed in [NOTICE.md](NOTICE.md).
