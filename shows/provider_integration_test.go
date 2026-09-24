package shows

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type directProvider struct {
	source      string
	allowSearch bool
	calls       []metadata.Request
	searchCalls int
}

func (p *directProvider) Name() string {
	if p.source != "" {
		return p.source
	}
	return "thetvdb"
}
func (p *directProvider) Scope() string { return "test" }
func (p *directProvider) Supports(k metadata.Kind) bool {
	return k == metadata.Show || k == metadata.Episode
}
func (p *directProvider) Search(context.Context, metadata.Query) ([]metadata.Candidate, error) {
	p.searchCalls++
	if p.allowSearch {
		return []metadata.Candidate{{Ref: metadata.Ref{Provider: p.Name(), Kind: metadata.Show, ID: "371065"}, Title: "节目"}}, nil
	}
	return nil, errors.New("已有编号不应搜索")
}
func (p *directProvider) Fetch(ctx context.Context, r metadata.Request) (*metadata.Record, error) {
	p.calls = append(p.calls, r)
	if r.Ref.ID != "371065" {
		return nil, errors.New("父节目编号丢失")
	}
	if r.Kind == metadata.Show {
		var externalIDs []metadata.Identifier
		if p.Name() == "thetvdb" {
			externalIDs = []metadata.Identifier{{Type: "tmdb", Value: "74747"}}
		}
		return &metadata.Record{Ref: metadata.Ref{Provider: p.Name(), Kind: metadata.Show, ID: "371065"}, Title: "坑王驾到", ExternalIDs: externalIDs}, nil
	}
	return &metadata.Record{Ref: metadata.Ref{Provider: p.Name(), Kind: metadata.Episode, ID: "7407799"}, Title: "第二回", SeasonNumber: r.Season, EpisodeNumber: r.Episode}, nil
}
func TestProcessAIProviderIDSkipsSearch(t *testing.T) {
	oldS, oldC, oldL := config.Scraper, config.Collector, config.Log
	defer func() { config.Scraper, config.Collector, config.Log = oldS, oldC, oldL }()
	root := t.TempDir()
	dir := filepath.Join(root, "节目.mkv.archive", "Season 01")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	name := "节目.S01E02.mkv"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"坑王驾到","thetvdb_id":"371065","season":1,"episode":2,"confidence":0.99}`}}}})
	}))
	defer server.Close()
	config.Log = &config.LogConfig{Mode: 1}
	utils.InitLogger()
	config.Collector = &config.CollectorConfig{RunMode: 2, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	extractor, err := ai.New(config.LLMConfig{Name: "extractor", Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := new(directProvider)
	m := metadata.NewManager([]metadata.Provider{p}, time.Hour, judgeForTest{})
	images := &artwork.Downloader{Clients: map[string]*http.Client{"thetvdb": server.Client()}}
	for range 2 {
		if err := ProcessMetadata(context.Background(), media_file.NewMediaFile(path, name, media_file.TvShows), extractor, m, images); err != nil {
			t.Fatal(err)
		}
	}
	if p.searchCalls != 0 || len(p.calls) != 2 {
		t.Fatalf("发生重复抓取或搜索：%d %+v", p.searchCalls, p.calls)
	}
	data, err := os.ReadFile(filepath.Join(dir, "节目.S01E02.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Title string `xml:"title"`
		IDs   []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"uniqueid"`
		Runtime *int `xml:"runtime"`
	}
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "第二回" || len(got.IDs) != 1 || got.IDs[0].Type != "tvdb" || got.IDs[0].Value != "7407799" || got.Runtime != nil {
		t.Fatalf("单集身份或输出错误：%s", data)
	}
}

func TestRulePreservesSpecialSeason(t *testing.T) {
	oldCollector, oldScraper := config.Collector, config.Scraper
	defer func() { config.Collector, config.Scraper = oldCollector, oldScraper }()
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	config.Collector = &config.CollectorConfig{ShowsDir: []string{root}}
	config.Scraper = nil
	mf := &media_file.MediaFile{Path: filepath.Join(dir, "Show.S00E02.mkv"), Dir: dir, Filename: "Show.S00E02.mkv", Suffix: ".mkv"}
	extractor := setAnalysisResponse(t, `{"title":"Show","season":0,"episode":2}`)
	s, err := parseShowFileContext(context.Background(), mf, extractor)
	if err != nil || s.Season != 0 || s.Episode != 2 {
		t.Fatalf("特别篇被改写：%+v %v", s, err)
	}
}

func TestPinnedProviderConsumesAIID(t *testing.T) {
	oldS, oldC, oldL := config.Scraper, config.Collector, config.Log
	defer func() { config.Scraper, config.Collector, config.Log = oldS, oldC, oldL }()
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(filepath.Join(dir, ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".metadata", "source.json"), []byte(`{"provider":"thetvdb"}`), 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"Show","thetvdb_id":"371065","season":1,"episode":2,"confidence":0.99}`}}}})
	}))
	defer server.Close()
	config.Log = &config.LogConfig{Mode: 1}
	utils.InitLogger()
	config.Collector = &config.CollectorConfig{RunMode: 2, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	extractor, err := ai.New(config.LLMConfig{Name: "extractor", Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := new(directProvider)
	m := metadata.NewManager([]metadata.Provider{p}, 0, judgeForTest{})
	name := "Show.S01E02.mkv"
	if err := ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows), extractor, m, nil); err != nil {
		t.Fatal(err)
	}
	if p.searchCalls != 0 {
		t.Fatal("已有AI编号却执行了搜索")
	}
}

func TestManualGroupConstrainsProviderBeforeAI(t *testing.T) {
	oldS, oldC, oldL := config.Scraper, config.Collector, config.Log
	defer func() { config.Scraper, config.Collector, config.Log = oldS, oldC, oldL }()
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(filepath.Join(dir, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmdb", "group.txt"), []byte("group-a"), 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"Show","thetvdb_id":"371065","season":1,"episode":2,"confidence":0.99}`}}}})
	}))
	defer server.Close()
	config.Log = &config.LogConfig{Mode: 1}
	utils.InitLogger()
	config.Collector = &config.CollectorConfig{RunMode: 2, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	extractor, err := ai.New(config.LLMConfig{Name: "extractor", Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	tvdb := new(directProvider)
	tmdb := &directProvider{source: "tmdb", allowSearch: true}
	m := metadata.NewManager([]metadata.Provider{tvdb, tmdb}, 0, judgeForTest{})
	name := "Show.S01E02.mkv"
	if err := ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows), extractor, m, nil); err != nil {
		t.Fatal(err)
	}
	if len(tvdb.calls) != 0 || len(tmdb.calls) != 2 || tmdb.calls[1].Group != "group-a" {
		t.Fatalf("人工分组被AI改变来源：tvdb=%+v tmdb=%+v", tvdb.calls, tmdb.calls)
	}
}

func TestJoinDoesNotInventUnknownEpisode(t *testing.T) {
	oldCollector, oldScraper := config.Collector, config.Scraper
	defer func() { config.Collector, config.Scraper = oldCollector, oldScraper }()
	root := t.TempDir()
	dir := filepath.Join(root, "Show")
	if err := os.MkdirAll(filepath.Join(dir, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmdb", "join.txt"), []byte("1,2,13"), 0644); err != nil {
		t.Fatal(err)
	}
	config.Collector = &config.CollectorConfig{RunMode: 2, ShowsDir: []string{root}}
	config.Scraper = &config.ScraperConfig{}
	extractor := setAnalysisResponse(t, `{"title":"Show","season":1,"episode":null,"thetvdb_id":"371065"}`)
	p := new(directProvider)
	m := metadata.NewManager([]metadata.Provider{p}, 0, judgeForTest{})
	name := "Unknown.mkv"
	mf := media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows)
	if err := ProcessMetadata(context.Background(), mf, extractor, m, nil); err == nil {
		t.Fatal("join映射将未知集号变成了有效集号")
	}
	if len(p.calls) != 0 {
		t.Fatalf("未知集号仍请求了来源：%+v", p.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "Unknown.nfo")); !os.IsNotExist(err) {
		t.Fatalf("未知集号写出了NFO：%v", err)
	}
}
