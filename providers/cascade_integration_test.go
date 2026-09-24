package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/config"
)

func (f *pipelineFixture) thetvdbSource() func(http.ResponseWriter, *http.Request) bool {
	f.t.Helper()
	pages := make(map[string][]byte)
	for path, file := range map[string]string{
		"/series/26882341-show":                     "series.html",
		"/series/26882341-show/allseasons/official": "allseasons.html",
		"/series/26882341-show/episodes/7407799":    "episode.html",
	} {
		data, err := os.ReadFile(filepath.Join("..", "thetvdb", "testdata", file))
		if err != nil {
			f.t.Fatal(err)
		}
		pages[path] = data
	}
	return func(w http.ResponseWriter, r *http.Request) bool {
		if data, ok := pages[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return true
		}
		switch r.URL.Path {
		case "/search":
			fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL = '%s/web/search/queries';</script>`, f.server.URL)
		case "/web/search/queries":
			_, _ = w.Write([]byte(`{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":100,"exhaustiveNbHits":true,"hits":[{"id":371065,"name":"坑王驾到","slug":"26882341-show","type":"series","year":"2016"}]}]}`))
		case "/dereferrer/series/371065":
			http.Redirect(w, r, "/series/26882341-show", 302)
		case "/series/26882341-show/seasons/official/1":
			_, _ = w.Write([]byte(`<div id="episodes"><table><tbody><tr><td>S01E02</td><td><a href="/series/26882341-show/episodes/7407799">第二回</a></td><td></td><td>60</td><td></td></tr></tbody></table></div>`))
		default:
			return false
		}
		return true
	}
}

func TestPipelineSourceFailuresAdvanceToSecondWebsite(t *testing.T) {
	for _, stage := range []string{"rejected", "low_score", "search_http", "search_timeout", "search_protocol", "judge_http", "judge_protocol", "missing_episode"} {
		t.Run(stage, func(t *testing.T) {
			f := fixturePipeline(t)
			f.providers = []string{"tmdb", "thetvdb"}
			f.file("Show/Show.S01E02.mkv")
			f.analysis = `{"items":[{"relative_path":"Show/Show.S01E02.mkv","media_type":"tv","series_root":"Show","title":"坑王驾到","season":1,"episode":2}]}`
			f.source = tmdbShow
			f.choices = []string{"none", "c0"}
			if stage != "rejected" {
				f.choices = nil
			}
			if stage == "low_score" {
				f.scores = []float64{0.2, 0.99}
			}
			if stage == "judge_http" || stage == "judge_protocol" {
				f.judgeFailures = []string{stage}
			}
			tvdb := f.thetvdbSource()
			f.rawSource = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == "/3/search/tv" {
					switch stage {
					case "search_http":
						w.WriteHeader(503)
						return true
					case "search_timeout":
						<-r.Context().Done()
						return true
					case "search_protocol":
						_, _ = w.Write([]byte(`{"results":`))
						return true
					}
				}
				return tvdb(w, r)
			}
			if stage == "missing_episode" {
				f.source = func(r *http.Request) (any, bool) {
					body, ok := tmdbShow(r)
					if r.URL.Path == "/3/tv/42" {
						body.(map[string]any)["season/1"] = map[string]any{"id": 10, "season_number": 1, "episodes": []any{}}
					}
					return body, ok
				}
			}
			if err := f.run(); err != nil {
				t.Fatalf("首源未成功后没有接续: %v", err)
			}
			got := readNFO(t, filepath.Join(f.root, "Show/Show.S01E02.nfo"))
			if !strings.Contains(got, `type="tvdb"`) || !strings.Contains(got, ">7407799</uniqueid>") || strings.Contains(got, `type="tmdb"`) {
				t.Fatalf("来源事实没有整体切换: %s", got)
			}
			tasks, requests := f.snapshot()
			if strings.Contains(strings.Join(tasks, ","), "describe_local_metadata") {
				t.Fatalf("第二网站成功后仍本地整理: %v", tasks)
			}
			if stage == "rejected" || stage == "low_score" || stage == "judge_http" || stage == "judge_protocol" {
				for _, path := range requests {
					if strings.HasPrefix(path, "/3/tv/42") {
						t.Fatalf("未确认作品仍取详情: %v", requests)
					}
				}
			}
		})
	}
}

func (f *pipelineFixture) runCLI() ([]byte, error) {
	f.t.Helper()
	f.configure()
	settings := config.Config{LLMs: config.LLMs, Scraper: config.Scraper, Tmdb: config.Tmdb, TheTVDB: config.TheTVDB, Collector: config.Collector, Log: &config.LogConfig{Mode: config.LogModeStdout, Level: config.LogLevelFatal}}
	data, err := json.Marshal(settings)
	if err != nil {
		f.t.Fatal(err)
	}
	file := filepath.Join(f.t.TempDir(), "config.json")
	if err := os.WriteFile(file, data, 0644); err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", ".", "--config", file, "--path", f.root)
	command.Dir = ".."
	return command.CombinedOutput()
}

func TestCLIWebsiteExhaustionReachesLocalAndPreservesFailureReasons(t *testing.T) {
	for _, scenario := range []string{"web_errors_local_success", "web_empty_local_success", "all_failed", "manual_source"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixturePipeline(t)
			f.providers = []string{"thetvdb", "tmdb"}
			f.file("Show/Show.S01E01.mkv")
			f.analysis = `{"items":[{"relative_path":"Show/Show.S01E01.mkv","media_type":"tv","series_root":"Show","title":"Show","season":1,"episode":1}]}`
			f.local = `{"items":[{"relative_path":"Show/Show.S01E01.mkv","title":"本地首集","plot":"","genres":[]}]}`
			if scenario == "all_failed" {
				f.local = `invalid-local-json`
			}
			if scenario == "manual_source" {
				if err := os.MkdirAll(filepath.Join(f.root, "Show", ".metadata"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.root, "Show", ".metadata", "source.json"), []byte(`{"provider":"thetvdb"}`), 0644); err != nil {
					t.Fatal(err)
				}
			}
			f.rawSource = func(w http.ResponseWriter, r *http.Request) bool {
				if scenario != "web_empty_local_success" {
					if strings.HasPrefix(r.URL.Path, "/3/") {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							_ = conn.Close()
						}
					} else {
						w.WriteHeader(503)
					}
					return true
				}
				switch r.URL.Path {
				case "/search":
					fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL = '%s/web/search/queries';</script>`, f.server.URL)
				case "/web/search/queries":
					_, _ = w.Write([]byte(`{"results":[{"page":0,"nbPages":0,"nbHits":0,"hitsPerPage":100,"exhaustiveNbHits":true,"hits":[]}]}`))
				case "/3/search/tv":
					_, _ = w.Write([]byte(`{"page":1,"total_pages":0,"total_results":0,"results":[]}`))
				default:
					return false
				}
				return true
			}
			output, err := f.runCLI()
			tasks, requests := f.snapshot()
			if scenario == "manual_source" {
				if err == nil || fmt.Sprint(tasks) != "[extract_media_identity]" {
					t.Fatalf("人工约束未终止: %v %s tasks=%v", err, output, tasks)
				}
				for _, path := range requests {
					if strings.HasPrefix(path, "/3/") {
						t.Fatalf("人工约束后切换网站: %v", requests)
					}
				}
				return
			}
			if fmt.Sprint(tasks) != "[extract_media_identity describe_local_metadata]" {
				t.Fatalf("网站耗尽没有进入一次本地整理: err=%v output=%s tasks=%v", err, output, tasks)
			}
			for _, secret := range []string{"extract-key", "judge-key", "source-key"} {
				if strings.Contains(string(output), secret) {
					t.Fatalf("日志泄露密钥: %s", output)
				}
			}
			if scenario == "all_failed" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() == 0 {
					t.Fatalf("三方式失败未非零退出: %v %s", err, output)
				}
				for _, marker := range []string{"thetvdb", "tmdb", "503", "EOF", "本地整理", "invalid character"} {
					if !strings.Contains(string(output), marker) {
						t.Fatalf("Fatal 级别日志缺少 %s: %s", marker, output)
					}
				}
				if strings.Count(string(output), "来源=thetvdb") != 1 || strings.Count(string(output), "来源=tmdb") != 1 {
					t.Fatalf("逐源原因重复: %s", output)
				}
				for _, path := range []string{"Show/tvshow.nfo", "Show/Show.S01E01.nfo"} {
					if _, err := os.Stat(filepath.Join(f.root, path)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("失败仍新建 NFO: %s %v", path, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("本地整理成功仍返回异常: %v %s", err, output)
			}
			got := readNFO(t, filepath.Join(f.root, "Show/Show.S01E01.nfo"))
			if !strings.Contains(got, `type="local"`) || !strings.Contains(got, "本地首集") || strings.Contains(got, `type="tmdb"`) || strings.Contains(got, `type="tvdb"`) {
				t.Fatalf("本地元数据错误: %s", got)
			}
		})
	}
}

func TestPipelineUnsupportedWebsiteMediaTypeReachesLocal(t *testing.T) {
	f := fixturePipeline(t)
	f.providers = []string{"thetvdb"}
	f.file("Movie.mkv")
	f.analysis = `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Movie"}]}`
	f.local = `{"items":[{"relative_path":"Movie.mkv","title":"本地电影","plot":"","genres":[]}]}`
	if err := f.run(); err != nil {
		t.Fatalf("不适用网站来源阻断本地整理: %v", err)
	}
	got := readNFO(t, filepath.Join(f.root, "Movie.nfo"))
	if !strings.Contains(got, `type="local"`) || !strings.Contains(got, "本地电影") {
		t.Fatalf("本地电影未写入: %s", got)
	}
	tasks, requests := f.snapshot()
	if fmt.Sprint(tasks) != "[extract_media_identity describe_local_metadata]" || len(requests) != 2 {
		t.Fatalf("不适用来源仍被访问: tasks=%v requests=%v", tasks, requests)
	}
}
