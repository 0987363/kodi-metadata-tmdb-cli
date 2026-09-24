package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"strings"
)

func skipFolder(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if config.Collector == nil {
		return false
	}
	for _, folder := range config.Collector.SkipFolders {
		if folder != "" && name == folder {
			return true
		}
	}
	return false
}

func skipFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if config.Collector == nil {
		return false
	}
	for _, keyword := range config.Collector.SkipKeywords {
		if keyword != "" && strings.Contains(name, keyword) {
			return true
		}
	}
	for _, suffix := range config.Collector.TmpSuffix {
		if suffix != "" && strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
			return true
		}
	}
	return false
}
