package providers

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"strings"
	"testing"
)

func factoryConfig(t *testing.T) {
	t.Helper()
	oldL, oldS, oldT, oldC := config.LLMs, config.Scraper, config.TheTVDB, config.Collector
	t.Cleanup(func() { config.LLMs, config.Scraper, config.TheTVDB, config.Collector = oldL, oldS, oldT, oldC })
	config.LLMs = []config.LLMConfig{{Name: "general", Type: "openai", BaseURL: "https://example.invalid/v1", ApiKey: "test", Model: "general"}, {Name: "decision", Type: "jev"}}
	config.Scraper = &config.ScraperConfig{Providers: []string{"thetvdb"}, ExtractLLM: "general", SelectLLM: "general", JevMatchThreshold: 0.8}
	config.TheTVDB = &config.TheTVDBConfig{BaseURL: "https://thetvdb.com", TimeoutSeconds: 1}
	config.Collector = &config.CollectorConfig{}
}

func TestFactoryNamedModelAndSourceValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func()
		want   string
	}{
		{"valid", func() {}, ""},
		{"missing_extract", func() { config.Scraper.ExtractLLM = "missing" }, "提取模型"},
		{"jev_cannot_extract", func() { config.Scraper.ExtractLLM = "decision" }, "extract_llm"},
		{"missing_select", func() { config.Scraper.SelectLLM = "missing" }, "判断模型"},
		{"selected_jev_needs_key", func() { config.Scraper.SelectLLM = "decision" }, "判断模型"},
		{"unused_jev_threshold", func() { config.Scraper.JevMatchThreshold = -1 }, ""},
		{"invalid_source", func() { config.Scraper.Providers = []string{"unknown"} }, "未知的数据源"},
		{"duplicate_source", func() { config.Scraper.Providers = []string{"thetvdb", "thetvdb"} }, "重复的数据源"},
		{"empty_sources", func() { config.Scraper.Providers = nil }, "providers"},
		{"negative_cache", func() { config.Scraper.CacheHours = -1 }, "cache_hours"},
		{"missing_key", func() { config.LLMs[0].ApiKey = "" }, "提取模型"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factoryConfig(t)
			t.Setenv("TYPESAFE_API_KEY", "")
			tc.change()
			extract, m, images, err := New()
			if tc.want == "" {
				if err != nil || extract == nil || m == nil || images == nil {
					t.Fatalf("装配失败：%v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误配置未拒绝：%v", err)
			}
		})
	}
}
