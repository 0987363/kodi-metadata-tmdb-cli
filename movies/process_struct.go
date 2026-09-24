package movies

import (
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"path/filepath"
)

// Movie 保存文件识别结果与输出位置，来源记录由 metadata.Ref 表示。
type Movie struct {
	MediaFile     *media_file.MediaFile `json:"media_file"`
	PosterFile    string                `json:"poster_file"`
	FanArtFile    string                `json:"fanart_file"`
	ClearLogoFile string                `json:"clearlogo_file"`
	NfoFiles      []string              `json:"nfo_files"` // 当前电影必需的 NFO 输出位置；原盘在根目录和索引目录保存相同事实
	Title         string                `json:"title"`
	AliasTitle    string                `json:"alias_title"`
	ChsTitle      string                `json:"chs_title"`
	EngTitle      string                `json:"eng_title"`
	Year          int                   `json:"year"`
	TMDBID        string                `json:"tmdb_id,omitempty"`    // AI 返回的 TMDb 电影编号，仍需验证
	TheTVDBID     string                `json:"thetvdb_id,omitempty"` // 保留识别结果，由已启用来源能力校验
}

func (m *Movie) GetCacheDir() string { return filepath.Join(filepath.Dir(m.MediaFile.Path), "tmdb") }
func (m *Movie) IdFile() string {
	return filepath.Join(m.GetCacheDir(), m.MediaFile.Filename+".id.txt")
}
