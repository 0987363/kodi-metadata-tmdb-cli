package config

import (
	"errors"
	"fmt"
	"strings"
)

func FindLLM(name string) (LLMConfig, error) {
	return findLLM(LLMs, name)
}

func findLLM(models []LLMConfig, name string) (LLMConfig, error) {
	names := make(map[string]struct{}, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.Name) == "" {
			return LLMConfig{}, errors.New("llms.name 不能为空")
		}
		if _, exists := names[model.Name]; exists {
			return LLMConfig{}, fmt.Errorf("llms.name 重复: %q", model.Name)
		}
		names[model.Name] = struct{}{}
		if model.Type != "openai" && model.Type != "jev" {
			return LLMConfig{}, fmt.Errorf("模型 %q 的 type 必须为 openai 或 jev", model.Name)
		}
		if model.Type == "jev" && model.Temperature != nil {
			return LLMConfig{}, fmt.Errorf("Jev 模型 %q 不允许设置 temperature", model.Name)
		}
	}
	if strings.TrimSpace(name) == "" {
		return LLMConfig{}, errors.New("刮削模型实例名称不能为空")
	}
	for _, model := range models {
		if model.Name == name {
			return model, nil
		}
	}
	return LLMConfig{}, fmt.Errorf("未找到模型实例 %q", name)
}
