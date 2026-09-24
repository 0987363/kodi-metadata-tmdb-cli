package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

const acceptedResponse = `{"model":"jev-1.13.0","answers":{"select":{"type":"choice","choice":"c1","probabilities":{"c0":0.4,"c1":0.6,"none":0},"confidence":0.1},"match_0":{"type":"noul","noul":0.95},"match_1":{"type":"noul","noul":0.98}},"usage":{"input_tokens":500,"output_tokens":70}}`

func workCandidates() []metadata.Candidate {
	return []metadata.Candidate{
		{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "同名节目", OriginalTitle: "Original Show", Year: 2020},
		{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Title: "同名节目", OriginalTitle: "Original Show", Year: 2020},
	}
}

func sourceRequest() metadata.Request {
	return metadata.Request{Path: "/library/同名节目", Files: []string{"Season 0/Original.Show.S00E02.mkv", "Season 1/Original.Show.S01E01.mkv"}, Kind: metadata.Show, Query: metadata.Query{Kind: metadata.Show, Title: "同名节目", OriginalTitle: "Original Show", Year: 2020}, Episodes: []metadata.EpisodeKey{{Season: 0, Episode: 2}, {Season: 1, Episode: 1}}, Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Hints: []metadata.Ref{{Provider: "tmdb", Kind: metadata.Show, ID: "42"}}}
}

func TestSelectUsesChoiceAndNoulForBasicWorkIdentity(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Errorf("错误的请求目标: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("未按协议提供 Bearer 或 JSON 请求头")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var request struct {
			Model     string                 `json:"model"`
			State     metadata.JudgmentInput `json:"state"`
			Questions map[string]struct {
				Type         string                     `json:"type"`
				Instructions json.RawMessage            `json:"instructions"`
				Criteria     map[string]json.RawMessage `json:"criteria"`
				Options      json.RawMessage            `json:"options"`
			} `json:"questions"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		if request.Model != "jev-latest" || request.State.Kind != metadata.Show || request.State.Query.Title != "同名节目" || request.State.Query.Year != 2020 || request.State.Ref.Provider != "thetvdb" {
			t.Errorf("模型或作品身份错误: %#v", request)
		}
		if len(request.Questions) != 3 {
			t.Errorf("问题数量=%d", len(request.Questions))
		}
		selectQuestion := request.Questions["select"]
		if selectQuestion.Type != "choice" || len(selectQuestion.Criteria) != 3 || len(selectQuestion.Criteria["none"]) == 0 || len(selectQuestion.Options) != 0 {
			t.Errorf("Choice 必须采用完整的 criteria: %#v", selectQuestion)
		}
		for i, source := range []string{"tmdb", "thetvdb"} {
			var candidate metadata.Candidate
			if err := json.Unmarshal(selectQuestion.Criteria[fmt.Sprintf("c%d", i)], &candidate); err != nil {
				t.Error(err)
			}
			if candidate.Ref.Provider != source || candidate.Ref.Kind != metadata.Show || candidate.Ref.ID != "42" || candidate.Title != "同名节目" || candidate.OriginalTitle != "Original Show" || candidate.Year != 2020 {
				t.Errorf("候选来源或基本身份丢失: %#v", candidate)
			}
			match := request.Questions[fmt.Sprintf("match_%d", i)]
			var instruction struct {
				Question  string             `json:"question"`
				Candidate metadata.Candidate `json:"candidate"`
			}
			if err := json.Unmarshal(match.Instructions, &instruction); err != nil {
				t.Error(err)
			}
			if match.Type != "noul" || instruction.Candidate.Ref.Provider != source || instruction.Candidate.Title != "同名节目" || !strings.Contains(instruction.Question, "同一作品") {
				t.Errorf("Noul 没有提出明确作品身份问题: %s", match.Instructions)
			}
			if strings.Contains(instruction.Question, "逐集") || strings.Contains(instruction.Question, "计数") || strings.Contains(instruction.Question, "覆盖") {
				t.Errorf("Noul 混入季集完整性: %s", instruction.Question)
			}
		}
		for _, forbidden := range []string{"test-secret", `"files"`, `"path"`, `"filename"`, `"episodes"`, `"season"`, `"episode"`, `"group"`, `"hints"`, `"work"`, `"plot"`, `"premiered"`, `"external_ids"`, `"actors"`, `"artwork"`} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("请求混入非基本作品身份 %q", forbidden)
			}
		}
		io.WriteString(w, acceptedResponse)
	}))
	defer server.Close()
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "test-secret"}, 0.9).Select(context.Background(), sourceRequest(), workCandidates())
	if err != nil || index != 1 || calls.Load() != 1 {
		t.Fatalf("返回序位=%d, 错误=%v, 请求次数=%d", index, err, calls.Load())
	}
}
func TestSelectAcceptsLowChoiceConfidenceAndThresholdBoundary(t *testing.T) {
	for _, body := range []string{
		strings.ReplaceAll(acceptedResponse, `"confidence":0.1`, `"confidence":0`),
		strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":0.9`),
		strings.ReplaceAll(acceptedResponse, `"c0":0.4,"c1":0.6`, `"c0":0.5,"c1":0.5`),
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }))
			defer server.Close()
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0.9).Select(context.Background(), sourceRequest(), workCandidates())
			if err != nil || index != 1 {
				t.Fatalf("序位=%d, 错误=%v", index, err)
			}
		})
	}
}

func TestSelectRejectsInvalidAndNonmatchingAnswers(t *testing.T) {
	cases := map[string]string{
		"none":                 strings.ReplaceAll(strings.ReplaceAll(acceptedResponse, `"choice":"c1"`, `"choice":"none"`), `"c0":0.4,"c1":0.6,"none":0`, `"c0":0.1,"c1":0.1,"none":0.8`),
		"unknown choice":       strings.ReplaceAll(acceptedResponse, `"choice":"c1"`, `"choice":"42"`),
		"low match":            strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":0.7`),
		"malformed json":       `{"model":`,
		"multiple json values": acceptedResponse + `{}`,
		"no model":             strings.ReplaceAll(acceptedResponse, `"model":"jev-1.13.0",`, ""),
		"no answers":           `{"model":"jev-1.13.0"}`,
		"no select":            strings.ReplaceAll(acceptedResponse, `"select":`, `"wrong":`),
		"wrong choice type":    strings.ReplaceAll(acceptedResponse, `"type":"choice"`, `"type":"noul"`),
		"missing choice":       strings.ReplaceAll(acceptedResponse, `"choice":"c1",`, ""),
		"no probabilities":     strings.ReplaceAll(acceptedResponse, `"probabilities":{"c0":0.4,"c1":0.6,"none":0},`, ""),
		"missing probability":  strings.ReplaceAll(acceptedResponse, `,"none":0`, ""),
		"null probability":     strings.ReplaceAll(acceptedResponse, `"none":0`, `"none":null`),
		"extra probability":    strings.ReplaceAll(acceptedResponse, `"none":0`, `"none":0,"unknown":0`),
		"probability sum":      strings.ReplaceAll(acceptedResponse, `"c0":0.4`, `"c0":0.3`),
		"negative probability": strings.ReplaceAll(acceptedResponse, `"c0":0.4,"c1":0.6`, `"c0":-0.1,"c1":1.1`),
		"choice not highest":   strings.ReplaceAll(acceptedResponse, `"c0":0.4,"c1":0.6`, `"c0":0.7,"c1":0.3`),
		"no confidence":        strings.ReplaceAll(acceptedResponse, `,"confidence":0.1`, ""),
		"null confidence":      strings.ReplaceAll(acceptedResponse, `"confidence":0.1`, `"confidence":null`),
		"confidence above one": strings.ReplaceAll(acceptedResponse, `"confidence":0.1`, `"confidence":1.1`),
		"no selected noul":     strings.ReplaceAll(acceptedResponse, `,"match_1":{"type":"noul","noul":0.98}`, ""),
		"no unselected noul":   strings.ReplaceAll(acceptedResponse, `,"match_0":{"type":"noul","noul":0.95}`, ""),
		"wrong noul type":      strings.ReplaceAll(acceptedResponse, `"type":"noul"`, `"type":"score"`),
		"missing noul value":   strings.ReplaceAll(acceptedResponse, `,"noul":0.98`, ""),
		"null noul":            strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":null`),
		"noul wrong json type": strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":"0.98"`),
		"noul above one":       strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":1.01`),
		"noul below zero":      strings.ReplaceAll(acceptedResponse, `"noul":0.98`, `"noul":-0.1`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }))
			defer server.Close()
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0.9).Select(context.Background(), sourceRequest(), workCandidates())
			if err == nil || index != -1 {
				t.Fatalf("非法响应被接受，序位=%d, 错误=%v", index, err)
			}
		})
	}
}

func TestSelectRejectsHTTPFailuresWithoutRetryOrSecretLeak(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"Bearer secret-api-key"}`)
			}))
			defer server.Close()
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "secret-api-key"}, 0).Select(context.Background(), sourceRequest(), workCandidates())
			if err == nil || index != -1 || calls.Load() != 1 || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "secret-api-key") {
				t.Fatalf("序位=%d, 错误=%v, 调用=%d", index, err, calls.Load())
			}
		})
	}
}

func TestSelectEnforcesResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, strings.Repeat(" ", 2*1024*1024)+acceptedResponse)
	}))
	defer server.Close()
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), sourceRequest(), workCandidates())
	if err == nil || index != -1 {
		t.Fatalf("超长响应被接受: %d, %v", index, err)
	}
}

func TestSelectNormalizesEndpointsAndUsesConfiguredModel(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/", "/v1/systemone", "/v1/systemone/"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/systemone" {
					t.Errorf("路径=%s", r.URL.Path)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if string(body["model"]) != `"jev-1.13.0"` {
					t.Errorf("未采用指定模型: %s", body["model"])
				}
				io.WriteString(w, acceptedResponse)
			}))
			defer server.Close()
			_, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL + suffix, ApiKey: "key", Model: "jev-1.13.0"}, 0).Select(context.Background(), sourceRequest(), workCandidates())
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectValidatesConfigurationAndCandidatesBeforeIO(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); io.WriteString(w, acceptedResponse) }))
	defer server.Close()
	valid := config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}
	temperature := 0.2
	for _, test := range []struct {
		name      string
		config    config.LLMConfig
		threshold float64
		options   []metadata.Candidate
	}{
		{"empty config", config.LLMConfig{}, 0, workCandidates()},
		{"wrong protocol", config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "key"}, 0, workCandidates()},
		{"empty key", config.LLMConfig{Type: "jev", BaseURL: server.URL}, 0, workCandidates()},
		{"temperature", config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", Temperature: &temperature}, 0, workCandidates()},
		{"empty candidates", valid, 0, nil},
		{"empty work title", valid, 0, []metadata.Candidate{{}}},
		{"too many candidates", valid, 0, make([]metadata.Candidate, 255)},
		{"negative threshold", valid, -0.1, workCandidates()},
		{"threshold above one", valid, 1.1, workCandidates()},
		{"nan threshold", valid, math.NaN(), workCandidates()},
		{"invalid endpoint", config.LLMConfig{Type: "jev", BaseURL: "not-a-url", ApiKey: "key"}, 0, workCandidates()},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := New(test.config, test.threshold)
			if err != nil {
				if client != nil {
					t.Fatal("失败的构造不应返回客户端")
				}
				return
			}
			index, err := client.Select(context.Background(), sourceRequest(), test.options)
			if err == nil || index != -1 {
				t.Fatalf("无效输入被接受: %d, %v", index, err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("无效输入发出 %d 个请求", calls.Load())
	}
}

func TestSelectHonorsCancellationAndClientTimeout(t *testing.T) {
	for _, test := range []struct {
		name           string
		canceled       bool
		timeoutSeconds int
	}{
		{"context canceled", true, 30},
		{"client timeout", false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.canceled {
				go func() { <-started; cancel() }()
			}
			start := time.Now()
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", TimeoutSeconds: test.timeoutSeconds}, 0).Select(ctx, sourceRequest(), workCandidates())
			want := context.DeadlineExceeded
			if test.canceled {
				want = context.Canceled
			}
			if index != -1 || !errors.Is(err, want) || time.Since(start) > 4*time.Second {
				t.Fatalf("取消或超时未生效: %d, %v", index, err)
			}
		})
	}
}

func TestSelectTimeoutAfterResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", TimeoutSeconds: 1}, 0).Select(context.Background(), sourceRequest(), workCandidates())
	if index != -1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("读取响应超时未保留取消语义: %d, %v", index, err)
	}
}

func TestSelectAccepts254CandidatesAndRejects255WithoutTruncation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Questions map[string]struct{ Criteria map[string]json.RawMessage }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Questions) != 255 || len(request.Questions["select"].Criteria) != 255 {
			t.Error("未提交所有 254 个真实候选和 none")
		}
		answers := make(map[string]any)
		probabilities := map[string]float64{"none": 0}
		for i := 0; i < 254; i++ {
			probabilities[fmt.Sprintf("c%d", i)] = 0
			answers[fmt.Sprintf("match_%d", i)] = map[string]any{"type": "noul", "noul": 0.99}
		}
		probabilities["c253"] = 1
		answers["select"] = map[string]any{"type": "choice", "choice": "c253", "probabilities": probabilities, "confidence": 1}
		json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 10000, "output_tokens": 1500}})
	}))
	defer server.Close()
	options := make([]metadata.Candidate, 255)
	for i := range options {
		options[i] = workCandidates()[0]
	}
	client := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0)
	index, err := client.Select(context.Background(), sourceRequest(), options[:254])
	if index != 253 || err != nil {
		t.Fatalf("254 个候选未正确选择末项: %d, %v", index, err)
	}
	index, err = client.Select(context.Background(), sourceRequest(), options)
	if index != -1 || err == nil || calls.Load() != 1 {
		t.Fatalf("255 个候选被截断或提交: %d, %v, 请求=%d", index, err, calls.Load())
	}
}

func TestSelectMovieAndDefaultMatchThreshold(t *testing.T) {
	for _, test := range []struct {
		name     string
		match    float64
		rejected bool
	}{{"below default", 0.79, true}, {"at default", 0.8, false}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if strings.Contains(string(body), `"season_number"`) || strings.Contains(string(body), `"episode_number"`) {
					t.Error("电影候选不应伪造季集号")
				}
				fmt.Fprintf(w, `{"model":"jev-1.13.0","answers":{"select":{"type":"choice","choice":"c0","probabilities":{"c0":1,"none":0},"confidence":1},"match_0":{"type":"noul","noul":%v}},"usage":{"input_tokens":200,"output_tokens":30}}`, test.match)
			}))
			defer server.Close()
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), metadata.Request{Kind: metadata.Movie, Filename: "Example.2020.mkv"}, []metadata.Candidate{{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "123"}, Title: "Example", Year: 2020}})
			if test.rejected && (index != -1 || err == nil) || !test.rejected && (index != 0 || err != nil) {
				t.Fatalf("默认匹配门槛处理错误: %d, %v", index, err)
			}
		})
	}
}

func TestSelectRejectsRedirectWithoutForwardingAuthorization(t *testing.T) {
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirected.Add(1)
			io.WriteString(w, acceptedResponse)
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), sourceRequest(), workCandidates())
	if index != -1 || err == nil || redirected.Load() != 0 {
		t.Fatalf("重定向被接受: %d, %v, 转发=%d", index, err, redirected.Load())
	}
}

func TestEvidenceRetainsSourceScopedWorkReference(t *testing.T) {
	candidate := metadata.Candidate{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "371065", Slug: "show"}, Title: "Show", OriginalTitle: "Original Show", Year: 2020}
	payload, err := json.Marshal(buildEvaluation("jev-test", metadata.Request{Kind: metadata.Show}, []metadata.Candidate{candidate}))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Questions map[string]struct {
			Criteria map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	var actual metadata.Candidate
	if err := json.Unmarshal(raw.Questions["select"].Criteria["c0"], &actual); err != nil {
		t.Fatal(err)
	}
	if actual != candidate {
		t.Fatalf("作品引用或基本身份丢失: %+v", actual)
	}
}

func newTestClient(t *testing.T, c config.LLMConfig, threshold float64) *Client {
	t.Helper()
	client, err := New(c, threshold)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestJevCredentialsPreferExplicitConfigurationAndSnapshotEnvironment(t *testing.T) {
	for _, test := range []struct{ name, configured, want string }{
		{"explicit", "configured-key", "Bearer configured-key"},
		{"environment", "", "Bearer environment-key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", "environment-key")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != test.want {
					t.Error("Jev 凭据未遵循显式配置优先规则")
				}
				io.WriteString(w, acceptedResponse)
			}))
			defer server.Close()
			client := newTestClient(t, config.LLMConfig{Name: "judge", Type: "jev", BaseURL: server.URL, ApiKey: test.configured}, 0.8)
			t.Setenv("TYPESAFE_API_KEY", "changed-after-construction")
			if _, err := client.Select(context.Background(), sourceRequest(), workCandidates()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJevInstancesKeepTheirOwnEndpointCredentialsAndModel(t *testing.T) {
	var clients []*Client
	for _, name := range []string{"first", "second"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if r.Header.Get("Authorization") != "Bearer "+name+"-key" || request.Model != name+"-model" {
				t.Error("Jev 实例参数串用")
			}
			io.WriteString(w, acceptedResponse)
		}))
		t.Cleanup(server.Close)
		cfg := config.LLMConfig{Name: name, Type: "jev", BaseURL: server.URL, ApiKey: name + "-key", Model: name + "-model"}
		clients = append(clients, newTestClient(t, cfg, 0.9))
		cfg.BaseURL, cfg.ApiKey, cfg.Model = "https://changed.invalid", "changed", "changed"
	}
	for _, client := range clients {
		if selected, err := client.Select(context.Background(), sourceRequest(), workCandidates()); err != nil || selected != 1 {
			t.Fatalf("Jev 实例请求失败: %d %v", selected, err)
		}
	}
}

func TestNewRejectsInvalidProxyBeforeRequest(t *testing.T) {
	for _, proxyURL := range []string{"://invalid", "ftp://user:private-key@proxy.invalid:8080", "http:///", "socks5://", "http://proxy.invalid:99999"} {
		client, err := New(config.LLMConfig{Type: "jev", ApiKey: "key", Proxy: proxyURL}, 0.8)
		if err == nil || client != nil {
			t.Fatalf("Jev 构造接受了非法代理: %v", err)
		}
	}
}

func TestSelectRejectsInvalidBasicCandidatesBeforeIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"model":"jev-1.13.0","answers":{"select":{"type":"choice","choice":"c0","probabilities":{"c0":1,"none":0},"confidence":1},"match_0":{"type":"noul","noul":0.99}}}`)
	}))
	defer server.Close()
	client := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "test"}, 0.9)
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
			index, err := client.Select(context.Background(), metadata.Request{Kind: metadata.Show}, []metadata.Candidate{test.candidate})
			if err == nil || index != -1 {
				t.Fatalf("非法基本候选被接受: %d %v", index, err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("非法候选触发请求: %d", calls.Load())
	}
}
