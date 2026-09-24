package movies

import (
	"context"
	"encoding/xml"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/tmdb"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessMetadataUsesExplicitMovieID(t *testing.T) {
	oldC, oldS, oldL := config.Collector, config.Scraper, config.Log
	defer func() { config.Collector, config.Scraper, config.Log = oldC, oldS, oldL }()
	dir := t.TempDir()
	config.Collector = &config.CollectorConfig{RunMode: 2}
	config.Scraper = &config.ScraperConfig{NfoField: config.NfoField{Genre: true}}
	extractor := setAnalysisResponse(t, `{"title":"Clerk","year":2020}`)
	config.Log = &config.LogConfig{Mode: 1}
	utils.InitLogger()
	name := "Clerk.mkv"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmdb", name+".id.txt"), []byte("42"), 0644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/3/movie/42" {
			t.Errorf("不应搜索：%s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"title":"Clerk","imdb_id":"tt42","genres":[{"id":1,"name":"Comedy"}],"credits":{"cast":[{"name":"Actor","character":"Role"}]},"release_date":"2020-01-01"}`))
	}))
	defer server.Close()
	p := tmdb.NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL, ImageHost: server.URL, ApiKey: "test", TimeoutSeconds: 1})
	m := metadata.NewManager([]metadata.Provider{p}, time.Hour, judgeForTest{})
	for range 2 {
		if err := ProcessMetadata(context.Background(), media_file.NewMediaFile(path, name, media_file.Movies), extractor, m, &artwork.Downloader{Clients: map[string]*http.Client{"tmdb": server.Client()}}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "Clerk.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Genres []string `xml:"genre"`
		Tags   []string `xml:"tag"`
		Actors []struct {
			Name string `xml:"name"`
		} `xml:"actor"`
	}
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got.Genres) != 1 || len(got.Tags) != 0 || len(got.Actors) != 1 {
		t.Fatalf("缓存/开关/无头像演员错误：%d %s", calls, data)
	}
}

func TestConflictingMovieOverridesFailBeforeRequest(t *testing.T) {
	oldCollector, oldScraper := config.Collector, config.Scraper
	defer func() { config.Collector, config.Scraper = oldCollector, oldScraper }()
	config.Collector = &config.CollectorConfig{}
	config.Scraper = &config.ScraperConfig{}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	for file, id := range map[string]string{"id.txt": "1", "Movie.mkv.id.txt": "2"} {
		if err := os.WriteFile(filepath.Join(dir, "tmdb", file), []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mf := &media_file.MediaFile{Path: filepath.Join(dir, "Movie.mkv"), Dir: dir, Filename: "Movie.mkv", Suffix: ".mkv"}
	err := ProcessMetadata(context.Background(), mf, nil, metadata.NewManager(nil, 0, judgeForTest{}), nil)
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("应明确拒绝冲突人工编号：%v", err)
	}
}

func TestProcessDiscWritesBothNfoLocations(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(discName, func(t *testing.T) {
			oldCollector, oldScraper := config.Collector, config.Scraper
			t.Cleanup(func() { config.Collector, config.Scraper = oldCollector, oldScraper })
			config.Collector = &config.CollectorConfig{RunMode: config.CollectorRunModeOnce}
			config.Scraper = &config.ScraperConfig{}
			dir := t.TempDir()
			disc := filepath.Join(dir, discName)
			if err := os.Mkdir(disc, 0755); err != nil {
				t.Fatal(err)
			}
			extractor := setAnalysisResponse(t, `{"title":"Disc Movie","year":2020,"tmdb_id":"42"}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/3/movie/42" {
					t.Errorf("不应请求其他电影：%s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				_, _ = w.Write([]byte(`{"id":42,"title":"Disc Movie","release_date":"2020-01-01"}`))
			}))
			t.Cleanup(server.Close)
			provider := tmdb.NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL, ImageHost: server.URL, ApiKey: "test", TimeoutSeconds: 1})
			manager := metadata.NewManager([]metadata.Provider{provider}, time.Hour, judgeForTest{})
			mf := media_file.NewMediaFile(disc, discName, media_file.Movies)
			if err := ProcessMetadata(context.Background(), mf, extractor, manager, &artwork.Downloader{Clients: map[string]*http.Client{"tmdb": server.Client()}}); err != nil {
				t.Fatal(err)
			}
			rootNfo, err := os.ReadFile(filepath.Join(dir, "movie.nfo"))
			if err != nil {
				t.Fatal(err)
			}
			indexName := "index.nfo"
			if discName == "VIDEO_TS" {
				indexName = "VIDEO_TS.nfo"
			}
			indexNfo, err := os.ReadFile(filepath.Join(disc, indexName))
			if err != nil {
				t.Fatal(err)
			}
			if string(rootNfo) != string(indexNfo) {
				t.Fatal("原盘两个发现位置必须保存相同的 NFO 内容")
			}
			var got struct {
				XMLName xml.Name
				Title   string `xml:"title"`
			}
			if err := xml.Unmarshal(rootNfo, &got); err != nil || got.XMLName.Local != "movie" || got.Title != "Disc Movie" {
				t.Fatalf("原盘 NFO 内容错误：%s，错误：%v", rootNfo, err)
			}
		})
	}
}
