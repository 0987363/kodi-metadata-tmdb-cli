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

func candidateOptions() []metadata.Option {
	return []metadata.Option{
		{Work: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "同名节目", OriginalTitle: "Original Show", Plot: "节目介绍", Premiered: "2020-01-02", Actors: []metadata.Actor{{Name: "不应发送的演员"}}, Artwork: []metadata.Artwork{{URL: "https://image.example/should-not-send.jpg"}}}, Episode: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: "100"}, Title: "试播集", SeasonNumber: 0, EpisodeNumber: 2, Premiered: "2020-01-03"}},
		{Work: &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Title: "同名节目", OriginalTitle: "Original Show", Plot: "节目介绍", Premiered: "2020-01-02"}, Episode: &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Episode, ID: "100"}, Title: "試播集", SeasonNumber: 0, EpisodeNumber: 2, Premiered: "2020-01-03"}},
	}
}

func sourceRequest() metadata.Request {
	return metadata.Request{Path: "/library/同名节目/Season 0", Filename: "Original.Show.S00E02.mkv", Kind: metadata.Show, Query: metadata.Query{Kind: metadata.Show, Title: "同名节目", OriginalTitle: "Original Show", Year: 2020}, EpisodeTitle: "试播集", Season: 0, Episode: 2, Group: "aired", Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "42"}, Hints: []metadata.Ref{{Provider: "tmdb", Kind: metadata.Show, ID: "42"}}}
}

func TestSelectUsesChoiceAndNoulWithIndependentSourceEvidence(t *testing.T) {
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
			Model     string           `json:"model"`
			State     metadata.Request `json:"state"`
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
		if request.Model != "jev-latest" || request.State.Filename != "Original.Show.S00E02.mkv" || request.State.Ref.Provider != "thetvdb" || request.State.Group != "aired" || request.State.Episode != 2 || len(request.State.Hints) != 1 {
			t.Errorf("模型或输入证据错误: %#v", request)
		}
		if !strings.Contains(string(body), `"season":0`) {
			t.Error("输入证据丢失第 0 季")
		}
		if len(request.Questions) != 3 {
			t.Errorf("问题数量=%d", len(request.Questions))
		}
		selectQuestion := request.Questions["select"]
		if selectQuestion.Type != "choice" || len(selectQuestion.Criteria) != 3 || len(selectQuestion.Criteria["none"]) == 0 || len(selectQuestion.Options) != 0 {
			t.Errorf("Choice 必须采用完整的 criteria: %#v", selectQuestion)
		}
		for i, source := range []string{"tmdb", "thetvdb"} {
			var evidence struct {
				Work struct {
					Source, ID, Title, Plot, Premiered string
					OriginalTitle                      string `json:"original_title"`
				} `json:"work"`
				Episode struct {
					Source        string
					SeasonNumber  *int `json:"season_number"`
					EpisodeNumber int  `json:"episode_number"`
				} `json:"episode"`
			}
			if err := json.Unmarshal(selectQuestion.Criteria[fmt.Sprintf("c%d", i)], &evidence); err != nil {
				t.Error(err)
			}
			if evidence.Work.Source != source || evidence.Work.ID != "42" || evidence.Work.Title != "同名节目" || evidence.Work.OriginalTitle != "Original Show" || evidence.Work.Plot != "节目介绍" || evidence.Work.Premiered != "2020-01-02" || evidence.Episode.Source != source || evidence.Episode.SeasonNumber == nil || *evidence.Episode.SeasonNumber != 0 || evidence.Episode.EpisodeNumber != 2 {
				t.Errorf("候选来源或精简事实丢失: %#v", evidence)
			}
			match := request.Questions[fmt.Sprintf("match_%d", i)]
			if match.Type != "noul" || !strings.Contains(string(match.Instructions), source) || !strings.Contains(string(match.Instructions), "试") && !strings.Contains(string(match.Instructions), "試") {
				t.Errorf("Noul 未携带实际候选证据: %s", match.Instructions)
			}
		}
		for _, forbidden := range []string{"test-secret", "不应发送的演员", "should-not-send.jpg", `"actors"`, `"artwork"`} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("请求泄露无关信息 %q", forbidden)
			}
		}
		io.WriteString(w, acceptedResponse)
	}))
	defer server.Close()
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "test-secret"}, 0.9).Select(context.Background(), sourceRequest(), candidateOptions())
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
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0.9).Select(context.Background(), sourceRequest(), candidateOptions())
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
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0.9).Select(context.Background(), sourceRequest(), candidateOptions())
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
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "secret-api-key"}, 0).Select(context.Background(), sourceRequest(), candidateOptions())
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
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), sourceRequest(), candidateOptions())
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
			_, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL + suffix, ApiKey: "key", Model: "jev-1.13.0"}, 0).Select(context.Background(), sourceRequest(), candidateOptions())
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
		options   []metadata.Option
	}{
		{"empty config", config.LLMConfig{}, 0, candidateOptions()},
		{"wrong protocol", config.LLMConfig{Type: "openai", BaseURL: server.URL, ApiKey: "key"}, 0, candidateOptions()},
		{"empty key", config.LLMConfig{Type: "jev", BaseURL: server.URL}, 0, candidateOptions()},
		{"temperature", config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", Temperature: &temperature}, 0, candidateOptions()},
		{"empty candidates", valid, 0, nil},
		{"nil work", valid, 0, []metadata.Option{{}}},
		{"too many candidates", valid, 0, make([]metadata.Option, 255)},
		{"negative threshold", valid, -0.1, candidateOptions()},
		{"threshold above one", valid, 1.1, candidateOptions()},
		{"nan threshold", valid, math.NaN(), candidateOptions()},
		{"invalid endpoint", config.LLMConfig{Type: "jev", BaseURL: "not-a-url", ApiKey: "key"}, 0, candidateOptions()},
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
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", TimeoutSeconds: test.timeoutSeconds}, 0).Select(ctx, sourceRequest(), candidateOptions())
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
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key", TimeoutSeconds: 1}, 0).Select(context.Background(), sourceRequest(), candidateOptions())
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
	options := make([]metadata.Option, 255)
	for i := range options {
		options[i] = candidateOptions()[0]
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
			index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), metadata.Request{Kind: metadata.Movie, Filename: "Example.2020.mkv"}, []metadata.Option{{Work: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "123"}, Title: "Example", Premiered: "2020-01-01"}}})
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
	index, err := newTestClient(t, config.LLMConfig{Type: "jev", BaseURL: server.URL, ApiKey: "key"}, 0).Select(context.Background(), sourceRequest(), candidateOptions())
	if index != -1 || err == nil || redirected.Load() != 0 {
		t.Fatalf("重定向被接受: %d, %v, 转发=%d", index, err, redirected.Load())
	}
}

func TestEvidenceIncludesVerifiedExternalReferences(t *testing.T) {
	option := metadata.Option{Work: &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "371065"}, Title: "Show", ExternalIDs: []metadata.Identifier{{Type: "tmdb", Value: "74747"}}}}
	payload, err := json.Marshal(buildEvaluation("jev-test", metadata.Request{Kind: metadata.Show}, []metadata.Option{option}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Questions map[string]struct {
			Criteria map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	var candidate struct {
		Work struct {
			ExternalIDs []metadata.Identifier `json:"external_ids"`
		} `json:"work"`
	}
	if err := json.Unmarshal(got.Questions["select"].Criteria["c0"], &candidate); err != nil {
		t.Fatal(err)
	}
	if len(candidate.Work.ExternalIDs) != 1 || candidate.Work.ExternalIDs[0].Type != "tmdb" || candidate.Work.ExternalIDs[0].Value != "74747" {
		t.Fatal("Jev证据遗漏网站已验证的同对象跨站编号")
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
			if _, err := client.Select(context.Background(), sourceRequest(), candidateOptions()); err != nil {
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
		if selected, err := client.Select(context.Background(), sourceRequest(), candidateOptions()); err != nil || selected != 1 {
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
