package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// UnmarshalJSON 在所有配置层级拒绝未知字段，避免旧配置被静默忽略。
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var parsed plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return err
	}
	*c = Config(parsed)
	return nil
}

func (c *LLMConfig) UnmarshalJSON(data []byte) error {
	type plain LLMConfig
	var parsed plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return err
	}
	if parsed.Type == "jev" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if _, exists := fields["temperature"]; exists {
			return fmt.Errorf("Jev 模型 %q 不允许设置 temperature", parsed.Name)
		}
	}
	*c = LLMConfig(parsed)
	return nil
}

func parseConfig(data []byte) (*Config, error) {
	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	applyDefaults(&parsed)
	if parsed.Scraper == nil {
		return nil, errors.New("必须配置 scraper")
	}
	if parsed.Scraper.CacheHours < 0 {
		return nil, errors.New("scraper.cache_hours 不能为负数")
	}
	if _, err := findLLM(parsed.LLMs, parsed.Scraper.ExtractLLM); err != nil {
		return nil, fmt.Errorf("scraper.extract_llm: %w", err)
	}
	selected, err := findLLM(parsed.LLMs, parsed.Scraper.SelectLLM)
	if err != nil {
		return nil, fmt.Errorf("scraper.select_llm: %w", err)
	}
	threshold := parsed.Scraper.JevMatchThreshold
	if selected.Type == "jev" && (math.IsNaN(threshold) || threshold < 0 || threshold > 1) {
		return nil, errors.New("scraper.jev_match_threshold 必须在 0 到 1 之间")
	}
	return &parsed, nil
}
