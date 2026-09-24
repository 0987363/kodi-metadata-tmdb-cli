package shows

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"fmt"
	"path/filepath"
)

func parseShowFileContext(ctx context.Context, mf *media_file.MediaFile, extractor *ai.Client) (*Show, error) {
	if mf == nil {
		return nil, errors.New("剧集文件为空")
	}
	if extractor == nil {
		return nil, errors.New("LLM 提取器未初始化")
	}
	result, err := extractor.ParseMediaContext(ctx, &ai.ParseInput{MediaType: "tv", Path: mf.Path, Filename: mf.Filename})
	if err != nil {
		return nil, err
	}
	show := &Show{MediaFile: mf, Title: result.Title, AliasTitle: result.AliasTitle, ChsTitle: result.ChsTitle, EngTitle: result.EngTitle, Year: result.Year, TMDBID: result.TMDBID, TheTVDBID: result.TheTVDBID, EpisodeTitle: result.EpisodeTitle}
	if err := fillShowPathMeta(show); err != nil {
		return nil, err
	}
	if result.Season != nil {
		show.Season = *result.Season
		show.SeasonKnown = true
	}
	if result.Episode != nil {
		show.Episode = *result.Episode
	}
	return show, nil
}
func fillShowPathMeta(show *Show) error {
	if show == nil || show.MediaFile == nil {
		return nil
	}
	roots := make([]string, 0, len(config.Collector.ShowsDir))
	for _, root := range config.Collector.ShowsDir {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("解析节目库目录：%w", err)
		}
		roots = append(roots, filepath.Clean(absolute))
	}

	cursor := show.MediaFile.Path
	for {
		parent := filepath.Dir(cursor)

		absoluteParent, err := filepath.Abs(parent)
		if err != nil {
			return fmt.Errorf("解析剧集父目录：%w", err)
		}
		for _, showsDir := range roots {
			if utils.PathEqual(showsDir, filepath.Clean(absoluteParent)) {
				show.TvRoot = cursor
				goto foundRoot
			}
		}

		if utils.PathEqual(parent, cursor) {
			break
		}
		cursor = parent
	}

foundRoot:
	if show.TvRoot == "" {
		show.TvRoot = filepath.Dir(show.MediaFile.Path)
	}
	if show.SeasonRoot == "" {
		show.SeasonRoot = filepath.Dir(show.MediaFile.Path)
	}
	return nil
}
