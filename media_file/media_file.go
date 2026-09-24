package media_file

import (
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"path/filepath"
	"strings"
)

// NewMediaFile 实例化媒体类型
func NewMediaFile(path, filename string) *MediaFile {
	path = utils.NormalizePath(path)

	if strings.HasPrefix(filename, ".") || filename == "" {
		return nil
	}

	return &MediaFile{
		Path:      path,
		Dir:       filepath.Dir(path),
		Filename:  filename,
		MediaType: parseMediaType(filename),
		Suffix:    filepath.Ext(filename),
	}
}

// MediaType 解析文件类型
func parseMediaType(filename string) MediaType {
	filename = strings.ToLower(filename)
	ext := filepath.Ext(filename)

	if strings.EqualFold(ext, NfoType) {
		return NFO
	}

	// 图片
	for _, v := range ArtworkFileTypes { // todo map
		if strings.HasSuffix(filename, v) {
			return GRAPHIC
		}
	}

	for _, v := range AudioFileTypes {
		if strings.HasSuffix(filename, v) {
			return AUDIO
		}
	}

	for _, v := range SubtitleFileTypes {
		if strings.HasSuffix(filename, v) {
			return SUBTITLE
		}
	}

	if filename == VideoTsType || filename == BDMVType || filename == HvdvdType || filename == DVDType {
		return DISC
	}

	for _, v := range VideoFileTypes {
		if strings.HasSuffix(filename, v) {
			return VIDEO
		}
	}

	return UNKNOWN
}
