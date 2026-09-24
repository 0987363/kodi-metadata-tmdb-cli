package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const configuredModels = `"llms":[{"name":"extract","type":"openai","base_url":"https://example.invalid/v1","api_key":"key","model":"extract-model"},{"name":"judge","type":"openai","base_url":"https://example.invalid/v1","api_key":"judge-key","model":"judge-model"},{"name":"unused-jev","type":"jev"}]`

func writeTestConfig(t *testing.T, logMode, logLevel int) string {
	t.Helper()
	content := fmt.Sprintf(`{
  %s,
  "scraper":{"providers":["thetvdb"],"extract_llm":"extract","select_llm":"judge","cache_hours":0,"nfo_field":{"tag":true,"genre":false}},
  "log":{"mode":%d,"level":%d,"file":"./test.log"},
  "collector":{"skip_folders":["cache"],"skip_keywords":["临时"],"tmp_suffix":[".part"]}
}`, configuredModels, logMode, logLevel)
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadConfigPreservesLogEnumsAndCollectorFilters(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		mode, level, wantMode, wantLevel int
	}{
		{name: "valid", mode: LogModeBoth, level: LogLevelFatal, wantMode: LogModeBoth, wantLevel: LogLevelFatal},
		{name: "invalid", mode: 99, level: 99, wantMode: LogModeStdout, wantLevel: LogLevelInfo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			LoadConfig(writeTestConfig(t, tc.mode, tc.level))
			if Log.Mode != tc.wantMode || Log.Level != tc.wantLevel {
				t.Fatalf("既有枚举行为变化: %+v %+v", Collector, Log)
			}
			if len(Collector.SkipFolders) != 1 || Collector.SkipFolders[0] != "cache" || len(Collector.SkipKeywords) != 1 || Collector.SkipKeywords[0] != "临时" || len(Collector.TmpSuffix) != 1 || Collector.TmpSuffix[0] != ".part" {
				t.Fatal("过滤配置未加载")
			}
		})
	}
}

func TestLoadConfigBindsNamedModelsAndScraperWithoutUnusedCredentials(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	LoadConfig(writeTestConfig(t, 1, 1))
	if len(LLMs) != 3 || Scraper == nil || Scraper.ExtractLLM != "extract" || Scraper.SelectLLM != "judge" || len(Scraper.Providers) != 1 || Scraper.Providers[0] != "thetvdb" || Scraper.CacheHours != 0 || !Scraper.NfoField.Tag || Scraper.NfoField.Genre {
		t.Fatalf("模型列表或独立刮削配置未正确绑定: %+v %+v", LLMs, Scraper)
	}
	if TheTVDB.Language != "zho" || TheTVDB.TimeoutSeconds != 30 || Tmdb.ApiKey != "" || Scraper.JevMatchThreshold != 0.8 {
		t.Fatal("默认配置或不需要 TMDb 凭据的来源模式变化")
	}
	model, err := FindLLM("judge")
	if err != nil || model.Model != "judge-model" || model.ApiKey != "judge-key" {
		t.Fatalf("名称未解析到对应实例: %+v %v", model, err)
	}
}

func TestParseConfigRejectsModelDefinitionAndReferenceErrors(t *testing.T) {
	for _, tc := range []struct{ name, models, scraper string }{
		{"empty name", `[{"name":" ","type":"openai"}]`, `{"extract_llm":" ","select_llm":" "}`},
		{"duplicate names", `[{"name":"extract","type":"openai"},{"name":"extract","type":"jev"}]`, `{"extract_llm":"extract","select_llm":"extract"}`},
		{"unknown type", `[{"name":"extract","type":"other"}]`, `{"extract_llm":"extract","select_llm":"extract"}`},
		{"missing extract", `[{"name":"extract","type":"openai"}]`, `{"select_llm":"extract"}`},
		{"missing select", `[{"name":"extract","type":"openai"}]`, `{"extract_llm":"extract"}`},
		{"unknown extract", `[{"name":"extract","type":"openai"}]`, `{"extract_llm":"missing","select_llm":"extract"}`},
		{"unknown select", `[{"name":"extract","type":"openai"}]`, `{"extract_llm":"extract","select_llm":"missing"}`},
		{"negative cache", `[{"name":"extract","type":"openai"}]`, `{"extract_llm":"extract","select_llm":"extract","cache_hours":-1}`},
		{"jev temperature", `[{"name":"extract","type":"openai"},{"name":"unused","type":"jev","temperature":0}]`, `{"extract_llm":"extract","select_llm":"extract"}`},
		{"jev null temperature", `[{"name":"extract","type":"openai"},{"name":"unused","type":"jev","temperature":null}]`, `{"extract_llm":"extract","select_llm":"extract"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig([]byte(`{"llms":` + tc.models + `,"scraper":` + tc.scraper + `}`))
			if err == nil {
				t.Fatal("错误的模型或引用被接受")
			}
		})
	}
	for _, body := range []string{`{}`, `null`, `{"llms":[],"scraper":{}}`} {
		if _, err := parseConfig([]byte(body)); err == nil {
			t.Fatalf("未配置刮削模型却被接受: %s", body)
		}
	}
}

func TestParseConfigOnlyValidatesSelectedJevThreshold(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		threshold  float64
		wantError  bool
	}{
		{"openai ignores jev threshold", "openai", 2, false},
		{"jev rejects excessive threshold", "jev", 2, true},
		{"jev rejects negative threshold", "jev", -0.1, true},
		{"jev accepts threshold", "jev", 0.9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"llms":[{"name":"extract","type":"openai"},{"name":"judge","type":%q}],"scraper":{"extract_llm":"extract","select_llm":"judge","jev_match_threshold":%v}}`, tc.kind, tc.threshold)
			_, err := parseConfig([]byte(body))
			if (err != nil) != tc.wantError {
				t.Fatalf("门槛校验结果: %v", err)
			}
		})
	}
}

func TestFindLLMRejectsMissingAndMalformedDefinitions(t *testing.T) {
	old := LLMs
	t.Cleanup(func() { LLMs = old })
	for _, tc := range []struct {
		name      string
		models    []LLMConfig
		reference string
	}{
		{"missing", []LLMConfig{{Name: "extract", Type: "openai"}}, "missing"},
		{"empty reference", []LLMConfig{{Name: "extract", Type: "openai"}}, ""},
		{"duplicate", []LLMConfig{{Name: "extract", Type: "openai"}, {Name: "extract", Type: "jev"}}, "extract"},
		{"unsupported unselected", []LLMConfig{{Name: "extract", Type: "openai"}, {Name: "other", Type: "other"}}, "extract"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			LLMs = tc.models
			if _, err := FindLLM(tc.reference); err == nil {
				t.Fatal("错误模型定义被接受")
			}
		})
	}
}

func TestExampleConfigurationUsesNamedModels(t *testing.T) {
	data, err := os.ReadFile("../example.config.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.LLMs) != 3 || parsed.Scraper.ExtractLLM != "media-extract" || parsed.Scraper.SelectLLM != "jev-select" || strings.Join(parsed.Scraper.Providers, ",") != "thetvdb,tmdb" {
		t.Fatal("示例未完整展示具名实例与来源顺序")
	}
	if _, err := json.Marshal(parsed); err != nil {
		t.Fatal(err)
	}
}
