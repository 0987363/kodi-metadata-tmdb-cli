package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfiguredLLMJudgeCLIAndFailureIsolation(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	for _, scenario := range []string{"success", "none", "error"} {
		t.Run(scenario, func(t *testing.T) {
			extractions, judgments := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/chat/completions" {
					var body struct {
						Messages []struct {
							Content string `json:"content"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) < 2 {
						t.Error("模型消息无效")
						w.WriteHeader(400)
						return
					}
					var prompt struct {
						Task       string                     `json:"task"`
						Candidates map[string]json.RawMessage `json:"candidates"`
					}
					if err := json.Unmarshal([]byte(body.Messages[1].Content), &prompt); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					content := ""
					switch prompt.Task {
					case "extract_media_identity":
						extractions++
						content = `{"title":"Target","year":2020}`
					case "select_metadata_candidate":
						judgments++
						if len(prompt.Candidates) != 2 {
							t.Error("判断器未收到完整候选")
						}
						if scenario == "error" {
							w.WriteHeader(500)
							return
						}
						content = `{"choice":"c1"}`
						if scenario == "none" {
							content = `{"choice":"none"}`
						}
					default:
						t.Errorf("未知模型阶段：%s", prompt.Task)
						w.WriteHeader(400)
						return
					}
					if r.Header.Get("Authorization") != "Bearer general-key" {
						t.Error("判断阶段未复用ai凭据")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
					return
				}
				switch r.URL.Path {
				case "/3/search/movie":
					_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"total_results":2,"results":[{"id":1,"title":"Wrong"},{"id":2,"title":"Target"}]}`))
				case "/3/movie/1":
					_, _ = w.Write([]byte(`{"id":1,"title":"Wrong"}`))
				case "/3/movie/2":
					_, _ = w.Write([]byte(`{"id":2,"title":"Target"}`))
				default:
					t.Errorf("不应访问Jev或未知接口：%s", r.URL)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			root := t.TempDir()
			media := filepath.Join(root, "movies")
			if err := os.Mkdir(media, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(media, "Target.2020.mkv"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			nfoPath := filepath.Join(media, "Target.2020.nfo")
			if err := os.WriteFile(nfoPath, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			settings := map[string]any{
				"scraper":   map[string]any{"providers": []string{"tmdb"}, "extract_llm": "general", "select_llm": "general", "cache_hours": 1},
				"llms":      []any{map[string]any{"name": "general", "type": "openai", "base_url": server.URL, "api_key": "general-key", "model": "general", "timeout_seconds": 2}},
				"tmdb":      map[string]any{"api_host": server.URL, "image_host": server.URL, "api_key": "source-key", "timeout_seconds": 2},
				"collector": map[string]any{"run_mode": 2, "movies_dir": []string{media}},
			}
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
			output, runErr := command.CombinedOutput()
			if extractions != 1 || judgments != 1 {
				t.Fatalf("不是完整LLM流程：提取%d 判断%d\n%s", extractions, judgments, output)
			}
			nfo, err := os.ReadFile(nfoPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "success" {
				if runErr != nil || !strings.Contains(string(nfo), ">2</uniqueid>") {
					t.Fatalf("未采用LLM选择：%v\n%s\n%s", runErr, output, nfo)
				}
			} else if runErr == nil || string(nfo) != "original" {
				t.Fatal(fmt.Sprintf("失败后发生了回退或覆盖：%v %s", runErr, nfo))
			}
		})
	}
}
