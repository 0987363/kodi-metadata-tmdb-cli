package shows

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/nfo"
	"fengqi/kodi-metadata-tmdb-cli/tmdb"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessAIKeepsCachedShowIdentityForNextEpisode(t *testing.T) {
	oldScraper, oldTMDB, oldCollector, oldLog := config.Scraper, config.Tmdb, config.Collector, config.Log
	oldLogger := utils.Logger
	t.Cleanup(func() {
		config.Scraper, config.Tmdb, config.Collector, config.Log = oldScraper, oldTMDB, oldCollector, oldLog
		utils.Logger = oldLogger
	})
	root := t.TempDir()
	dir := filepath.Join(root, "Alone")
	cache := filepath.Join(dir, "tmdb")
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "id.txt"), []byte("63726"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "tv.json"), []byte(`{"id":63726,"name":"荒野独居","first_air_date":"2015-06-18","last_air_date":"2025-01-01"}`), 0644); err != nil {
		t.Fatal(err)
	}
	name := "Alone.S01E02.Of.Wolf.and.Man.1080p.AMZN.WEB-DL.DD+2.0.x264-Cinefeel.mkv"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	episodeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"Alone","eng_title":"Alone","season":1,"episode":2,"confidence":0.99}`}}}})
		case "/3/tv/63726":
			_, _ = w.Write([]byte(`{"id":63726,"name":"荒野独居"}`))
		case "/3/tv/63726/season/1/episode/2":
			episodeCalls++
			_, _ = w.Write([]byte(`{"id":1002,"name":"Of Wolf and Man","air_date":"2015-06-25","season_number":1,"episode_number":2}`))
		default:
			t.Errorf("单集请求使用了错误的作品身份：%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config.Log = &config.LogConfig{Mode: config.LogModeStdout}
	utils.InitLogger()
	config.Collector = &config.CollectorConfig{RunMode: config.CollectorRunModeOnce, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	config.Tmdb = &config.TmdbConfig{ApiHost: server.URL, ImageHost: server.URL}
	extractor, err := ai.New(config.LLMConfig{Name: "extractor", Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	mf := media_file.NewMediaFile(path, name, media_file.TvShows)
	manager := metadata.NewManager([]metadata.Provider{tmdb.NewMetadataProvider(config.Tmdb)}, time.Hour, judgeForTest{})
	if err := ProcessMetadata(context.Background(), mf, extractor, manager, &artwork.Downloader{Clients: map[string]*http.Client{"tmdb": server.Client()}}); err != nil {
		t.Fatal(err)
	}
	if episodeCalls != 1 {
		t.Fatalf("未使用已缓存的电视剧身份获取下一集：%d", episodeCalls)
	}
	data, err := os.ReadFile(path[:len(path)-len(filepath.Ext(path))] + ".nfo")
	if err != nil {
		t.Fatal(err)
	}
	var output nfo.Document
	if err := xml.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.ShowTitle != "荒野独居" || output.Season == nil || *output.Season != 1 || output.Episode == nil || *output.Episode != 2 || len(output.UniqueIDs) != 1 || output.UniqueIDs[0].Value != "1002" {
		t.Fatalf("单集 NFO 作品信息错误：%+v", output)
	}
}
