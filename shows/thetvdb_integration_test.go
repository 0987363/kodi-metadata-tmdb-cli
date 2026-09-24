package shows

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/thetvdb"
	"fengqi/kodi-metadata-tmdb-cli/utils"
)

type imageTransport func(*http.Request) (*http.Response, error)

func (f imageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTheTVDBHTMLToFilesAndAmbiguousEpisode(t *testing.T) {
	oldC, oldS, oldL := config.Collector, config.Scraper, config.Log
	defer func() { config.Collector, config.Scraper, config.Log = oldC, oldS, oldL }()
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(filepath.Join(dir, ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".metadata", "source.json"), []byte(`{"provider":"thetvdb","id":"371065"}`), 0644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/dereferrer/series/371065" {
			http.Redirect(w, r, "/series/26882341-show", 302)
			return
		}
		file := ""
		switch r.URL.Path {
		case "/series/26882341-show":
			file = "series.html"
		case "/series/26882341-show/allseasons/official":
			file = "allseasons.html"
		case "/series/26882341-show/episodes/7407799":
			file = "episode.html"
		default:
			t.Errorf("非预期的请求：%s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", file))
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	images := &artwork.Downloader{Clients: map[string]*http.Client{"thetvdb": {Transport: imageTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "artworks.thetvdb.com" {
			t.Errorf("图片来源错误：%s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture-image")), Request: r}, nil
	})}}}
	config.Log = &config.LogConfig{Mode: 1}
	utils.InitLogger()
	config.Collector = &config.CollectorConfig{RunMode: 2, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	extractor := setAnalysisResponse(t, `{"title":"Show","season":1,"episode":2}`)
	manager := metadata.NewManager([]metadata.Provider{thetvdb.New(server.Client(), server.URL, "zho", 0)}, time.Hour, judgeForTest{})
	name := "Show.S01E02.mkv"
	mf := media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows)
	if err := ProcessMetadata(context.Background(), mf, extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	firstCalls := calls
	if err := ProcessMetadata(context.Background(), mf, extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	if calls != firstCalls {
		t.Fatalf("重复执行未命中缓存：%d -> %d", firstCalls, calls)
	}
	var episode struct {
		Title   string `xml:"title"`
		Runtime int    `xml:"runtime"`
		IDs     []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"uniqueid"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "Show.S01E02.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(data, &episode); err != nil {
		t.Fatal(err)
	}
	if episode.Runtime != 60 || len(episode.IDs) != 1 || episode.IDs[0].Type != "tvdb" || episode.IDs[0].Value != "7407799" {
		t.Fatalf("HTML至NFO映射错误：%s", data)
	}
	for _, file := range []string{"tvshow.nfo", "poster.jpg", "fanart.jpg"} {
		if info, err := os.Stat(filepath.Join(dir, file)); err != nil || info.Size() == 0 {
			t.Fatalf("缺少输出%s：%v", file, err)
		}
	}
	extractor = setAnalysisResponse(t, `{"title":"Show","season":1,"episode":1}`)
	bad := media_file.NewMediaFile(filepath.Join(dir, "Show.S01E01.mkv"), "Show.S01E01.mkv", media_file.TvShows)
	if err := ProcessMetadata(context.Background(), bad, extractor, manager, images); !errors.Is(err, metadata.ErrAmbiguous) {
		t.Fatalf("重复季集记录未拒绝：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Show.S01E01.nfo")); !os.IsNotExist(err) {
		t.Fatalf("歧义单集不应写NFO：%v", err)
	}
}
