package movies

import (
	"context"
	"encoding/json"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type judgeForTest struct{}

func (judgeForTest) Select(context.Context, metadata.Request, []metadata.Option) (int, error) {
	return 0, nil
}
func setAnalysisResponse(t *testing.T, content string) *ai.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(s.Close)
	client, err := ai.New(config.LLMConfig{Name: "extractor", Type: "openai", BaseURL: s.URL, ApiKey: "test", Model: "test", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestParseRejectsMissingExtractor(t *testing.T) {
	_, err := parseMoviesFileContext(context.Background(), &media_file.MediaFile{Path: "Unknown.mkv", Filename: "Unknown.mkv"}, nil)
	if err == nil || !strings.Contains(err.Error(), "提取器") {
		t.Fatalf("未返回缺失提取器错误：%v", err)
	}
}

func TestProcessRejectsMissingScraperConfig(t *testing.T) {
	old := config.Scraper
	t.Cleanup(func() { config.Scraper = old })
	config.Scraper = nil
	err := ProcessMetadata(context.Background(), &media_file.MediaFile{Path: "Unknown.mkv", Filename: "Unknown.mkv"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "scraper") {
		t.Fatalf("未返回缺失刮削配置错误：%v", err)
	}
}
