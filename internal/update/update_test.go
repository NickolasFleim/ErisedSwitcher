package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.3.0", "1.2.0", 1}, {"1.2.0", "1.2.0", 0}, {"1.10.0", "1.9.9", 1}, {"1.2", "1.2.0", 0},
		{"1.3.0", "1.3.0-beta.1", 1}, {"1.3.0-beta.1", "1.3.0", -1}, {"2.0.0", "1.99.99", 1}, {"v1.2.1", "1.2.0", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, ожидалось %d", c.a, c.b, got, c.want)
		}
	}
}

// Фейковый GitHub: два релиза, предварительный и черновик.
func fakeGitHub(t *testing.T, payload []byte, digest bool, sums string) *httptest.Server {
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("нет User-Agent")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			d := ""
			if digest {
				d = "sha256:" + hexsum
			}
			if sums == "" {
				sums = hexsum + "  ErisedSwitcher.exe\n"
			}
			fmt.Fprintf(w, `[
			 {"tag_name":"v1.4.0-beta.1","prerelease":true,"assets":[{"name":"ErisedSwitcher.exe","browser_download_url":"%[1]s/a/beta.exe","size":1}]},
			 {"tag_name":"v9.9.9","draft":true,"assets":[{"name":"ErisedSwitcher.exe","browser_download_url":"%[1]s/a/draft.exe","size":1}]},
			 {"tag_name":"v1.3.0","name":"Erised Switcher 1.3.0","body":"Новое: обновления","html_url":"%[1]s/r/1.3.0",
			  "assets":[{"name":"ErisedSwitcher.exe","browser_download_url":"%[1]s/a/app.exe","size":%[2]d,"digest":"%[3]s"},
			            {"name":"checksums.txt","browser_download_url":"%[1]s/a/checksums.txt","size":10}]},
			 {"tag_name":"v1.2.0","assets":[{"name":"ErisedSwitcher.exe","browser_download_url":"%[1]s/a/old.exe","size":1}]}
			]`, srv.URL, len(payload), d)
		case r.URL.Path == "/a/app.exe":
			w.Write(payload)
		case r.URL.Path == "/a/checksums.txt":
			fmt.Fprint(w, sums)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

func TestLatestAndDownload(t *testing.T) {
	payload := append([]byte("MZ"), []byte(strings.Repeat("x", 100000))...)
	for _, digest := range []bool{true, false} {
		srv := fakeGitHub(t, payload, digest, "")
		c := &Client{API: srv.URL, Repo: "me/app"}
		r, err := c.Latest(context.Background(), "amd64", false)
		if err != nil {
			t.Fatal(err)
		}
		if r.Version != "1.3.0" || r.Notes != "Новое: обновления" {
			t.Fatalf("найдена не та версия: %+v", r)
		}
		if pre, _ := c.Latest(context.Background(), "amd64", true); pre.Version != "1.4.0-beta.1" {
			t.Fatalf("с предварительными: %s", pre.Version)
		}
		if !Newer(r.Version, "1.2.0") || Newer(r.Version, "1.3.0") {
			t.Fatal("сравнение с текущей версией")
		}
		dir := t.TempDir()
		var last int64
		p, err := c.Download(context.Background(), r, dir, func(d, _ int64) { last = d })
		if err != nil {
			t.Fatalf("загрузка (digest=%v): %v", digest, err)
		}
		b, _ := os.ReadFile(p)
		if string(b) != string(payload) || last != int64(len(payload)) {
			t.Fatal("скачано не то")
		}
		srv.Close()
	}
}

func TestDownloadRejectsBadChecksum(t *testing.T) {
	payload := []byte("MZ-настоящий файл")
	srv := fakeGitHub(t, payload, false, strings.Repeat("0", 64)+"  ErisedSwitcher.exe\n")
	defer srv.Close()
	c := &Client{API: srv.URL, Repo: "me/app"}
	r, err := c.Latest(context.Background(), "amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := c.Download(context.Background(), r, dir, nil); err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatalf("подменённый файл должен быть отвергнут: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("после отказа не должно оставаться файлов")
	}
}

func TestNoAssetForArch(t *testing.T) {
	srv := fakeGitHub(t, []byte("MZ"), true, "")
	defer srv.Close()
	c := &Client{API: srv.URL, Repo: "me/app"}
	if _, err := c.Latest(context.Background(), "arm64", false); err == nil {
		t.Fatal("для arm64 файлов нет — должна быть ошибка")
	}
}
