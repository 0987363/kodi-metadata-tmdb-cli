package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestNamedClientsKeepEndpointCredentialsModelAndTemperatureIsolated(t *testing.T) {
	var extractCalls, judgeCalls atomic.Int32
	extractServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractCalls.Add(1)
		assertCompletionRequest(t, r, "Bearer extract-key", "extract-model", 0.2, "extract_media_identity")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[{"relative_path":"Movie.mkv","media_type":"movie","title":"Extracted Movie"}]}`}}}})
	}))
	defer extractServer.Close()
	judgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		judgeCalls.Add(1)
		assertCompletionRequest(t, r, "Bearer judge-key", "judge-model", 0.7, "select_metadata_candidate")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"choice":"c0"}`}}}})
	}))
	defer judgeServer.Close()
	extractTemperature, judgeTemperature := 0.2, 0.7
	extractConfig := config.LLMConfig{Name: "extract", Type: "openai", BaseURL: extractServer.URL + "/v1", ApiKey: "extract-key", Model: "extract-model", Temperature: &extractTemperature}
	judgeConfig := config.LLMConfig{Name: "judge", Type: "openai", BaseURL: judgeServer.URL + "/v1", ApiKey: "judge-key", Model: "judge-model", Temperature: &judgeTemperature}
	extractor, err := New(extractConfig)
	if err != nil {
		t.Fatal(err)
	}
	judgeClient, err := New(judgeConfig)
	if err != nil {
		t.Fatal(err)
	}
	extractConfig.BaseURL, extractConfig.ApiKey, extractConfig.Model = judgeServer.URL, "mutated", "mutated"
	judgeConfig.BaseURL, judgeConfig.ApiKey, judgeConfig.Model = extractServer.URL, "mutated", "mutated"
	extractTemperature, judgeTemperature = 1, 1
	var group sync.WaitGroup
	for range 3 {
		group.Add(2)
		go func() {
			defer group.Done()
			got, err := extractor.Analyze(context.Background(), BatchInput{Root: "/media", Files: []BatchFile{{RelativePath: "Movie.mkv"}}})
			if err != nil || len(got) != 1 || got[0].Title != "Extracted Movie" {
				t.Errorf("提取实例调用失败: %+v %v", got, err)
			}
		}()
		go func() {
			defer group.Done()
			got, err := NewJudge(judgeClient).Select(context.Background(), metadata.Request{Kind: metadata.Movie}, judgeOptions())
			if err != nil || got != 0 {
				t.Errorf("判断实例调用失败: %d %v", got, err)
			}
		}()
	}
	group.Wait()
	if extractCalls.Load() != 3 || judgeCalls.Load() != 3 {
		t.Fatalf("实例请求串用: %d %d", extractCalls.Load(), judgeCalls.Load())
	}
}

func assertCompletionRequest(t *testing.T, r *http.Request, authorization, model string, temperature float64, task string) {
	t.Helper()
	var body struct {
		Model       string  `json:"model"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		return
	}
	if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != authorization || body.Model != model || body.Temperature != temperature {
		t.Errorf("实例请求参数发生串用: %s %s %+v", r.URL.Path, r.Header.Get("Authorization"), body)
	}
	if len(body.Messages) != 2 {
		t.Errorf("消息数量错误: %d", len(body.Messages))
		return
	}
	var prompt struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(body.Messages[1].Content), &prompt); err != nil || prompt.Task != task {
		t.Errorf("实例调用任务错误: %s %v", prompt.Task, err)
	}
}

func TestNewRejectsInvalidActiveConnection(t *testing.T) {
	for _, c := range []config.LLMConfig{
		{},
		{Type: "jev", BaseURL: "https://example.invalid/v1", ApiKey: "key", Model: "model"},
		{Type: "openai", BaseURL: "https://example.invalid/v1", Model: "model"},
		{Type: "openai", BaseURL: "https://example.invalid/v1", ApiKey: "key"},
		{Type: "openai", BaseURL: "invalid", ApiKey: "key", Model: "model"},
		{Type: "openai", BaseURL: "https://user:secret@example.invalid/v1", ApiKey: "key", Model: "model"},
	} {
		if client, err := New(c); err == nil || client != nil {
			t.Fatalf("错误连接参数被接受: %v", err)
		}
	}
}

func TestNewRejectsInvalidProxyBeforeRequest(t *testing.T) {
	for _, proxyURL := range []string{"://invalid", "ftp://user:private-key@proxy.invalid:8080", "http:///", "socks5://", "http://proxy.invalid:99999"} {
		client, err := New(config.LLMConfig{Type: "openai", BaseURL: "https://example.invalid/v1", ApiKey: "key", Model: "model", Proxy: proxyURL})
		if err == nil || client != nil {
			t.Fatalf("模型构造接受了非法代理: %v", err)
		}
	}
}
