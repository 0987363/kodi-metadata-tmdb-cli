package utils

import (
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/config"
)

// RedactError 隐藏错误消息中的已配置模型和 TMDb 凭据，供编排边界记录失败原因。
func RedactError(err error) string {
	message := err.Error()
	for _, model := range config.LLMs {
		if model.ApiKey != "" {
			message = strings.ReplaceAll(message, model.ApiKey, "[已隐藏]")
		}
	}
	if config.Tmdb != nil && config.Tmdb.ApiKey != "" {
		message = strings.ReplaceAll(message, config.Tmdb.ApiKey, "[已隐藏]")
	}
	return message
}
