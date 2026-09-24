package providers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/movies"
	"fengqi/kodi-metadata-tmdb-cli/shows"
)

type integrationState struct {
	analysis, judgments, sources int
	analysisError, judgeError    bool
	reject                       bool
	lowFirstMatch                bool
	extracted, chosenSource      string
	chosenID                     string
	sawBoth                      bool
}
type imageReply struct{}

func (imageReply) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("image-fixture")), Request: r}, nil
}

func setupPipeline(t *testing.T, state *integrationState, sourceHandler http.HandlerFunc) (string, *ai.Client, *metadata.Manager, *artwork.Downloader) {
	t.Helper()
	oldL, oldS, oldT, oldV, oldC := config.LLMs, config.Scraper, config.Tmdb, config.TheTVDB, config.Collector
	t.Cleanup(func() {
		config.LLMs, config.Scraper, config.Tmdb, config.TheTVDB, config.Collector = oldL, oldS, oldT, oldV, oldC
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			state.analysis++
			var input map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if r.Header.Get("Authorization") != "Bearer analysis-key" {
				t.Error("通用LLM凭据与Jev混用")
			}
			if state.analysisError {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": state.extracted}}}})
		case "/v1/systemone":
			state.judgments++
			if r.Header.Get("Authorization") != "Bearer judge-key" {
				t.Error("Jev凭据与通用LLM混用")
			}
			var request struct {
				Questions map[string]struct {
					Type     string                     `json:"type"`
					Criteria map[string]json.RawMessage `json:"criteria"`
				} `json:"questions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(422)
				return
			}
			if state.judgeError {
				w.WriteHeader(429)
				return
			}
			choice := "none"
			probabilities := map[string]float64{}
			answers := map[string]any{}
			sources := map[string]bool{}
			for key, value := range request.Questions["select"].Criteria {
				probabilities[key] = 0
				if key == "none" {
					continue
				}
				var candidate struct {
					Work struct {
						Source string `json:"source"`
						ID     string `json:"id"`
					} `json:"work"`
				}
				if err := json.Unmarshal(value, &candidate); err != nil {
					t.Fatal(err)
				}
				sources[candidate.Work.Source] = true
				if candidate.Work.Source == state.chosenSource && (state.chosenID == "" || candidate.Work.ID == state.chosenID) {
					choice = key
				}
				match := 0.99
				if state.lowFirstMatch && candidate.Work.Source == "tmdb" {
					choice = key
					match = 0.2
				}
				answers["match_"+strings.TrimPrefix(key, "c")] = map[string]any{"type": "noul", "noul": match}
			}
			state.sawBoth = sources["tmdb"] && sources["thetvdb"]
			if state.reject {
				choice = "none"
			}
			probabilities[choice] = 1
			answers["select"] = map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": probabilities}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}})
		default:
			state.sources++
			sourceHandler(w, r)
		}
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	config.LLMs = []config.LLMConfig{{Name: "extract", Type: "openai", BaseURL: server.URL, ApiKey: "analysis-key", Model: "generic-model", TimeoutSeconds: 2}, {Name: "judge", Type: "jev", BaseURL: server.URL, ApiKey: "judge-key", Model: "jev-test", TimeoutSeconds: 2}}
	config.Scraper = &config.ScraperConfig{Providers: []string{"tmdb", "thetvdb"}, ExtractLLM: "extract", SelectLLM: "judge", CacheHours: 24, JevMatchThreshold: 0.8}
	config.Tmdb = &config.TmdbConfig{ApiHost: server.URL, ImageHost: server.URL, ApiKey: "source-key", Language: "zh-CN", TimeoutSeconds: 2}
	config.TheTVDB = &config.TheTVDBConfig{BaseURL: server.URL, Language: "zho", TimeoutSeconds: 2}
	config.Collector = &config.CollectorConfig{RunMode: 2, MoviesDir: []string{filepath.Join(root, "movies")}, ShowsDir: []string{filepath.Join(root, "shows")}}
	extractor, manager, images, err := New()
	if err != nil {
		t.Fatal(err)
	}
	images.Clients["tmdb"] = &http.Client{Transport: imageReply{}}
	images.Clients["thetvdb"] = &http.Client{Transport: imageReply{}}
	return root, extractor, manager, images
}

func TestLLMToOrderedSourcesToJevToEpisodeFiles(t *testing.T) {
	state := &integrationState{extracted: `{"title":"坑王驾到","tmdb_id":"42","thetvdb_id":"371065","season":1,"episode":2,"episode_title":"探地穴 第二回"}`, chosenSource: "thetvdb", lowFirstMatch: true}
	root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		file := ""
		switch r.URL.Path {
		case "/3/tv/42":
			_, _ = w.Write([]byte(`{"id":42,"name":"另一个节目"}`))
			return
		case "/3/tv/42/season/1/episode/2":
			_, _ = w.Write([]byte(`{"id":4202,"name":"另一个单集","season_number":1,"episode_number":2}`))
			return
		case "/dereferrer/series/371065":
			http.Redirect(w, r, "/series/26882341-show", 302)
			return
		case "/series/26882341-show":
			file = "series.html"
		case "/series/26882341-show/allseasons/official":
			file = "allseasons.html"
		case "/series/26882341-show/episodes/7407799":
			file = "episode.html"
		default:
			t.Errorf("有来源编号时不应搜索或访问未知地址：%s", r.URL)
			w.WriteHeader(500)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	})
	dir := filepath.Join(root, "shows", "坑王驾到")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	name := "坑王驾到.S01E02.mkv"
	mf := media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows)
	if err := shows.ProcessMetadata(context.Background(), mf, extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	firstSources := state.sources
	if err := shows.ProcessMetadata(context.Background(), mf, extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	if state.analysis != 2 || state.judgments != 4 || state.sawBoth || state.sources != firstSources {
		t.Fatalf("完整流程或缓存行为错误：%+v", state)
	}
	data, err := os.ReadFile(filepath.Join(dir, "坑王驾到.S01E02.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	var episode struct {
		Runtime int `xml:"runtime"`
		IDs     []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"uniqueid"`
	}
	if err := xml.Unmarshal(data, &episode); err != nil {
		t.Fatal(err)
	}
	if episode.Runtime != 60 || len(episode.IDs) != 1 || episode.IDs[0].Type != "tvdb" || episode.IDs[0].Value != "7407799" {
		t.Fatalf("未使用Jev选择的同源单集：%s", data)
	}
	for _, file := range []string{"tvshow.nfo", "poster.jpg", "fanart.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLLMWithoutIDFetchesAllMovieCandidatesBeforeJev(t *testing.T) {
	state := &integrationState{extracted: `{"title":"Target","year":2020}`, chosenSource: "tmdb", chosenID: "2"}
	fetches := 0
	root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"total_results":2,"results":[{"id":1,"title":"Wrong"},{"id":2,"title":"Target"}]}`))
		case "/3/movie/1":
			fetches++
			_, _ = w.Write([]byte(`{"id":1,"title":"Wrong"}`))
		case "/3/movie/2":
			fetches++
			_, _ = w.Write([]byte(`{"id":2,"title":"Target"}`))
		default:
			t.Errorf("未知请求：%s", r.URL)
			w.WriteHeader(500)
		}
	})
	dir := filepath.Join(root, "movies")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	name := "Target.2020.mkv"
	if err := movies.ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.Movies), extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Target.2020.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if state.analysis != 1 || state.judgments != 1 || fetches != 2 || !strings.Contains(string(data), ">2</uniqueid>") {
		t.Fatalf("在Jev之前已预选候选：%+v %d %s", state, fetches, data)
	}
}

func TestModelFailureOrNoMatchPreservesExistingNFO(t *testing.T) {
	for _, scenario := range []string{"analysis error", "judge error", "none"} {
		t.Run(scenario, func(t *testing.T) {
			state := &integrationState{extracted: `{"title":"Known","tmdb_id":"42"}`, chosenSource: "tmdb", analysisError: scenario == "analysis error", judgeError: scenario == "judge error", reject: scenario == "none"}
			root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/3/movie/42" {
					t.Errorf("不应搜索：%s", r.URL)
				}
				_, _ = w.Write([]byte(`{"id":42,"title":"Known"}`))
			})
			dir := filepath.Join(root, "movies")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			name := "Known.2020.mkv"
			nfoPath := filepath.Join(dir, "Known.2020.nfo")
			if err := os.WriteFile(nfoPath, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			err := movies.ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.Movies), extractor, manager, images)
			if err == nil {
				t.Fatal("模型失败后仍选择了首个结果")
			}
			if scenario == "none" && !errors.Is(err, metadata.ErrNotFound) {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(nfoPath)
			if string(data) != "original" {
				t.Fatal("失败决策覆盖了原NFO")
			}
			if scenario == "analysis error" && (state.sources != 0 || state.judgments != 0) {
				t.Fatalf("LLM失败后仍走了规则分支：%+v", state)
			}
		})
	}
}

func TestManualGroupFiltersUnrelatedCandidateButKeepsExplicitIDConflict(t *testing.T) {
	for _, pinID := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", pinID), func(t *testing.T) {
			state := &integrationState{extracted: `{"title":"Example","season":2,"episode":1}`, chosenSource: "tmdb", chosenID: "42"}
			root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/3/search/tv":
					_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"total_results":2,"results":[{"id":11,"name":"Wrong"},{"id":42,"name":"Example"}]}`))
				case "/3/tv/episode_group/group-a":
					_, _ = w.Write([]byte(`{"id":"group-a","group_count":1,"episode_count":1,"groups":[{"id":"part","name":"Part 2","order":2,"episodes":[{"id":4202,"show_id":42,"season_number":1,"episode_number":2,"order":0}]}]}`))
				case "/3/tv/42":
					_, _ = w.Write([]byte(`{"id":42,"name":"Example"}`))
				case "/3/tv/42/season/1/episode/2":
					_, _ = w.Write([]byte(`{"id":4202,"name":"Episode","season_number":1,"episode_number":2}`))
				default:
					t.Errorf("不应请求不相关作品：%s", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			dir := filepath.Join(root, "shows", "Example")
			if err := os.MkdirAll(filepath.Join(dir, "tmdb"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "tmdb", "group.txt"), []byte("group-a"), 0644); err != nil {
				t.Fatal(err)
			}
			if pinID {
				if err := os.WriteFile(filepath.Join(dir, "tmdb", "id.txt"), []byte("11"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			name := "Example.S02E01.mkv"
			err := shows.ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.TvShows), extractor, manager, images)
			if pinID {
				if err == nil || state.judgments != 0 {
					t.Fatalf("人工节目与分组冲突被忽略：%v %+v", err, state)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if state.judgments != 1 {
				t.Fatalf("正确分组候选未交给Jev：%+v", state)
			}
			data, err := os.ReadFile(filepath.Join(dir, "Example.S02E01.nfo"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "<season>2</season>") || !strings.Contains(string(data), "<episode>1</episode>") {
				t.Fatalf("分组坐标丢失：%s", data)
			}
		})
	}
}

func TestConfiguredCLIRunsBothModelsBeforeWritingMovie(t *testing.T) {
	state := &integrationState{extracted: `{"title":"CLI","year":2020,"tmdb_id":"42"}`, chosenSource: "tmdb"}
	root, _, _, _ := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/movie/42" {
			t.Errorf("CLI不应搜索或访问未知来源：%s", r.URL)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"title":"Actual CLI movie"}`))
	})
	dir := filepath.Join(root, "movies")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "shows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLI.2020.mkv"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	settings := config.Config{LLMs: config.LLMs, Scraper: config.Scraper, Tmdb: config.Tmdb, TheTVDB: config.TheTVDB, Collector: config.Collector}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "config.json")
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", ".", "-config", file, "-mode", "2")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("配置文件→CLI→模型→输出失败：%v\n%s", err, output)
	}
	nfoData, err := os.ReadFile(filepath.Join(dir, "CLI.2020.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nfoData), "<title>Actual CLI movie</title>") || !strings.Contains(string(nfoData), ">42</uniqueid>") {
		t.Fatalf("CLI没有使用已选数据源事实：%s", nfoData)
	}
}

func TestFirstConfirmedSourceStopsPipeline(t *testing.T) {
	state := &integrationState{extracted: `{"title":"Priority","tmdb_id":"42","thetvdb_id":"371065","season":1,"episode":2}`, chosenSource: "tmdb"}
	root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/tv/42":
			_, _ = w.Write([]byte(`{"id":42,"name":"Priority"}`))
		case "/3/tv/42/season/1/episode/2":
			_, _ = w.Write([]byte(`{"id":4202,"name":"Priority episode","season_number":1,"episode_number":2}`))
		default:
			t.Errorf("首来源确认后不应请求后续来源：%s", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	dir := filepath.Join(root, "shows", "Priority")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	mf := media_file.NewMediaFile(filepath.Join(dir, "Priority.S01E02.mkv"), "Priority.S01E02.mkv", media_file.TvShows)
	if err := shows.ProcessMetadata(context.Background(), mf, extractor, manager, images); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Priority.S01E02.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if state.judgments != 1 || state.sources != 2 || !strings.Contains(string(data), ">4202</uniqueid>") {
		t.Fatalf("优先来源没有完整独立输出：%+v %s", state, data)
	}
}

func TestSearchPage404IsFailureNotSourceMiss(t *testing.T) {
	state := &integrationState{extracted: `{"title":"Pagination","thetvdb_id":"371065","season":1,"episode":2}`, chosenSource: "thetvdb"}
	root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/search/tv" {
			t.Errorf("分页故障不应继续后续来源：%s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(404)
			return
		}
		var hits []map[string]any
		for i := 1; i <= 20; i++ {
			hits = append(hits, map[string]any{"id": i, "name": fmt.Sprintf("Candidate %d", i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": 1, "total_pages": 2, "total_results": 21, "results": hits})
	})
	dir := filepath.Join(root, "shows", "Pagination")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Pagination.S01E02.nfo")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	mf := media_file.NewMediaFile(filepath.Join(dir, "Pagination.S01E02.mkv"), "Pagination.S01E02.mkv", media_file.TvShows)
	err := shows.ProcessMetadata(context.Background(), mf, extractor, manager, images)
	if err == nil || errors.Is(err, metadata.ErrNotFound) || state.sources != 2 || state.judgments != 0 {
		t.Fatalf("分页404被误当未匹配：%v %+v", err, state)
	}
	content, readErr := os.ReadFile(file)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("分页失败覆盖NFO：%v %s", readErr, content)
	}
}

func TestSpecCLIRelativeShowLibraryUsesShowReference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "metadata-cli")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译CLI失败：%v\n%s", err, output)
	}
	state := &integrationState{extracted: `{"title":"Relative","tmdb_id":"99","season":1,"episode":2}`, chosenSource: "tmdb", chosenID: "42"}
	root, _, _, _ := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/tv/42":
			_, _ = w.Write([]byte(`{"id":42,"name":"Manual show"}`))
		case "/3/tv/42/season/1/episode/2":
			_, _ = w.Write([]byte(`{"id":4202,"name":"Manual episode","season_number":1,"episode_number":2}`))
		default:
			t.Errorf("未使用节目根人工来源：%s", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	library := filepath.Join(root, "shows")
	showRoot := filepath.Join(library, "Relative")
	seasonRoot := filepath.Join(showRoot, "Season 01")
	for _, dir := range []string{seasonRoot, filepath.Join(showRoot, ".metadata")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(showRoot, ".metadata", "source.json"), []byte(`{"provider":"tmdb","kind":"show","id":"42"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonRoot, "Relative.S01E02.mkv"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	config.Collector.ShowsDir = []string{"."}
	config.Collector.MoviesDir = nil
	config.Collector.RunMode = config.CollectorRunModeSpec
	settings := config.Config{LLMs: config.LLMs, Scraper: config.Scraper, Tmdb: config.Tmdb, TheTVDB: config.TheTVDB, Collector: config.Collector}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "config.json")
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "-config", file, "-mode", "3")
	command.Dir = library
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("相对节目库CLI失败：%v\n%s", err, output)
	}
	showData, err := os.ReadFile(filepath.Join(showRoot, "tvshow.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	episodeData, err := os.ReadFile(filepath.Join(seasonRoot, "Relative.S01E02.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(seasonRoot, "tvshow.nfo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("节目NFO错误写入季目录：%v", err)
	}
	if state.analysis != 1 || state.judgments != 1 || state.sources != 2 || !strings.Contains(string(showData), ">42</uniqueid>") || !strings.Contains(string(episodeData), ">4202</uniqueid>") || !strings.Contains(string(episodeData), "<season>1</season>") || !strings.Contains(string(episodeData), "<episode>2</episode>") {
		t.Fatalf("节目根引用或输出错误：%+v\n%s\n%s", state, showData, episodeData)
	}
}

func TestMalformedTVDBSearchStopsBeforeNextSourceAndJudge(t *testing.T) {
	for _, hit := range []string{`null`, `{}`, `{"id":1,"name":"Target","type":"unknown"}`} {
		t.Run(hit, func(t *testing.T) {
			state := &integrationState{extracted: `{"title":"Malformed","season":1,"episode":2}`, chosenSource: "tmdb"}
			root, extractor, _, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/search":
					fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL='http://%s/web/search/queries'</script>`, r.Host)
				case "/web/search/queries":
					fmt.Fprintf(w, `{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[%s]}]}`, hit)
				default:
					t.Errorf("协议错误后不应请求后续来源：%s", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			config.Scraper.Providers = []string{"thetvdb", "tmdb"}
			_, manager, _, err := New()
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "shows", "Malformed")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "Malformed.S01E02.nfo")
			if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			mf := media_file.NewMediaFile(filepath.Join(dir, "Malformed.S01E02.mkv"), "Malformed.S01E02.mkv", media_file.TvShows)
			err = shows.ProcessMetadata(context.Background(), mf, extractor, manager, images)
			if err == nil || errors.Is(err, metadata.ErrNotFound) || state.sources != 2 || state.judgments != 0 || state.analysis != 1 {
				t.Fatalf("畸形候选未停止管线：%v %+v", err, state)
			}
			data, readErr := os.ReadFile(file)
			if readErr != nil || string(data) != "original" {
				t.Fatalf("来源协议错误覆盖NFO：%v %s", readErr, data)
			}
		})
	}
}

func TestNullExtractionWithManualMovieReferenceStopsBeforeSources(t *testing.T) {
	state := &integrationState{extracted: `null`, chosenSource: "tmdb"}
	root, extractor, manager, images := setupPipeline(t, state, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/movie/42" {
			t.Errorf("意外来源请求：%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":42,"title":"Manual movie"}`))
	})
	dir := filepath.Join(root, "movies")
	if err := os.MkdirAll(filepath.Join(dir, ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	name := "Manual.mkv"
	if err := os.WriteFile(filepath.Join(dir, ".metadata", name+".source.json"), []byte(`{"provider":"tmdb","kind":"movie","id":"42"}`), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Manual.nfo")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	err := movies.ProcessMetadata(context.Background(), media_file.NewMediaFile(filepath.Join(dir, name), name, media_file.Movies), extractor, manager, images)
	if err == nil || state.analysis != 1 || state.sources != 0 || state.judgments != 0 {
		t.Fatalf("null 提取结果未停止管线：%v %+v", err, state)
	}
	data, readErr := os.ReadFile(file)
	if readErr != nil || string(data) != "original" {
		t.Fatalf("null 提取结果覆盖NFO：%v %s", readErr, data)
	}
}
