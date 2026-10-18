//go:build windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"erised/internal/settingswin"
	"erised/internal/update"
	"erised/internal/win"
)

// updater — проверка и установка новых версий.
type updater struct {
	a  *App
	mu sync.Mutex
	// Состояние для окна настроек и меню.
	status    string // idle | checking | downloading | ready | installing | error
	lastCheck time.Time
	latest    *update.Release
	err       string
	done      int64
	total     int64
	path      string // скачанный и проверенный файл
	notified  string // о какой версии уже сообщили
	wake      chan struct{}
}

func (a *App) updateDir() string { return filepath.Join(a.dir, "update") }

func (a *App) startUpdater() {
	u := &updater{a: a, status: "idle", wake: make(chan struct{}, 1)}
	a.upd = u
	a.afterUpdateNotice()
	go u.loop()
}

func (u *updater) client() *update.Client {
	cfg := u.a.cfg.Load()
	return &update.Client{Repo: cfg.Update.Repo, UserAgent: "ErisedSwitcher/" + Version}
}

func (u *updater) loop() {
	// Первая проверка — не сразу после запуска Windows.
	select {
	case <-time.After(45 * time.Second):
	case <-u.wake:
	}
	for {
		cfg := u.a.cfg.Load().Update
		u.mu.Lock()
		due := time.Since(u.lastCheck) >= time.Duration(cfg.IntervalHours)*time.Hour
		u.mu.Unlock()
		win.Protect("обновления", func() {
			if cfg.Check && due {
				u.check(false)
			}
			if cfg.Auto {
				u.autoInstall()
			}
		})
		select {
		case <-time.After(time.Minute):
		case <-u.wake:
		}
	}
}

// check ищет новую версию. manual — по кнопке (сообщает и «новых версий нет»).
func (u *updater) check(manual bool) {
	cfg := u.a.cfg.Load().Update
	u.set(func() { u.status, u.err = "checking", "" })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := u.client().Latest(ctx, runtime.GOARCH, cfg.Prerelease)
	u.mu.Lock()
	u.lastCheck = time.Now()
	if err != nil {
		u.status, u.err = "error", err.Error()
		u.mu.Unlock()
		log.Printf("проверка обновлений: %v", err)
		if manual {
			u.a.tray.Balloon("Не удалось проверить обновления", err.Error(), true)
		}
		return
	}
	if !update.Newer(rel.Version, Version) {
		u.latest, u.status = nil, "idle"
		u.mu.Unlock()
		if manual {
			u.a.tray.Balloon("Обновлений нет", "У вас последняя версия "+Version+".", false)
		}
		return
	}
	if u.latest == nil || u.latest.Version != rel.Version {
		u.path = ""
	}
	u.latest = rel
	if u.path != "" {
		u.status = "ready"
	} else {
		u.status = "idle"
	}
	notify := u.notified != rel.Version && (manual || (!cfg.Auto && cfg.Skipped != rel.Version))
	if notify {
		u.notified = rel.Version
	}
	u.mu.Unlock()
	log.Printf("доступна версия %s", rel.Version)
	if notify {
		u.a.tray.Balloon("Доступна версия "+rel.Version, "Обновить можно в меню значка или в настройках (вкладка «Общие»).", false)
	}
}

func (u *updater) set(f func()) { u.mu.Lock(); f(); u.mu.Unlock() }

// download скачивает найденную версию (если ещё не скачана).
func (u *updater) download() (string, error) {
	u.mu.Lock()
	rel, path := u.latest, u.path
	if rel == nil {
		u.mu.Unlock()
		return "", errors.New("новая версия не найдена")
	}
	if path != "" {
		u.mu.Unlock()
		return path, nil
	}
	u.status, u.err, u.done, u.total = "downloading", "", 0, rel.Asset.Size
	u.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	p, err := u.client().Download(ctx, rel, u.a.updateDir(), func(d, t int64) { u.set(func() { u.done, u.total = d, t }) })
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.status, u.err = "error", err.Error()
		log.Printf("загрузка обновления: %v", err)
		return "", err
	}
	update.Cleanup(u.a.updateDir(), p)
	u.path, u.status = p, "ready"
	return p, nil
}

// autoInstall — автообновление: скачать и поставить, когда вы ничего не печатаете.
func (u *updater) autoInstall() {
	cfg := u.a.cfg.Load().Update
	u.mu.Lock()
	rel := u.latest
	u.mu.Unlock()
	if rel == nil || rel.Version == cfg.Skipped {
		return
	}
	if wingetManaged() {
		// Установлено через winget — обновляет winget («winget upgrade»), мы только сообщаем.
		u.mu.Lock()
		notify := u.notified != rel.Version
		u.notified = rel.Version
		u.mu.Unlock()
		if notify {
			u.a.tray.Balloon("Доступна версия "+rel.Version, "Программа установлена через winget: обновите командой winget upgrade NickolasFleim.ErisedSwitcher", false)
		}
		return
	}
	if _, err := u.download(); err != nil {
		return
	}
	// Ставим, когда минуту не было ни нажатий, ни движений мыши и ничего не печатается.
	u.a.busyMu.Lock()
	busy := u.a.busy
	u.a.busyMu.Unlock()
	if busy || win.IdleFor() < time.Minute {
		return
	}
	if err := u.install(); err != nil {
		u.a.tray.Balloon("Не удалось установить обновление", err.Error(), true)
	}
}

// install запускает помощник установки и завершает программу.
func (u *updater) install() error {
	path, err := u.download()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	u.set(func() { u.status = "installing" })
	log.Printf("устанавливаю обновление %s", path)
	// Окно настроек принадлежит старой версии — закрываем.
	if h := win.FindWindowByTitle(settingswin.ClassName, settingswin.Title); h != 0 {
		win.PostMessage(h, win.WM_CLOSE, 0, 0)
	}
	_ = u.a.SaveWords()
	if u.a.hist != nil {
		_ = u.a.hist.SaveIfDirty()
	}
	cmd := exec.Command(path, "--apply-update", exe, strconv.Itoa(os.Getpid()), u.a.dir)
	if err := cmd.Start(); err != nil {
		u.set(func() { u.status, u.err = "error", err.Error() })
		return fmt.Errorf("не удалось запустить установку: %w", err)
	}
	win.AllowForeground(cmd.Process.Pid)
	// Выходим: помощник дождётся, заменит файл и запустит новую версию.
	win.PostMessage(u.a.hwnd, win.WM_CLOSE, 0, 0)
	return nil
}

// Status — для окна настроек.
func (u *updater) Status() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	cfg := u.a.cfg.Load().Update
	m := map[string]any{
		"current":    Version,
		"status":     u.status,
		"error":      u.err,
		"last_check": u.lastCheck,
		"done":       u.done,
		"total":      u.total,
		"skipped":    cfg.Skipped,
	}
	if u.latest != nil {
		m["latest"] = u.latest
	}
	return m
}

// Action — кнопки в настройках и меню.
func (u *updater) Action(op string) error {
	switch op {
	case "check":
		u.check(true)
	case "install":
		go func() {
			if err := u.install(); err != nil {
				u.a.tray.Balloon("Не удалось установить обновление", err.Error(), true)
			}
		}()
	case "open":
		u.mu.Lock()
		rel := u.latest
		u.mu.Unlock()
		if rel != nil && strings.HasPrefix(rel.URL, "https://github.com/") {
			win.ShellOpen(rel.URL)
		}
	case "skip":
		u.mu.Lock()
		rel := u.latest
		u.mu.Unlock()
		if rel == nil {
			return errors.New("пропускать нечего")
		}
		c := u.a.cfg.Load().Clone()
		c.Update.Skipped = rel.Version
		return u.a.applyConfig(c, true)
	default:
		return fmt.Errorf("неизвестное действие %q", op)
	}
	return nil
}

// Available — версия, до которой можно обновиться (пусто, если нет).
func (u *updater) Available() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest == nil {
		return ""
	}
	return u.latest.Version
}

// afterUpdateNotice — после обновления: сообщаем, что получилось, и убираем следы.
func (a *App) afterUpdateNotice() {
	verFile := filepath.Join(a.dir, "version.txt")
	errFile := filepath.Join(a.updateDir(), "error.txt")
	if b, err := os.ReadFile(errFile); err == nil {
		a.tray.Balloon("Обновление не установлено", strings.TrimSpace(string(b)), true)
		_ = os.Remove(errFile)
	}
	prev, _ := os.ReadFile(verFile)
	if p := strings.TrimSpace(string(prev)); p != "" && update.Newer(Version, p) {
		a.tray.Balloon("Erised Switcher обновлён до "+Version, "Что нового — в настройках, вкладка «Общие».", false)
		update.Cleanup(a.updateDir(), "")
	}
	if string(prev) != Version {
		_ = os.WriteFile(verFile, []byte(Version), 0o644)
	}
	// Старый файл программы после замены.
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// ApplyUpdate — режим помощника: ErisedSwitcher.exe --apply-update <куда> <pid> <папка данных>.
// Ждёт выхода старой версии, заменяет файл и запускает новую.
func ApplyUpdate(args []string) int {
	if len(args) < 3 {
		return 2
	}
	target, dataDir := args[0], args[2]
	pid, _ := strconv.Atoi(args[1])
	if f, err := os.OpenFile(filepath.Join(dataDir, "erised.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	self, err := os.Executable()
	if err != nil {
		return 1
	}
	if pid > 0 && !win.WaitProcessExit(pid, 20*time.Second) {
		log.Printf("обновление: старая версия не завершилась")
	}
	var lastErr error
	for i := 0; i < 40; i++ {
		lastErr = replaceFile(self, target)
		if lastErr == nil {
			break
		}
		if isAccessDenied(lastErr) && !win.IsAdmin() && i >= 3 {
			// В папку программы нельзя писать без прав администратора — просим их (один раз).
			log.Printf("обновление: нужны права администратора (%v)", lastErr)
			quoted := make([]string, len(args))
			for j, a := range args {
				quoted[j] = syscall.EscapeArg(a)
			}
			if win.RunAsAdmin(self, "--apply-update "+strings.Join(quoted, " ")) {
				return 0
			}
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if lastErr != nil {
		log.Printf("обновление не установлено: %v", lastErr)
		_ = os.MkdirAll(filepath.Join(dataDir, "update"), 0o755)
		_ = os.WriteFile(filepath.Join(dataDir, "update", "error.txt"),
			[]byte("Не удалось заменить "+target+": "+lastErr.Error()+". Работает прежняя версия."), 0o644)
	} else {
		log.Printf("обновление установлено в %s", target)
	}
	// Запускаем (новую или прежнюю) версию. Через explorer — чтобы программа не унаследовала
	// права администратора, если помощнику пришлось их запросить.
	if win.IsAdmin() {
		if exec.Command("explorer.exe", target).Start() == nil {
			return 0
		}
	}
	if err := exec.Command(target).Start(); err != nil {
		log.Printf("обновление: не удалось запустить %s: %v", target, err)
		return 1
	}
	return 0
}

// replaceFile кладёт src на место dst: копия рядом, старый файл в .old, новый на его место.
func replaceFile(src, dst string) error {
	tmp := dst + ".new"
	if err := copyFileMode(src, tmp); err != nil {
		return err
	}
	old := dst + ".old"
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(old, dst) // вернуть как было
		return err
	}
	_ = os.Remove(old) // может не удалиться, если файл ещё занят — удалим при следующем запуске
	return nil
}

func copyFileMode(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func isAccessDenied(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 5 // ERROR_ACCESS_DENIED
	}
	return os.IsPermission(err)
}
