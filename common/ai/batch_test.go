package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
)

func batchClient(t *testing.T, respond func([]string) string) (*Client, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var envelope struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var prompt struct {
			Task   string       `json:"task"`
			Input  BatchInput   `json:"input"`
			Schema outputSchema `json:"schema"`
		}
		if err := json.Unmarshal([]byte(envelope.Messages[1].Content), &prompt); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		paths := make([]string, len(prompt.Input.Files))
		for i, file := range prompt.Input.Files {
			paths[i] = file.RelativePath
		}
		if len(paths) > 50 {
			t.Errorf("批次超过 50: %d", len(paths))
		}
		if prompt.Task == "" {
			t.Error("任务为空")
		}
		allowed := []string{"relative_path", "title", "plot", "genres"}
		if prompt.Task == "extract_media_identity" {
			allowed = []string{"relative_path", "media_type", "series_root", "content_role", "tmdb_id", "thetvdb_id", "title", "alias_title", "chs_title", "eng_title", "year", "season", "episode", "episode_title"}
		}
		if prompt.Schema.Items.Type == "" || len(prompt.Schema.Items.Fields) != len(allowed) {
			t.Errorf("输出字段协议不完整: %+v", prompt.Schema)
		}
		for _, name := range allowed {
			if prompt.Schema.Items.Fields[name] == "" {
				t.Errorf("输出协议缺少字段 %q", name)
			}
		}
		var raw struct {
			Input struct {
				Files []map[string]json.RawMessage `json:"files"`
			} `json:"input"`
		}
		if err := json.Unmarshal([]byte(envelope.Messages[1].Content), &raw); err != nil {
			t.Error(err)
		}
		for _, file := range raw.Input.Files {
			if len(file) != 1 || len(file["relative_path"]) == 0 {
				t.Errorf("输入预填了模型输出字段: %+v", file)
			}
		}
		content, _ := json.Marshal(respond(paths))
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	t.Cleanup(server.Close)
	client, err := New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return client, calls
}

func analysisClient(t *testing.T, content string) *Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(s.Close)
	client, err := New(config.LLMConfig{Type: "openai", BaseURL: s.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAnalyzeBatchesAndPreservesSpecials(t *testing.T) {
	client, calls := batchClient(t, func(paths []string) string {
		items := make([]map[string]any, len(paths))
		for i, p := range paths {
			items[i] = map[string]any{"relative_path": p, "media_type": "tv", "series_root": "Show", "title": "Show", "season": 0, "episode": i + 1, "thetvdb_id": "42"}
		}
		b, _ := json.Marshal(map[string]any{"items": items})
		return string(b)
	})
	files := make([]BatchFile, 51)
	for i := range files {
		files[i] = BatchFile{RelativePath: fmt.Sprintf("Show/S00E%02d.mkv", i+1)}
	}
	got, err := client.Analyze(context.Background(), BatchInput{Root: "/media", Files: files})
	if err != nil || len(got) != 51 || calls.Load() != 2 || got[0].Season == nil || *got[0].Season != 0 {
		t.Fatalf("批量识别错误: %d %d %v", len(got), calls.Load(), err)
	}
	if stats := client.Stats(); stats.AnalyzeRequests != 2 {
		t.Fatalf("计数=%+v", stats)
	}
}

func TestAnalyzeRejectsInvalidResponses(t *testing.T) {
	cases := map[string]string{
		"missing":      `{"items":[]}`,
		"duplicate":    `{"items":[{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Show","title":"Show"},{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Show","title":"Show"}]}`,
		"unknown path": `{"items":[{"relative_path":"Show/other.mkv","media_type":"tv","series_root":"Show","title":"Show"}]}`,
		"unknown type": `{"items":[{"relative_path":"Show/a.mkv","media_type":"clip","title":"Show"}]}`,
		"movie tvdb":   `{"items":[{"relative_path":"Show/a.mkv","media_type":"movie","title":"Movie","thetvdb_id":"1"}]}`,
		"movie root":   `{"items":[{"relative_path":"Show/a.mkv","media_type":"movie","title":"Movie","series_root":"Show"}]}`,
		"movie season": `{"items":[{"relative_path":"Show/a.mkv","media_type":"movie","title":"Movie","season":0}]}`,
		"invalid id":   `{"items":[{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Show","title":"Show","tmdb_id":"person/1"}]}`,
		"invalid root": `{"items":[{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Other","title":"Show"}]}`,
		"empty title":  `{"items":[{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Show"}]}`,
		"extra":        `{"items":[{"relative_path":"Show/a.mkv","media_type":"tv","series_root":"Show","title":"Show","actors":["A"]}]}`,
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := batchClient(t, func([]string) string { return response })
			if _, err := client.Analyze(context.Background(), BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: "Show/a.mkv"}}}); err == nil {
				t.Fatal("接受无效响应")
			}
		})
	}
}

func TestAnalyzeRejectsEscapingInputBeforeIO(t *testing.T) {
	client, calls := batchClient(t, func([]string) string { return "" })
	for _, p := range []string{"../a.mkv", "/a.mkv", "a/../b.mkv", ""} {
		if _, err := client.Analyze(context.Background(), BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: p}}}); err == nil {
			t.Errorf("接受路径 %q", p)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("非法输入发出请求")
	}
}

func TestAnalyzePreservesUnixFilenameCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 将反斜杠视为路径分隔符")
	}
	client, _ := batchClient(t, func(paths []string) string {
		b, _ := json.Marshal(map[string]any{"items": []map[string]any{{"relative_path": paths[0], "media_type": "movie", "title": "作品"}, {"relative_path": paths[1], "media_type": "movie", "title": "作品"}}})
		return string(b)
	})
	files := []BatchFile{{RelativePath: "a:b.mkv"}, {RelativePath: `a\b.mkv`}}
	got, err := client.Analyze(context.Background(), BatchInput{Root: "/media", Files: files})
	if err != nil || len(got) != 2 || got[0].RelativePath != files[0].RelativePath || got[1].RelativePath != files[1].RelativePath {
		t.Fatalf("合法文件名未原样覆盖: %+v %v", got, err)
	}
}

func TestCompletionRejectsRedirectWithoutForwardingInput(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, task := range []string{"analyze", "local"} {
			t.Run(fmt.Sprintf("%d_%s", status, task), func(t *testing.T) {
				var forwarded atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); w.WriteHeader(http.StatusOK) }))
				defer target.Close()
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, status) }))
				defer source.Close()
				client, err := New(config.LLMConfig{Type: "openai", BaseURL: source.URL, ApiKey: "private-test-key", Model: "test"})
				if err != nil {
					t.Fatal(err)
				}
				input := BatchInput{Root: "/private/library", Files: []BatchFile{{RelativePath: "秘密片名.mkv"}}}
				if task == "analyze" {
					_, err = client.Analyze(context.Background(), input)
				} else {
					_, err = client.DescribeLocal(context.Background(), input)
				}
				if err == nil || forwarded.Load() != 0 {
					t.Fatalf("重定向泄露或被接受: err=%v forwarded=%d", err, forwarded.Load())
				}
			})
		}
	}
}

func TestAnalyzeRejectsUnavailableClientAndCanceledContext(t *testing.T) {
	input := BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: "a.mkv"}}}
	var missing *Client
	if _, err := missing.Analyze(context.Background(), input); err == nil {
		t.Fatal("未初始化客户端被接受")
	}
	client, calls := batchClient(t, func([]string) string { return "" })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Analyze(ctx, input); err == nil {
		t.Fatal("已取消请求被接受")
	}
	if _, err := client.DescribeLocal(ctx, input); err == nil {
		t.Fatal("已取消本地描述被接受")
	}
	if calls.Load() != 0 || client.Stats().AnalyzeRequests != 0 || client.Stats().LocalRequests != 0 {
		t.Fatalf("未发出的请求被计数: %+v", client.Stats())
	}
}

func TestDescribeLocalRestrictsFactsAndCoversPaths(t *testing.T) {
	client, _ := batchClient(t, func(paths []string) string {
		b, _ := json.Marshal(map[string]any{"items": []map[string]any{{"relative_path": paths[0], "title": "文件标题", "plot": "", "genres": []string{}}}})
		return string(b)
	})
	got, err := client.DescribeLocal(context.Background(), BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: "a.mkv"}}})
	if err != nil || len(got) != 1 || got[0].Plot != "" || client.Stats().LocalRequests != 1 {
		t.Fatalf("本地描述=%+v %v", got, err)
	}
	for _, response := range []string{`{"items":[]}`, `{"items":[{"relative_path":"a.mkv","title":"X","actors":["A"]}]}`, `{"items":[{"relative_path":"a.mkv","title":" "}]}`, `{"items":[{"relative_path":"../a.mkv","title":"X"}]}`} {
		t.Run(response, func(t *testing.T) {
			c, _ := batchClient(t, func([]string) string { return response })
			if _, err := c.DescribeLocal(context.Background(), BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: "a.mkv"}}}); err == nil {
				t.Fatal("接受无效描述")
			}
		})
	}
}
