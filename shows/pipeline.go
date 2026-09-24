package shows

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/nfo"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func ProcessMetadata(ctx context.Context, mf *media_file.MediaFile, extractor *ai.Client, manager *metadata.Manager, images *artwork.Downloader) error {
	if mf == nil {
		return errors.New("剧集文件为空")
	}
	if config.Scraper == nil {
		return errors.New("刮削配置 scraper 未初始化")
	}
	show, err := parseShowFileContext(ctx, mf, extractor)
	if err != nil {
		return err
	}
	if err := show.readMetadataOverrides(); err != nil {
		return err
	}
	root := filepath.Join(show.TvRoot, ".metadata")
	ref, err := metadata.LoadReference(filepath.Join(root, "source.json"), metadata.Show)
	if err != nil {
		return err
	}
	if show.TvId > 0 {
		ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: strconv.Itoa(show.TvId)})
		if err != nil {
			return err
		}
	}
	if show.GroupId != "" {
		ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Show})
		if err != nil {
			return err
		}
	}
	if !show.SeasonKnown || show.Episode < 1 || show.Season < 0 {
		return errors.New("LLM 未识别出有效季集号，且人工设置未补足")
	}
	var hints []metadata.Ref
	if show.TMDBID != "" {
		hints = append(hints, metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: show.TMDBID})
	}
	if show.TheTVDBID != "" {
		hints = append(hints, metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: show.TheTVDBID})
	}
	query := metadata.Query{Kind: metadata.Show, Title: show.Title, ChineseTitle: show.ChsTitle, OriginalTitle: show.EngTitle, Year: show.Year}
	selected, err := manager.Resolve(ctx, metadata.Request{Kind: metadata.Show, Ref: ref, Hints: hints, Path: mf.Path, Filename: mf.Filename, Query: query, Season: show.Season, Episode: show.Episode, EpisodeTitle: show.EpisodeTitle, Group: show.GroupId}, root)
	if err != nil {
		return err
	}
	detail, episode := selected.Work, selected.Episode
	if err := nfo.Write(filepath.Join(show.TvRoot, "tvshow.nfo"), detail, "", config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
		return err
	}
	episodeFile := strings.TrimSuffix(mf.Path, mf.Suffix)
	if err := nfo.Write(episodeFile+".nfo", episode, detail.Title, config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
		return err
	}
	var items []artwork.Item
	for _, a := range detail.Artwork {
		file := ""
		switch a.Kind {
		case "poster":
			file = "poster.jpg"
		case "fanart":
			file = "fanart.jpg"
		case "clearlogo":
			file = "clearlogo.png"
		case "season_poster":
			if a.Season == show.Season {
				file = fmt.Sprintf("season%02d-poster.jpg", show.Season)
			}
		}
		if file != "" {
			items = append(items, artwork.Item{URL: a.URL, Path: filepath.Join(show.TvRoot, file)})
		}
	}
	if err := images.Save(ctx, detail.Ref.Provider, items); err != nil {
		return err
	}
	items = nil
	for _, a := range episode.Artwork {
		if a.Kind == "thumb" {
			items = append(items, artwork.Item{URL: a.URL, Path: episodeFile + "-thumb.jpg"})
		}
	}
	if err := images.Save(ctx, episode.Ref.Provider, items); err != nil {
		return err
	}
	return nil
}
