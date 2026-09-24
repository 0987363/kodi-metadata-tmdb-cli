package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func judgeCandidates() []metadata.Candidate {
	return []metadata.Candidate{
		{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "First", OriginalTitle: "Original First", Year: 2020},
		{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Title: "Second", OriginalTitle: "Original Second", Year: 2021},
	}
}
func TestLLMJudgeSelectsFromBasicWorkCandidates(t *testing.T) {
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
			Task       string                        `json:"task"`
			Input      metadata.JudgmentInput        `json:"input"`
			Candidates map[string]metadata.Candidate `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &prompt); err != nil {
			t.Error(err)
		}
		if prompt.Task != "select_metadata_candidate" || prompt.Input.Query.Title != "作品" || prompt.Input.Ref.Provider != "thetvdb" || prompt.Candidates["c0"].Ref.Provider != "tmdb" || prompt.Candidates["c1"].Ref.Provider != "thetvdb" || prompt.Candidates["c1"].Ref.ID != "42" {
			t.Errorf("判断证据没有保留作品身份与来源：%+v", prompt)
		}
		for _, forbidden := range []string{`"files"`, `"path"`, `"filename"`, `"episodes"`, `"season"`, `"episode"`, `"group"`, `"hints"`, `"work"`, `"plot"`, `"external_ids"`, `"actors"`, `"artwork"`} {
			if strings.Contains(body.Messages[1].Content, forbidden) {
				t.Errorf("作品判断混入非基本身份字段 %s", forbidden)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"choice":"c1"}`}}}})
	}))
	defer server.Close()
	client, err := New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "general", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := metadata.Request{Kind: metadata.Show, Path: "/library/作品", Filename: "Special.mkv", Files: []string{"Special-v1.mkv", "Special-v2.mkv"}, Episodes: []metadata.EpisodeKey{{Season: 0, Episode: 2}}, Season: 1, Episode: 2, Group: "alternate", Hints: []metadata.Ref{{Provider: "tmdb", Kind: metadata.Show, ID: "999"}}, Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show}, Query: metadata.Query{Kind: metadata.Show, Title: "作品", OriginalTitle: "Original", Year: 2020}}
	got, err := NewJudge(client).Select(context.Background(), request, judgeCandidates())
	if err != nil || got != 1 {
		t.Fatalf("相同数值不同来源的候选混淆：%d %v", got, err)
	}
	if client.Stats().DecisionRequests != 1 {
		t.Fatalf("判断请求计数=%+v", client.Stats())
	}
}
func TestLLMJudgeRejectsNoneAndInvalidChoices(t *testing.T) {
	for _, value := range []string{`{"choice":"none"}`, `{"choice":"42"}`, `{"choice":"c9"}`, `{"choice":null}`, `{}`, "not json"} {
		t.Run(value, func(t *testing.T) {
			client := analysisClient(t, value)
			got, err := NewJudge(client).Select(context.Background(), metadata.Request{Kind: metadata.Show}, judgeCandidates())
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
	if _, err := NewJudge(nil).Select(ctx, metadata.Request{}, judgeCandidates()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewJudge(nil).Select(context.Background(), metadata.Request{}, nil); err == nil {
		t.Fatal("空候选不应调用模型")
	}
	if _, err := NewJudge(nil).Select(context.Background(), metadata.Request{}, []metadata.Candidate{{}}); err == nil {
		t.Fatal("缺少事实的候选不应调用模型")
	}
}

func TestLLMJudgeRejectsInvalidBasicCandidatesBeforeIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"choice":"c0"}`}}}})
	}))
	defer server.Close()
	client, err := New(config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "test", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		candidate metadata.Candidate
	}{
		{"empty title", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}}},
		{"blank title", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: " "}},
		{"empty source", metadata.Candidate{Ref: metadata.Ref{Kind: metadata.Show, ID: "42"}, Title: "Show"}},
		{"blank source", metadata.Candidate{Ref: metadata.Ref{Provider: " ", Kind: metadata.Show, ID: "42"}, Title: "Show"}},
		{"missing identifier", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show}, Title: "Show"}},
		{"slug only", metadata.Candidate{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "show"}, Title: "Show"}},
		{"blank identifier", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: " "}, Title: "Show"}},
		{"episode identity", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: "42"}, Title: "Episode"}},
		{"unknown kind", metadata.Candidate{Ref: metadata.Ref{Provider: "tmdb", ID: "42"}, Title: "Show"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, err := NewJudge(client).Select(context.Background(), metadata.Request{Kind: metadata.Show}, []metadata.Candidate{test.candidate})
			if err == nil || index != -1 {
				t.Fatalf("非法基本候选被接受: %d %v", index, err)
			}
		})
	}
	if calls.Load() != 0 || client.Stats().DecisionRequests != 0 {
		t.Fatalf("非法候选触发请求: %d %+v", calls.Load(), client.Stats())
	}
}
