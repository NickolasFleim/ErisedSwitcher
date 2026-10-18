// Package update — проверка и загрузка новых версий с GitHub Releases.
// Здесь только то, что не зависит от Windows: запрос к GitHub, сравнение версий,
// выбор файла под архитектуру, загрузка и проверка контрольной суммы.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo — откуда брать обновления.
const DefaultRepo = "NickolasFleim/ErisedSwitcher"

// Release — найденная версия.
type Release struct {
	Version   string    `json:"version"` // без «v»
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	URL       string    `json:"url"` // страница релиза
	Published time.Time `json:"published"`
	Pre       bool      `json:"prerelease"`
	Asset     Asset     `json:"asset"`
	SumsURL   string    `json:"-"` // checksums.txt, если есть
}

// Asset — файл программы в релизе.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"` // из поля digest, если GitHub его дал
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []ghAsset `json:"assets"`
}

// Client — обращение к GitHub.
type Client struct {
	Repo      string
	API       string // по умолчанию https://api.github.com (меняется в тестах)
	UserAgent string
	HTTP      *http.Client
}

func (c *Client) api() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return "https://api.github.com"
}

func (c *Client) get(ctx context.Context, url string, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "ErisedSwitcher"
	}
	req.Header.Set("User-Agent", ua)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 5 * time.Minute}
	}
	return h.Do(req)
}

// AssetName — имя файла программы для архитектуры (GOARCH).
func AssetName(arch string) string {
	if arch == "arm64" {
		return "ErisedSwitcher-arm64.exe"
	}
	return "ErisedSwitcher.exe"
}

// Latest находит самую новую версию (с предварительными, если pre=true).
func (c *Client) Latest(ctx context.Context, arch string, pre bool) (*Release, error) {
	repo := c.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	resp, err := c.get(ctx, c.api()+"/repos/"+repo+"/releases?per_page=20", "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("нет связи с GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("репозиторий с обновлениями не найден")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("GitHub временно ограничил запросы, попробуем позже")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub ответил %s", resp.Status)
	}
	var list []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("не удалось разобрать ответ GitHub: %w", err)
	}
	var best *Release
	for _, r := range list {
		if r.Draft || (r.Prerelease && !pre) {
			continue
		}
		v := strings.TrimPrefix(strings.TrimPrefix(r.Tag, "v"), "V")
		if _, ok := parse(v); !ok {
			continue
		}
		rel := &Release{Version: v, Tag: r.Tag, Name: r.Name, Notes: r.Body, URL: r.HTMLURL, Published: r.Published, Pre: r.Prerelease}
		want := AssetName(arch)
		for _, a := range r.Assets {
			switch {
			case strings.EqualFold(a.Name, want):
				rel.Asset = Asset{Name: a.Name, URL: a.URL, Size: a.Size}
				if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
					rel.Asset.SHA256 = strings.ToLower(d)
				}
			case strings.EqualFold(a.Name, "checksums.txt"):
				rel.SumsURL = a.URL
			}
		}
		if rel.Asset.URL == "" {
			continue // в этом релизе нет файла для нашей архитектуры
		}
		if best == nil || Compare(rel.Version, best.Version) > 0 {
			best = rel
		}
	}
	if best == nil {
		return nil, errors.New("на GitHub пока нет опубликованных версий")
	}
	return best, nil
}

// Compare сравнивает версии вида 1.2.3 (и 1.2.3-beta.1): -1, 0, 1.
func Compare(a, b string) int {
	va, _ := parse(a)
	vb, _ := parse(b)
	for i := 0; i < 3; i++ {
		if va.n[i] != vb.n[i] {
			if va.n[i] < vb.n[i] {
				return -1
			}
			return 1
		}
	}
	// Версия без суффикса новее предварительной: 1.3.0 > 1.3.0-beta.
	switch {
	case va.pre == vb.pre:
		return 0
	case va.pre == "":
		return 1
	case vb.pre == "":
		return -1
	case va.pre < vb.pre:
		return -1
	}
	return 1
}

type ver struct {
	n   [3]int
	pre string
}

func parse(s string) (ver, bool) {
	var v ver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	v.pre = pre
	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.n[i] = n
	}
	return v, true
}

// Newer — версия rel новее текущей cur.
func Newer(rel, cur string) bool { return Compare(rel, cur) > 0 }

// expectedSum — контрольная сумма файла: из поля digest или из checksums.txt.
func (c *Client) expectedSum(ctx context.Context, r *Release) (string, error) {
	if r.Asset.SHA256 != "" {
		return r.Asset.SHA256, nil
	}
	if r.SumsURL == "" {
		return "", errors.New("в релизе нет контрольной суммы — устанавливать небезопасно")
	}
	resp, err := c.get(ctx, r.SumsURL, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt: %s", resp.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.EqualFold(strings.TrimPrefix(f[1], "*"), r.Asset.Name) {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", errors.New("в checksums.txt нет суммы для " + r.Asset.Name)
}

// Download скачивает файл программы в dir и проверяет контрольную сумму SHA-256.
// Возвращает путь к проверенному файлу. progress может быть nil.
func (c *Client) Download(ctx context.Context, r *Release, dir string, progress func(done, total int64)) (string, error) {
	want, err := c.expectedSum(ctx, r)
	if err != nil {
		return "", err
	}
	if len(want) != 64 {
		return "", errors.New("неверная контрольная сумма в релизе")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, "ErisedSwitcher-"+r.Version+".exe")
	// Уже скачан и проверен раньше?
	if sum, err := fileSHA256(final); err == nil && sum == want {
		return final, nil
	}
	resp, err := c.get(ctx, r.Asset.URL, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("загрузка: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("загрузка: %s", resp.Status)
	}
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	total := r.Asset.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	var done int64
	buf := make([]byte, 64<<10)
	var first []byte
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if len(first) < 2 {
				first = append(first, buf[:min(n, 2-len(first))]...)
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return "", werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
			if done > 200<<20 {
				f.Close()
				os.Remove(part)
				return "", errors.New("файл обновления подозрительно большой")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return "", fmt.Errorf("загрузка прервалась: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		os.Remove(part)
		return "", errors.New("контрольная сумма не совпала — файл повреждён или подменён, обновление отменено")
	}
	if string(first) != "MZ" {
		os.Remove(part)
		return "", errors.New("скачанный файл — не программа Windows")
	}
	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	return final, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cleanup удаляет старые скачанные файлы, кроме keep.
func Cleanup(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if p != keep && strings.HasPrefix(e.Name(), "ErisedSwitcher-") {
			_ = os.Remove(p)
		}
	}
}
