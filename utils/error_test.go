package utils

import (
	"errors"
	"strings"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
)

func TestRedactErrorKeepsCauseWithoutConfiguredSecrets(t *testing.T) {
	oldModels, oldTMDB := config.LLMs, config.Tmdb
	t.Cleanup(func() { config.LLMs, config.Tmdb = oldModels, oldTMDB })
	config.LLMs = []config.LLMConfig{{ApiKey: "extract-secret"}, {ApiKey: "judge-secret"}, {}}
	config.Tmdb = &config.TmdbConfig{ApiKey: "tmdb-secret"}
	original := errors.Join(errors.New("HTTP 503 extract-secret"), errors.New("协议错误 judge-secret tmdb-secret"))
	message := RedactError(original)
	if strings.Contains(message, "-secret") || !strings.Contains(message, "HTTP 503") || !strings.Contains(message, "协议错误") || strings.Count(message, "[已隐藏]") != 3 {
		t.Fatalf("脱敏丢失原因或泄露凭据: %s", message)
	}
	config.LLMs, config.Tmdb = nil, nil
	if RedactError(original) != original.Error() {
		t.Fatal("无凭据配置仍改变错误")
	}
}
