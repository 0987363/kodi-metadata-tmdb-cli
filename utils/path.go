package utils

import (
	"path"
	"strings"
)

// NormalizePath 按统一分隔符比较路径，保留根目录和网络共享前缀。
func NormalizePath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	unc := strings.HasPrefix(value, "//")
	cleaned := path.Clean(value)
	if unc && strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	if len(cleaned) == 2 && cleaned[1] == ':' {
		cleaned += "/"
	}
	return cleaned
}

func PathEqual(pathA, pathB string) bool {
	return NormalizePath(pathA) == NormalizePath(pathB)
}
