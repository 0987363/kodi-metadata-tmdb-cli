package collector

import (
	"context"
	"encoding/json"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type runSource struct{ searches, batches int }

func (p *runSource) Name() string                  { return "tmdb" }
func (p *runSource) Scope() string                 { return "test" }
func (p *runSource) Supports(k metadata.Kind) bool { return k == metadata.Movie || k == metadata.Show }
func (p *runSource) Search(_ context.Context, q metadata.Query) ([]metadata.Candidate, error) {
	p.searches++
	return []metadata.Candidate{{Ref: metadata.Ref{Provider: "tmdb", Kind: q.Kind, ID: "42"}, Title: "来源标题"}}, nil
}
func (p *runSource) Fetch(_ context.Context, r metadata.Request) (*metadata.Record, error) {
	return &metadata.Record{Ref: r.Ref, Title: "电影事实"}, nil
}
func (p *runSource) FetchSeries(_ context.Context, r metadata.SeriesRequest) (*metadata.SeriesResult, error) {
	p.batches++
	result := &metadata.SeriesResult{Work: &metadata.Record{Ref: r.Ref, Title: "节目事实"}}
	for _, key := range r.Episodes {
		result.Episodes = append(result.Episodes, metadata.SeriesEpisode{Key: key, Record: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: "100"}, Title: "单集事实", SeasonNumber: key.Season, EpisodeNumber: key.Episode}})
	}
	return result, nil
}

type runJudge struct{ calls int }

func (j *runJudge) Select(_ context.Context, _ metadata.Request, _ []metadata.Option) (int, error) {
	j.calls++
	return 0, nil
}

func TestRunUsesAIClassificationInMixedDirectory(t *testing.T) {
	oldCollector, oldScraper := config.Collector, config.Scraper
	t.Cleanup(func() { config.Collector, config.Scraper = oldCollector, oldScraper })
	config.Collector = &config.CollectorConfig{}
	config.Scraper = &config.ScraperConfig{}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "节目"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"trailer.S01E01.mkv", "节目/01.mkv"} {
		if err := os.WriteFile(filepath.Join(root, relative), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		payload := `{"items":[{"relative_path":"trailer.S01E01.mkv","media_type":"movie","title":"独立作品","season":null,"episode":null},{"relative_path":"节目/01.mkv","media_type":"tv","series_root":"节目","title":"节目","season":0,"episode":1}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": payload}}}})
	}))
	defer server.Close()
	client, err := ai.New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	source := &runSource{}
	judge := &runJudge{}
	if err := Run(context.Background(), root, client, metadata.NewManager([]metadata.Provider{source}, 0, judge), &artwork.Downloader{}); err != nil {
		t.Fatal(err)
	}
	movie, err := os.ReadFile(filepath.Join(root, "trailer.S01E01.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	episode, err := os.ReadFile(filepath.Join(root, "节目/01.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(movie), "<movie>") || !strings.Contains(string(episode), "<season>0</season>") {
		t.Fatalf("AI类型或零季丢失: %s %s", movie, episode)
	}
	if calls != 1 || judge.calls != 2 || source.batches != 2 {
		t.Fatalf("重复提取/逐集判断: model=%d judge=%d batches=%d", calls, judge.calls, source.batches)
	}
}

func TestRunRejectsOutputCollisionsBeforeSourceRequests(t *testing.T) {
	for _, test := range []struct {
		name  string
		paths []string
		items string
	}{
		{"show control file", []string{"tvshow.mkv"}, `[{"relative_path":"tvshow.mkv","media_type":"tv","series_root":".","title":"节目","season":1,"episode":1}]`},
		{"same stem movies", []string{"A.mkv", "A.mp4"}, `[{"relative_path":"A.mkv","media_type":"movie","title":"作品"},{"relative_path":"A.mp4","media_type":"movie","title":"作品"}]`},
		{"cross task", []string{"tvshow.mkv", "episode.mkv"}, `[{"relative_path":"tvshow.mkv","media_type":"movie","title":"电影"},{"relative_path":"episode.mkv","media_type":"tv","series_root":".","title":"节目","season":1,"episode":1}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldCollector, oldScraper := config.Collector, config.Scraper
			t.Cleanup(func() { config.Collector, config.Scraper = oldCollector, oldScraper })
			config.Collector = &config.CollectorConfig{}
			config.Scraper = &config.ScraperConfig{}
			root := t.TempDir()
			for _, file := range test.paths {
				if err := os.WriteFile(filepath.Join(root, file), nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			prior := filepath.Join(root, "tvshow.nfo")
			if err := os.WriteFile(prior, []byte("prior"), 0644); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":` + test.items + `}`}}}})
			}))
			defer server.Close()
			client, err := ai.New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test"})
			if err != nil {
				t.Fatal(err)
			}
			source := &runSource{}
			err = Run(context.Background(), root, client, metadata.NewManager([]metadata.Provider{source}, 0, &runJudge{}), &artwork.Downloader{})
			if err == nil || source.searches != 0 || source.batches != 0 {
				t.Fatalf("冲突未在来源访问前拒绝: %v %+v", err, source)
			}
			data, _ := os.ReadFile(prior)
			if string(data) != "prior" {
				t.Fatalf("已有NFO被覆盖: %s", data)
			}
		})
	}
}
