//go:build windows

package app

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"erised/internal/win"
)

const issuesURL = "https://github.com/NickolasFleim/ErisedSwitcher/issues/new"

// crashes — отчёты о сбоях в папке crash.
type crashes struct {
	dir       string
	mu        sync.Mutex
	last      time.Time
	notified  bool
	fatalFile *os.File
	onNotify  func(title, text string)
}

func (a *App) crashDir() string { return filepath.Join(a.dir, "crash") }

// setupCrashes включает запись отчётов. Вызывается в самом начале Run.
func (a *App) setupCrashes() {
	c := &crashes{dir: a.crashDir()}
	a.crash = c
	_ = os.MkdirAll(c.dir, 0o755)
	// Сбои среды Go (их нельзя перехватить) пишутся в fatal.txt; если файл не пуст —
	// прошлый запуск закончился аварийно.
	fatal := filepath.Join(c.dir, "fatal.txt")
	if b, err := os.ReadFile(fatal); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		name := filepath.Join(c.dir, "crash-"+time.Now().Format("2006-01-02-150405")+"-fatal.txt")
		_ = os.WriteFile(name, []byte(a.reportHeader()+"Аварийное завершение (прошлый запуск):\n\n"+redact(string(b))), 0o644)
		a.crashedLastTime = true
	}
	if f, err := os.Create(fatal); err == nil {
		c.fatalFile = f
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}
	win.PanicHandler = a.onPanic
	c.prune(20)
}

// onPanic — перехваченный сбой: программа продолжает работать, отчёт сохраняется.
func (a *App) onPanic(where string, v any, stack []byte) {
	c := a.crash
	if c == nil {
		return
	}
	log.Printf("сбой (%s): %v", where, v)
	c.mu.Lock()
	tooSoon := time.Since(c.last) < time.Minute
	c.last = time.Now()
	first := !c.notified
	c.notified = true
	c.mu.Unlock()
	if tooSoon {
		return // при повторяющемся сбое не засыпаем диск отчётами
	}
	name := filepath.Join(c.dir, "crash-"+time.Now().Format("2006-01-02-150405")+".txt")
	text := a.reportHeader() + fmt.Sprintf("Где: %s\nОшибка: %v\n\n%s", where, v, redact(string(stack)))
	_ = os.WriteFile(name, []byte(text), 0o644)
	if first && a.tray.Hwnd != 0 {
		a.tray.Balloon("Произошла ошибка, программа продолжает работать",
			"Отчёт сохранён. Отправить его можно в настройках, вкладка «Журнал».", true)
	}
}

func (c *crashes) prune(keep int) {
	files := c.list()
	for i := keep; i < len(files); i++ {
		_ = os.Remove(filepath.Join(c.dir, files[i].Name))
	}
}

// CrashReport — сохранённый отчёт.
type CrashReport struct {
	Name string    `json:"name"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

func (c *crashes) list() []CrashReport {
	entries, _ := os.ReadDir(c.dir)
	var out []CrashReport
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "crash-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, CrashReport{Name: e.Name(), Time: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// reportHeader — сведения о системе (без набранного текста и личных путей).
func (a *App) reportHeader() string {
	admin := "нет"
	if win.IsAdmin() {
		admin = "да"
	}
	return fmt.Sprintf("Erised Switcher %s\n%s, %s\nОт администратора: %s\nРаскладки: %s\nВремя: %s\n\n",
		Version, win.WindowsVersion(), runtime.GOARCH, admin, a.layoutsText(), time.Now().Format(time.RFC3339))
}

// redact убирает из текста имя пользователя (в путях) — в отчёт оно не нужно.
func redact(s string) string {
	for _, env := range []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		if v := os.Getenv(env); len(v) > 3 {
			s = strings.ReplaceAll(s, v, "%"+env+"%")
			s = strings.ReplaceAll(s, strings.ReplaceAll(v, `\`, `/`), "%"+env+"%")
		}
	}
	if u := os.Getenv("USERNAME"); len(u) > 2 {
		s = strings.ReplaceAll(s, u, "<user>")
	}
	return s
}

// tailLog — последние строки erised.log (там только служебные сообщения, без набранного текста).
func (a *App) tailLog(n int) string {
	b, err := os.ReadFile(filepath.Join(a.dir, "erised.log"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return redact(strings.Join(lines, "\n"))
}

// BugReport — отчёт для GitHub: сведения о системе, последний сбой, конец журнала.
func (a *App) BugReport() string {
	var b strings.Builder
	b.WriteString(a.reportHeader())
	if list := a.crash.list(); len(list) > 0 {
		if t, err := os.ReadFile(filepath.Join(a.crash.dir, list[0].Name)); err == nil {
			b.WriteString("--- Последний сбой (" + list[0].Time.Format("2006-01-02 15:04") + ") ---\n")
			text := string(t)
			if i := strings.Index(text, "\n\n"); i > 0 {
				text = text[i+2:] // шапку уже вывели
			}
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	b.WriteString("--- erised.log ---\n")
	b.WriteString(a.tailLog(60))
	return b.String()
}

// CrashReports — список отчётов с текстом (для окна настроек).
func (a *App) CrashReports() []CrashReport {
	list := a.crash.list()
	for i := range list {
		if i >= 10 {
			list = list[:10]
			break
		}
		if t, err := os.ReadFile(filepath.Join(a.crash.dir, list[i].Name)); err == nil {
			list[i].Text = string(t)
		}
	}
	return list
}

// ReportIssue открывает на GitHub форму ошибки с уже заполненными версией и отчётом.
func (a *App) ReportIssue() {
	rep := a.BugReport()
	if len(rep) > 5500 { // длина адреса ограничена
		rep = rep[:5500] + "\n…(обрезано; полный отчёт — кнопкой «Скопировать отчёт»)"
	}
	q := url.Values{}
	q.Set("template", "bug.yml")
	q.Set("version", Version)
	q.Set("windows", win.WindowsVersion()+", "+runtime.GOARCH)
	q.Set("log", rep)
	win.ShellOpen(issuesURL + "?" + q.Encode())
}

// ClearCrashes удаляет отчёты.
func (a *App) ClearCrashes() {
	for _, r := range a.crash.list() {
		_ = os.Remove(filepath.Join(a.crash.dir, r.Name))
	}
}

// Diagnostics — для вкладки «Журнал».
func (a *App) Diagnostics() map[string]any {
	return map[string]any{"report": a.BugReport(), "crashes": a.CrashReports()}
}

// DiagnosticsAction — кнопки вкладки «Журнал».
func (a *App) DiagnosticsAction(op string) error {
	switch op {
	case "issue":
		a.ReportIssue()
	case "clear":
		a.ClearCrashes()
	default:
		return fmt.Errorf("неизвестное действие %q", op)
	}
	return nil
}
