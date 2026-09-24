package artwork

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadRejectsHTTPAndTruncatedBody(t *testing.T) {
	for _, code := range []int{403, 429, 500, 200} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if code == 200 {
					w.Header().Set("Content-Length", "99")
				}
				w.WriteHeader(code)
				_, _ = w.Write([]byte("bad"))
			}))
			defer s.Close()
			dest := filepath.Join(t.TempDir(), "poster.jpg")
			if err := Download(context.Background(), s.Client(), s.URL, dest); err == nil {
				t.Fatal("失败下载不能返回成功")
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("留下了半文件：%v", err)
			}
		})
	}
}
func TestDownloadRefreshesWhenSourceChanges(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count++; _, _ = w.Write([]byte(r.URL.Path)) }))
	defer s.Close()
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	for _, p := range []string{"/a", "/a", "/b"} {
		if err := Download(context.Background(), s.Client(), s.URL+p, dest); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(dest)
	if err != nil || string(b) != "/b" || count != 2 {
		t.Fatalf("图片来源切换或缓存错误：%s %v %d", b, err, count)
	}
}

func TestDownloaderChoosesOneImagePerDestination(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/preferred" {
			t.Errorf("访问了未选中的图片：%s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("preferred"))
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	d := &Downloader{Clients: map[string]*http.Client{"tmdb": server.Client()}}
	items := []Item{{URL: server.URL + "/preferred", Path: dest}, {URL: server.URL + "/unused", Path: dest}}
	for range 2 {
		if err := d.Save(context.Background(), "tmdb", items); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("重复下载了候选图片：%d", calls)
	}
}
