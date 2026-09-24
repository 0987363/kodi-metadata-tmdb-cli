package ai

import (
	"context"
	"encoding/json"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
func TestAnalysisPreservesSourceHintsAndSpecials(t *testing.T) {
	client := analysisClient(t, `{"title":"节目","tmdb_id":"11","thetvdb_id":"22","season":0,"episode":2,"episode_title":"特别篇"}`)
	got, err := client.ParseMediaContext(context.Background(), &ParseInput{MediaType: "tv", Filename: "Show.S00E02.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Season == nil || *got.Season != 0 || got.Episode == nil || *got.Episode != 2 || got.TheTVDBID != "22" || got.EpisodeTitle != "特别篇" {
		t.Fatalf("提取线索丢失：%+v", got)
	}
}
func TestAnalysisUnknownCoordinatesStayUnknown(t *testing.T) {
	client := analysisClient(t, `{"title":"节目","season":null,"episode":null}`)
	got, err := client.ParseMediaContext(context.Background(), &ParseInput{MediaType: "tv", Filename: "节目.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Season != nil || got.Episode != nil {
		t.Fatal("未知季集号被自动推测")
	}
}
func TestAnalysisRejectsInvalidOrNonJSONResults(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `"text"`, `1`, `true`, "```json\n{}\n```", `{"title":"节目","season":-1}`, `{"title":"节目","episode":0}`, `{"tmdb_id":"tmdb:11"}`} {
		t.Run(body, func(t *testing.T) {
			client := analysisClient(t, body)
			if _, err := client.ParseMediaContext(context.Background(), &ParseInput{MediaType: "tv", Filename: "Show.S01E01.mkv"}); err == nil {
				t.Fatal("接受了无效提取结果")
			}
		})
	}
}
func TestAnalysisRequiresConfiguredLLM(t *testing.T) {
	if client, err := New(config.LLMConfig{}); err == nil || client != nil {
		t.Fatal("没有 LLM 配置却创建了客户端")
	}
}
