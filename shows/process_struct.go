package shows

import (
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"os"
	"path/filepath"
	"strings"
)

// Show 区分文件季集坐标、人工 TMDb 指定和 AI 识别提示。
type Show struct {
	EpisodeTitle string                `json:"episode_title,omitempty"` // LLM 从文件上下文提取的单集名称提示
	MediaFile    *media_file.MediaFile `json:"media_file"`
	TvRoot       string                `json:"tv_root"`
	SeasonRoot   string                `json:"season_root"`
	TvId         int                   `json:"tv_id"`    // 人工指定的 TMDb 节目编号
	GroupId      string                `json:"group_id"` // 人工指定的 TMDb 剧集分组编号
	Season       int                   `json:"season"`
	SeasonKnown  bool                  `json:"-"`
	Episode      int                   `json:"episode"`
	Title        string                `json:"title"`
	AliasTitle   string                `json:"alias_title"`
	ChsTitle     string                `json:"chs_title"`
	EngTitle     string                `json:"eng_title"`
	Year         int                   `json:"year"`
	TMDBID       string                `json:"tmdb_id,omitempty"`    // AI 返回的 TMDb 节目编号，仍需验证
	TheTVDBID    string                `json:"thetvdb_id,omitempty"` // AI 返回的 TheTVDB 节目编号，仍需验证
}

func (s *Show) GetCacheDir() string { return filepath.Join(filepath.Dir(s.MediaFile.Path), "tmdb") }

// ReadGroupId 季目录显式分组优先，读取失败必须向调用方报告。
func (s *Show) ReadGroupId() error {
	for _, dir := range []string{s.SeasonRoot, s.TvRoot} {
		if dir == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "tmdb", "group.txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		s.GroupId = strings.TrimSpace(string(data))
		return nil
	}
	return nil
}
