package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"net/http"
	"net/http/httptest"
	"testing"
)

func judgeOptions() []metadata.Option {
	return []metadata.Option{
		{Work: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "First"}},
		{Work: &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Title: "Second", ExternalIDs: []metadata.Identifier{{Type: "tmdb", Value: "99"}}}},
	}
}
func TestLLMJudgeSelectsFromActualCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var prompt struct {
			Task       string `json:"task"`
			Candidates map[string]struct {
				Work struct {
					Source, ID  string
					ExternalIDs []metadata.Identifier `json:"external_ids"`
				} `json:"work"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &prompt); err != nil {
			t.Error(err)
		}
		if prompt.Task != "select_metadata_candidate" || prompt.Candidates["c0"].Work.Source != "tmdb" || prompt.Candidates["c1"].Work.Source != "thetvdb" || len(prompt.Candidates["c1"].Work.ExternalIDs) != 1 {
			t.Errorf("判断证据不完整：%+v", prompt)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"choice":"c1"}`}}}})
	}))
	defer server.Close()
	client, err := New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "general", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewJudge(client).Select(context.Background(), metadata.Request{Kind: metadata.Show}, judgeOptions())
	if err != nil || got != 1 {
		t.Fatalf("相同数值不同来源的候选混淆：%d %v", got, err)
	}
}
func TestLLMJudgeRejectsNoneAndInvalidChoices(t *testing.T) {
	for _, value := range []string{`{"choice":"none"}`, `{"choice":"42"}`, `{"choice":"c9"}`, `{"choice":null}`, `{}`, "not json"} {
		t.Run(value, func(t *testing.T) {
			client := analysisClient(t, value)
			got, err := NewJudge(client).Select(context.Background(), metadata.Request{Kind: metadata.Show}, judgeOptions())
			if err == nil || got != -1 {
				t.Fatalf("非法判断被接受：%d %v", got, err)
			}
			if value == `{"choice":"none"}` && !errors.Is(err, metadata.ErrNotFound) {
				t.Fatal(err)
			}
		})
	}
}
func TestLLMJudgeCancellationAndEmptyCandidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewJudge(nil).Select(ctx, metadata.Request{}, judgeOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewJudge(nil).Select(context.Background(), metadata.Request{}, nil); err == nil {
		t.Fatal("空候选不应调用模型")
	}
	if _, err := NewJudge(nil).Select(context.Background(), metadata.Request{}, []metadata.Option{{}}); err == nil {
		t.Fatal("缺少事实的候选不应调用模型")
	}
}
