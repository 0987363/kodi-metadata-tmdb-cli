package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/collector"
	"fengqi/kodi-metadata-tmdb-cli/config"
)

type pipelineFixture struct {
	t                                      *testing.T
	root                                   string
	server                                 *httptest.Server
	mu                                     sync.Mutex
	tasks                                  []string
	requests                               []string
	analysis, local, choice, judgeType     string
	choices                                []string
	judgeCalls                             int
	rawSource                              func(http.ResponseWriter, *http.Request) bool
	modelStatus, judgeStatus, sourceStatus int
	providers                              []string
	source                                 func(*http.Request) (any, bool)
}

func fixturePipeline(t *testing.T) *pipelineFixture {
	t.Helper()
	f := &pipelineFixture{t: t, root: t.TempDir(), choice: "c0", judgeType: "jev", providers: []string{"tmdb"}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	oldL, oldS, oldT, oldV, oldC := config.LLMs, config.Scraper, config.Tmdb, config.TheTVDB, config.Collector
	t.Cleanup(func() {
		config.LLMs, config.Scraper, config.Tmdb, config.TheTVDB, config.Collector = oldL, oldS, oldT, oldV, oldC
	})
	return f
}
func (f *pipelineFixture) configure() {
	config.LLMs = []config.LLMConfig{{Name: "extract", Type: "openai", BaseURL: f.server.URL, ApiKey: "extract-key", Model: "extract-model"}, {Name: "judge", Type: f.judgeType, BaseURL: f.server.URL, ApiKey: "judge-key", Model: "judge-model"}}
	config.Scraper = &config.ScraperConfig{Providers: f.providers, ExtractLLM: "extract", SelectLLM: "judge", JevMatchThreshold: 0.8}
	config.Tmdb = &config.TmdbConfig{ApiHost: f.server.URL, ImageHost: f.server.URL, ApiKey: "source-key", TimeoutSeconds: 2}
	config.TheTVDB = &config.TheTVDBConfig{BaseURL: f.server.URL, Language: "zho", TimeoutSeconds: 2}
	config.Collector = &config.CollectorConfig{}
}
func (f *pipelineFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.String())
	switch r.URL.Path {
	case "/chat/completions":
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) < 2 {
			f.t.Errorf("模型请求无效: %v", err)
			w.WriteHeader(400)
			return
		}
		var prompt struct {
			Task       string                     `json:"task"`
			Candidates map[string]json.RawMessage `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &prompt); err != nil {
			f.t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.tasks = append(f.tasks, prompt.Task)
		key, model := "extract-key", "extract-model"
		if prompt.Task == "select_metadata_candidate" {
			key, model = "judge-key", "judge-model"
		}
		if r.Header.Get("Authorization") != "Bearer "+key || body.Model != model {
			f.t.Errorf("具名模型串线: %s %s %s", prompt.Task, r.Header.Get("Authorization"), body.Model)
			w.WriteHeader(400)
			return
		}
		if f.modelStatus != 0 {
			w.WriteHeader(f.modelStatus)
			return
		}
		if prompt.Task == "select_metadata_candidate" && f.judgeStatus != 0 {
			w.WriteHeader(f.judgeStatus)
			return
		}
		content := ""
		switch prompt.Task {
		case "extract_media_identity":
			content = f.analysis
		case "describe_local_metadata":
			content = f.local
		case "select_metadata_candidate":
			if len(prompt.Candidates) == 0 {
				f.t.Error("判断候选为空")
			}
			content = fmt.Sprintf(`{"choice":%q}`, f.choice)
		default:
			f.t.Errorf("未知模型任务: %s", prompt.Task)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	case "/v1/systemone":
		f.tasks = append(f.tasks, "jev")
		if r.Header.Get("Authorization") != "Bearer judge-key" {
			f.t.Error("JEV 凭据错误")
		}
		var body struct {
			State     json.RawMessage `json:"state"`
			Questions map[string]struct {
				Criteria map[string]json.RawMessage `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, forbidden := range []string{`"episodes"`, `"files"`, `"season"`, `"episode"`, `"group"`} {
			if strings.Contains(string(body.State), forbidden) {
				f.t.Errorf("判断上下文泄漏季集字段：%s", forbidden)
			}
			for key, raw := range body.Questions["select"].Criteria {
				if key != "none" && strings.Contains(string(raw), forbidden) {
					f.t.Errorf("作品候选泄漏季集字段：%s", forbidden)
				}
			}
		}
		if f.judgeStatus != 0 {
			w.WriteHeader(f.judgeStatus)
			return
		}
		choice := f.choice
		if f.judgeCalls < len(f.choices) {
			choice = f.choices[f.judgeCalls]
		}
		f.judgeCalls++
		probabilities := map[string]float64{}
		answers := map[string]any{}
		for key := range body.Questions["select"].Criteria {
			probabilities[key] = 0
			if key != "none" {
				answers["match_"+strings.TrimPrefix(key, "c")] = map[string]any{"type": "noul", "noul": 0.99}
			}
		}
		probabilities[choice] = 1
		answers["select"] = map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": probabilities}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "judge-model", "answers": answers})
	default:
		if f.rawSource != nil && f.rawSource(w, r) {
			return
		}
		if f.sourceStatus != 0 {
			w.WriteHeader(f.sourceStatus)
			return
		}
		if f.source != nil {
			if body, ok := f.source(r); ok {
				_ = json.NewEncoder(w).Encode(body)
				return
			}
		}
		f.t.Errorf("意外来源请求: %s", r.URL)
		w.WriteHeader(500)
	}
}
func (f *pipelineFixture) file(relative string) {
	f.t.Helper()
	p := filepath.Join(f.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0644); err != nil {
		f.t.Fatal(err)
	}
}

type fixtureImageTransport struct{}

func (fixtureImageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("image-fixture")), Request: r}, nil
}
func (f *pipelineFixture) run() error {
	f.t.Helper()
	f.configure()
	e, m, i, err := New()
	if err != nil {
		return err
	}
	i.Clients["tmdb"] = &http.Client{Transport: fixtureImageTransport{}}
	i.Clients["thetvdb"] = &http.Client{Transport: fixtureImageTransport{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return collector.Run(ctx, f.root, e, m, i)
}
func (f *pipelineFixture) snapshot() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tasks...), append([]string(nil), f.requests...)
}
func readNFO(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func tmdbMovie(r *http.Request) (any, bool) {
	if r.URL.Path == "/3/search/movie" {
		return map[string]any{"page": 1, "total_pages": 1, "total_results": 1, "results": []any{map[string]any{"id": 42, "title": "Source Movie", "release_date": "2020-01-01"}}}, true
	}
	if r.URL.Path == "/3/movie/42" {
		return map[string]any{"id": 42, "title": "Source Movie", "runtime": 123}, true
	}
	return nil, false
}
func tmdbShow(r *http.Request) (any, bool) {
	if r.URL.Path == "/3/search/tv" {
		return map[string]any{"page": 1, "total_pages": 1, "total_results": 1, "results": []any{map[string]any{"id": 42, "name": "Source Show", "first_air_date": "2017-01-01"}}}, true
	}
	if r.URL.Path != "/3/tv/42" {
		return nil, false
	}
	result := map[string]any{"id": 42, "name": "Source Show", "number_of_seasons": 1, "number_of_episodes": 2}
	for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
		switch path {
		case "aggregate_credits":
			result[path] = map[string]any{"cast": []any{}, "crew": []any{}}
		case "content_ratings":
			result[path] = map[string]any{"results": []any{}}
		case "images":
			result[path] = map[string]any{"posters": []any{}}
		case "external_ids":
			result[path] = map[string]any{"imdb_id": nil}
		case "season/1":
			result[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 42, "season_number": 1, "episode_number": 1, "name": "Source Episode 1"}, map[string]any{"id": 102, "show_id": 42, "season_number": 1, "episode_number": 2, "name": "Source Episode 2"}}}
		case "season/1/episode/1/credits", "season/1/episode/2/credits":
			result[path] = map[string]any{"cast": []any{}, "crew": []any{}}
		case "season/1/episode/1/images", "season/1/episode/2/images":
			result[path] = map[string]any{"stills": []any{}}
		case "season/1/episode/1/external_ids", "season/1/episode/2/external_ids":
			result[path] = map[string]any{"imdb_id": nil}
		default:
			return nil, false
		}
	}
	return result, true
}
func TestPipelineMovieNamedModelsAndSourceFact(t *testing.T) {
	f := fixturePipeline(t)
	f.judgeType = "openai"
	f.file("Movie.mkv")
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Hint"}]}`
	f.source = tmdbMovie
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	got := readNFO(t, filepath.Join(f.root, "Movie.nfo"))
	if !strings.Contains(got, "<title>Source Movie</title>") || !strings.Contains(got, ">42</uniqueid>") || !strings.Contains(got, "<runtime>123</runtime>") {
		t.Fatalf("电影事实错误: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity select_metadata_candidate]" {
		t.Fatalf("模型任务错误: %v", tasks)
	}
}
func TestPipelineShowBatchAndJEV(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Show/Season 01/Show.S01E01.mkv")
	f.file("Show/Season 01/Show.S01E02.mkv")
	f.analysis = `{"items":[{"relative_path":"Show/Season 01/Show.S01E01.mkv","media_type":"tv","series_root":"Show","title":"Show","season":1,"episode":1},{"relative_path":"Show/Season 01/Show.S01E02.mkv","media_type":"tv","series_root":"Show","title":"Show","season":1,"episode":2}]}`
	f.source = tmdbShow
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show/Season 01/Show.S01E01.nfo", "Show/Season 01/Show.S01E02.nfo"} {
		got := readNFO(t, filepath.Join(f.root, name))
		if !strings.Contains(got, "<season>1</season>") || !strings.Contains(got, "<episode>") {
			t.Fatalf("单集错误: %s", got)
		}
	}
	if got := readNFO(t, filepath.Join(f.root, "Show/tvshow.nfo")); !strings.Contains(got, "Source Show") {
		t.Fatalf("节目错误: %s", got)
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity jev]" {
		t.Fatalf("同节目判断次数错误: %v", tasks)
	}
	count := 0
	for _, p := range requests {
		if strings.HasPrefix(p, "/3/tv/42?") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("应基础和详情各一次: %v", requests)
	}
}
func TestPipelineSourceFailurePreservesNFO(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Movie.mkv")
	nfo := filepath.Join(f.root, "Movie.nfo")
	_ = os.WriteFile(nfo, []byte("original"), 0644)
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Hint","tmdb_id":"42"}]}`
	f.sourceStatus = 500
	if err := f.run(); err == nil {
		t.Fatal("来源错误未返回")
	}
	if got := readNFO(t, nfo); got != "original" {
		t.Fatalf("旧 NFO 被覆盖: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("来源错误触发了本地生成: %v", tasks)
	}
}
func TestPipelineNoMatchCreatesLocalIdentityWithoutFacts(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Unknown.mkv")
	f.analysis = `{"items":[{"relative_path":"Unknown.mkv","media_type":"movie","title":"Unknown"}]}`
	f.local = `{"items":[{"relative_path":"Unknown.mkv","title":"Filename Only","plot":"","genres":[]}]}`
	f.source = func(r *http.Request) (any, bool) {
		if r.URL.Path == "/3/search/movie" {
			return map[string]any{"page": 1, "total_pages": 0, "total_results": 0, "results": []any{}}, true
		}
		return nil, false
	}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	got := readNFO(t, filepath.Join(f.root, "Unknown.nfo"))
	if !strings.Contains(got, "<title>Filename Only</title>") || !strings.Contains(got, "type=\"local\"") {
		t.Fatalf("本地身份错误: %s", got)
	}
	for _, fact := range []string{"<actor>", "<premiered>", "<rating>", "<runtime>", "<url>"} {
		if strings.Contains(got, fact) {
			t.Fatalf("本地虚构事实 %s: %s", fact, got)
		}
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity describe_local_metadata]" {
		t.Fatalf("本地任务错误: %v", tasks)
	}
}
func TestPipelineMalformedClassificationPreservesNFO(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Movie.mkv")
	nfo := filepath.Join(f.root, "Movie.nfo")
	_ = os.WriteFile(nfo, []byte("original"), 0644)
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"unknown","title":"Hint"}]}`
	if err := f.run(); err == nil {
		t.Fatal("无效分类未拒绝")
	}
	if got := readNFO(t, nfo); got != "original" {
		t.Fatalf("旧 NFO 被覆盖: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("无效分类启动后续任务: %v", tasks)
	}
}

func TestPipelineJudgeNoneStopsWithoutLocalOutput(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Unmatched.mkv")
	f.analysis = `{"items":[{"relative_path":"Unmatched.mkv","media_type":"movie","title":"Unmatched"}]}`
	f.source = tmdbMovie
	f.choice = "none"
	prior := filepath.Join(f.root, "Unmatched.nfo")
	if err := os.WriteFile(prior, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := f.run(); err == nil {
		t.Fatal("Jev 拒绝应退出")
	}
	if readNFO(t, prior) != "original" {
		t.Fatal("拒绝后仍写入本地 NFO")
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity jev]" {
		t.Fatalf("拒绝后继续模型任务：%v", tasks)
	}
	for _, request := range requests {
		if strings.HasPrefix(request, "/3/movie/") {
			t.Fatalf("判断前或拒绝后请求了详情：%v", requests)
		}
	}
}

func TestPipelineJudgeFailurePreservesPreviousNFO(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Movie.mkv")
	nfo := filepath.Join(f.root, "Movie.nfo")
	_ = os.WriteFile(nfo, []byte("original"), 0644)
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Hint"}]}`
	f.source = tmdbMovie
	f.judgeStatus = 429
	if err := f.run(); err == nil {
		t.Fatal("模型错误未返回")
	}
	if got := readNFO(t, nfo); got != "original" {
		t.Fatalf("模型错误覆盖 NFO: %s", got)
	}
}
func TestPipelineTMDbMalformedBatchPreservesPreviousShowNFO(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Show/Show.S01E01.mkv")
	nfo := filepath.Join(f.root, "Show/tvshow.nfo")
	_ = os.WriteFile(nfo, []byte("original"), 0644)
	f.analysis = `{"items":[{"relative_path":"Show/Show.S01E01.mkv","media_type":"tv","series_root":"Show","title":"Show","tmdb_id":"42","season":1,"episode":1}]}`
	f.source = func(r *http.Request) (any, bool) {
		if r.URL.Path == "/3/search/tv" {
			return tmdbShow(r)
		}
		if r.URL.Path == "/3/tv/42" {
			return map[string]any{"id": 42, "name": "Show", "season/1": map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 42, "season_number": 1, "episode_number": 1, "name": "Episode"}}}}, true
		}
		return nil, false
	}
	if err := f.run(); err == nil {
		t.Fatal("缺少追加对象未报错")
	}
	if got := readNFO(t, nfo); got != "original" {
		t.Fatalf("错误来源覆盖 NFO: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("来源协议错误触发判断或本地任务: %v", tasks)
	}
}

func TestCLIProcessesExactlyOnePathWithNamedModels(t *testing.T) {
	f := fixturePipeline(t)
	f.judgeType = "openai"
	f.file("Movie.mkv")
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Hint"}]}`
	f.source = tmdbMovie
	f.configure()
	settings := config.Config{LLMs: config.LLMs, Scraper: config.Scraper, Tmdb: config.Tmdb, TheTVDB: config.TheTVDB, Collector: config.Collector}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configFile, data, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", ".", "--config", configFile, "--path", f.root)
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI 失败: %v\n%s", err, output)
	}
	if got := readNFO(t, filepath.Join(f.root, "Movie.nfo")); !strings.Contains(got, "Source Movie") || !strings.Contains(got, ">42</uniqueid>") {
		t.Fatalf("CLI 输出错误: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity select_metadata_candidate]" {
		t.Fatalf("CLI 模型隔离错误: %v", tasks)
	}
	duplicate := exec.CommandContext(ctx, "go", "run", ".", "--config", configFile, "--path", f.root, "--path", f.root)
	duplicate.Dir = ".."
	output, err = duplicate.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "--path 只能指定一次") {
		t.Fatalf("重复 path 未拒绝: %v %s", err, output)
	}
}
func TestPipelineOrderedSourcesEmptyAdvancesThenStops(t *testing.T) {
	f := fixturePipeline(t)
	f.providers = []string{"tmdb", "thetvdb"}
	f.file("Show/Show.S01E02.mkv")
	f.analysis = `{"items":[{"relative_path":"Show/Show.S01E02.mkv","media_type":"tv","series_root":"Show","title":"坑王驾到","season":1,"episode":2}]}`
	series, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", "series.html"))
	if err != nil {
		t.Fatal(err)
	}
	all, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", "allseasons.html"))
	if err != nil {
		t.Fatal(err)
	}
	episode, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", "episode.html"))
	if err != nil {
		t.Fatal(err)
	}
	f.rawSource = func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/search":
			fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL = '%s/web/search/queries';</script>`, f.server.URL)
		case "/web/search/queries":
			_, _ = w.Write([]byte(`{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":100,"exhaustiveNbHits":true,"hits":[{"id":371065,"name":"坑王驾到","slug":"26882341-show","type":"series","year":"2016"}]}]}`))
		case "/dereferrer/series/371065":
			http.Redirect(w, r, "/series/26882341-show", 302)
		case "/series/26882341-show":
			_, _ = w.Write(series)
		case "/series/26882341-show/allseasons/official":
			_, _ = w.Write(all)
		case "/series/26882341-show/seasons/official/1":
			_, _ = w.Write([]byte(`<div id="episodes"><table><tbody><tr><td>S01E02</td><td><a href="/series/26882341-show/episodes/7407799">第二回</a></td><td></td><td>60</td><td></td></tr></tbody></table></div>`))
		case "/series/26882341-show/episodes/7407799":
			_, _ = w.Write(episode)
		default:
			return false
		}
		return true
	}
	f.source = func(r *http.Request) (any, bool) {
		if r.URL.Path == "/3/search/tv" {
			return map[string]any{"page": 1, "total_pages": 0, "total_results": 0, "results": []any{}}, true
		}
		if body, ok := tmdbShow(r); ok {
			return body, true
		}
		return nil, false
	}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	got := readNFO(t, filepath.Join(f.root, "Show/Show.S01E02.nfo"))
	if !strings.Contains(got, ">7407799</uniqueid>") {
		t.Fatalf("后续来源单集未采用: %s", got)
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity jev]" {
		t.Fatalf("未逐来源判断: %v", tasks)
	}
	first, second := -1, -1
	for i, p := range requests {
		if strings.HasPrefix(p, "/3/search/tv?") {
			first = i
		}
		if strings.HasPrefix(p, "/search?") && second < 0 {
			second = i
		}
	}
	if first < 0 || second <= first {
		t.Fatalf("来源顺序错误: %v", requests)
	}
}

func TestPipelineFirstConfirmedSourceStopsBeforeSecond(t *testing.T) {
	f := fixturePipeline(t)
	f.providers = []string{"tmdb", "thetvdb"}
	f.file("Show/Show.S01E01.mkv")
	f.analysis = `{"items":[{"relative_path":"Show/Show.S01E01.mkv","media_type":"tv","series_root":"Show","title":"Show","tmdb_id":"42","thetvdb_id":"371065","season":1,"episode":1}]}`
	f.source = tmdbShow
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	if got := readNFO(t, filepath.Join(f.root, "Show/Show.S01E01.nfo")); !strings.Contains(got, ">101</uniqueid>") {
		t.Fatalf("首来源单集未写入: %s", got)
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("首来源确认后仍判断: %v", tasks)
	}
	for _, p := range requests {
		if strings.Contains(p, "/series/") || strings.HasPrefix(p, "/search?") {
			t.Fatalf("首来源确认后访问后续来源: %v", requests)
		}
	}
}
func TestPipelineSearchPageFailureDoesNotBecomeLocalMiss(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Movie.mkv")
	nfo := filepath.Join(f.root, "Movie.nfo")
	_ = os.WriteFile(nfo, []byte("original"), 0644)
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Movie"}]}`
	f.rawSource = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/3/search/movie" {
			return false
		}
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(404)
			return true
		}
		hits := []any{}
		for n := 1; n <= 20; n++ {
			hits = append(hits, map[string]any{"id": n, "title": fmt.Sprintf("Candidate %d", n)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": 1, "total_pages": 2, "total_results": 21, "results": hits})
		return true
	}
	if err := f.run(); err == nil {
		t.Fatal("来源分页错误未返回")
	}
	if got := readNFO(t, nfo); got != "original" {
		t.Fatalf("分页错误覆盖 NFO: %s", got)
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("分页错误触发本地生成: %v", tasks)
	}
	found := false
	for _, p := range requests {
		if strings.Contains(p, "page=2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未请求第二页: %v", requests)
	}
}

func TestPipelineGeneralJudgeNoneAndFailurePreserveNFO(t *testing.T) {
	for _, scenario := range []string{"none", "error"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixturePipeline(t)
			f.judgeType = "openai"
			f.file("Movie.mkv")
			nfo := filepath.Join(f.root, "Movie.nfo")
			_ = os.WriteFile(nfo, []byte("original"), 0644)
			f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Hint"}]}`
			f.source = tmdbMovie
			f.local = `{"items":[{"relative_path":"Movie.mkv","title":"Local Movie","plot":"","genres":[]}]}`
			if scenario == "none" {
				f.choice = "none"
			} else {
				f.judgeStatus = 500
			}
			err := f.run()
			got := readNFO(t, nfo)
			tasks, _ := f.snapshot()
			if err == nil || got != "original" || fmt.Sprint(tasks) != "[extract_media_identity select_metadata_candidate]" {
				t.Fatalf("通用判断拒绝或错误未立即退出保护 NFO: %v %s %v", err, got, tasks)
			}

		})
	}
}

func TestPipelineEmptyAlternateTitlePreservesVerifiedMovie(t *testing.T) {
	f := fixturePipeline(t)
	f.file("Movie.mkv")
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"中文片名","chs_title":"中文片名","eng_title":"English Alias","year":2020,"tmdb_id":"42"}]}`
	var queries []string
	f.source = func(r *http.Request) (any, bool) {
		if r.URL.Path == "/3/search/movie" {
			title := r.URL.Query().Get("query")
			queries = append(queries, title)
			if title == "English Alias" {
				return map[string]any{"page": 1, "total_pages": 1, "total_results": 0, "results": []any{}}, true
			}
			if title != "中文片名" {
				f.t.Errorf("意外查询: %s", title)
			}
		}
		return tmdbMovie(r)
	}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	got := readNFO(t, filepath.Join(f.root, "Movie.nfo"))
	if !strings.Contains(got, "<title>Source Movie</title>") || !strings.Contains(got, ">42</uniqueid>") {
		t.Fatalf("真实候选未进入输出: %s", got)
	}
	tasks, _ := f.snapshot()
	if fmt.Sprint(queries) != "[中文片名 English Alias]" || fmt.Sprint(tasks) != "[extract_media_identity]" {
		t.Fatalf("未完成搜索核验或意外调用判断/本地整理：queries=%v tasks=%v", queries, tasks)
	}
}
