package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"path/filepath"
	"strings"
)

// shouldSkipFile 让扫描和监听使用相同的文件排除规则。
func (c *collector) shouldSkipFile(name string) bool {
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

// configuredMediaRoot 按目录组件选取最长匹配根，避免同名前缀和嵌套媒体库误归属。
func configuredMediaRoot(path string) (string, media_file.VideoType) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", 0
	}
	path = filepath.Clean(path)
	var matchedRoot string
	var videoType media_file.VideoType
	for _, library := range []struct {
		roots []string
		kind  media_file.VideoType
	}{
		{config.Collector.MoviesDir, media_file.Movies},
		{config.Collector.ShowsDir, media_file.TvShows},
	} {
		for _, root := range library.roots {
			root, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			root = filepath.Clean(root)
			relative, err := filepath.Rel(root, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
			if matchedRoot == "" || len(root) >= len(matchedRoot) {
				matchedRoot, videoType = root, library.kind
			}
		}
	}
	return matchedRoot, videoType
}

// insideDiscDirectory 只在所属配置媒体根内检查原盘祖先，不解释根外目录名。
func insideDiscDirectory(path, root string) bool {
	path, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return false
	}
	path, root = filepath.Clean(path), filepath.Clean(root)
	if path == root {
		return false
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		name := filepath.Base(dir)
		if strings.EqualFold(name, media_file.BDMVType) || strings.EqualFold(name, media_file.VideoTsType) || strings.EqualFold(name, media_file.HvdvdType) {
			return true
		}
		if parent := filepath.Dir(dir); dir == root || parent == dir {
			return false
		}
	}
}

// shouldSkipDisc 原盘的电影名称位于原盘目录的父目录，沿用同一套排除规则。
func (c *collector) shouldSkipDisc(mf *media_file.MediaFile) bool {
	name := filepath.Base(mf.Dir)
	return c.shouldSkipFile(mf.Filename) || c.shouldSkipFile(name) || c.skipFolders(mf.Dir, name)
}
