package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/common/httpx"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

const maxResponseBytes = 2 * 1024 * 1024

type Client struct {
	httpClient     *http.Client
	endpoint       string
	apiKey         string
	model          string
	matchThreshold float64
}

var _ metadata.Judge = (*Client)(nil)

func New(c config.LLMConfig, threshold float64) (*Client, error) {
	if c.Type != "jev" {
		return nil, errors.New("Jev 客户端仅支持 jev 类型")
	}
	if c.Temperature != nil {
		return nil, errors.New("Jev 实例不允许设置 temperature")
	}
	apiKey := strings.TrimSpace(c.ApiKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("Jev API key 未配置")
	}
	endpoint, err := normalizeEndpoint(c.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "jev-latest"
	}
	if threshold == 0 {
		threshold = 0.8
	}
	if !validProbability(threshold) {
		return nil, errors.New("scraper.jev_match_threshold 必须在 0 到 1 之间")
	}
	if err := httpx.ValidateProxy(c.Proxy); err != nil {
		return nil, err
	}
	timeout := c.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	httpClient := httpx.NewClient(c.Proxy, timeout)
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: httpClient, endpoint: endpoint, apiKey: apiKey, model: model, matchThreshold: threshold}, nil
}

func normalizeEndpoint(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "https://api.typesafe.ai/v1"
	}
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", errors.New("Jev base_url 必须是无凭据、查询参数或片段的 HTTP(S) 地址")
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	switch {
	case strings.HasSuffix(endpoint.Path, "/systemone"):
	case strings.HasSuffix(endpoint.Path, "/v1"):
		endpoint.Path += "/systemone"
	default:
		endpoint.Path += "/v1/systemone"
	}
	return endpoint.String(), nil
}

func (c *Client) Select(ctx context.Context, request metadata.Request, options []metadata.Candidate) (int, error) {
	if c == nil || c.httpClient == nil {
		return -1, errors.New("Jev 客户端未初始化")
	}
	if len(options) == 0 {
		return -1, errors.New("Jev 候选为空")
	}
	if len(options) > 254 {
		return -1, errors.New("Jev 最多支持 254 个真实候选及一个 none 选项")
	}
	for i, option := range options {
		if strings.TrimSpace(option.Title) == "" || strings.TrimSpace(option.Ref.Provider) == "" || strings.TrimSpace(option.Ref.ID) == "" || option.Ref.Kind != metadata.Movie && option.Ref.Kind != metadata.Show {
			return -1, fmt.Errorf("Jev 候选 c%d 缺少合法的作品基本身份", i)
		}
	}
	body, err := json.Marshal(buildEvaluation(c.model, request, options))
	if err != nil {
		return -1, errors.New("Jev 请求无法编码为 JSON")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return -1, errors.New("Jev HTTP 请求构造失败")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return -1, fmt.Errorf("Jev 请求取消: %w", ctx.Err())
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return -1, fmt.Errorf("Jev 请求超时: %w", context.DeadlineExceeded)
		}
		// 传输错误可能包含请求头或代理凭据，不直接暴露第三方错误文本。
		return -1, errors.New("Jev HTTP 请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("Jev HTTP 状态码 %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return -1, fmt.Errorf("Jev 响应读取取消: %w", ctx.Err())
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return -1, fmt.Errorf("Jev 响应读取超时: %w", context.DeadlineExceeded)
		}
		return -1, errors.New("Jev 响应读取失败")
	}
	if len(data) > maxResponseBytes {
		return -1, errors.New("Jev 响应超过 2 MiB")
	}
	return selectAnswer(data, len(options), c.matchThreshold)
}

type evaluationResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
}

type choiceAnswer struct {
	Type          string              `json:"type"`
	Choice        string              `json:"choice"`
	Probabilities map[string]*float64 `json:"probabilities"`
	Confidence    *float64            `json:"confidence"`
}

type noulAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

func selectAnswer(data []byte, count int, threshold float64) (int, error) {
	var response evaluationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return -1, errors.New("Jev 响应不是有效的 JSON")
	}
	if strings.TrimSpace(response.Model) == "" {
		return -1, errors.New("Jev 响应缺少模型名称")
	}
	var answer choiceAnswer
	if err := json.Unmarshal(response.Answers["select"], &answer); err != nil || answer.Type != "choice" || answer.Confidence == nil || !validProbability(*answer.Confidence) {
		return -1, errors.New("Jev Choice 响应缺失、类型错误或 confidence 无效")
	}
	keys := make(map[string]int, count+1)
	keys["none"] = -1
	for i := range count {
		keys[fmt.Sprintf("c%d", i)] = i
	}
	index, known := keys[answer.Choice]
	if !known {
		return -1, errors.New("Jev Choice 返回了未知候选")
	}
	if len(answer.Probabilities) != len(keys) {
		return -1, errors.New("Jev Choice 概率字典不完整")
	}
	var sum, highest float64
	for key := range keys {
		probability := answer.Probabilities[key]
		if probability == nil || !validProbability(*probability) {
			return -1, errors.New("Jev Choice 概率缺失或超出 0 到 1")
		}
		sum += *probability
		highest = math.Max(highest, *probability)
	}
	if math.Abs(sum-1) > 0.000001 || *answer.Probabilities[answer.Choice] < highest {
		return -1, errors.New("Jev Choice 概率和不为 1 或所选项不是最高概率")
	}
	var selectedMatch float64
	for i := range count {
		var match noulAnswer
		if err := json.Unmarshal(response.Answers[fmt.Sprintf("match_%d", i)], &match); err != nil || match.Type != "noul" || match.Noul == nil || !validProbability(*match.Noul) {
			return -1, fmt.Errorf("Jev 候选 c%d 的 Noul 缺失、类型错误或超出 0 到 1", i)
		}
		if i == index {
			selectedMatch = *match.Noul
		}
	}
	if index == -1 {
		return -1, fmt.Errorf("%w: Jev 判断所有候选均不匹配", metadata.ErrNotFound)
	}
	// Choice 的区分度可以因两个正确来源而降低；是否匹配只采用所选候选的 Noul。
	if selectedMatch < threshold {
		return -1, fmt.Errorf("%w: Jev 候选匹配概率 %.3f 低于门槛 %.3f", metadata.ErrAmbiguous, selectedMatch, threshold)
	}
	return index, nil
}

func validProbability(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1
}
