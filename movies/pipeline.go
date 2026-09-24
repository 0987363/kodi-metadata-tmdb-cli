package movies

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/nfo"
	"path/filepath"
)

func ProcessMetadata(ctx context.Context, mf *media_file.MediaFile, extractor *ai.Client, manager *metadata.Manager, images *artwork.Downloader) error {
	if mf == nil {
		return errors.New("电影文件为空")
	}
	if config.Scraper == nil {
		return errors.New("刮削配置 scraper 未初始化")
	}
	movie := newMovieWithPaths(mf)
	root := filepath.Join(mf.Dir, ".metadata")
	ref, err := metadata.LoadReference(filepath.Join(root, mf.Filename+".source.json"), metadata.Movie)
	if err != nil {
		return err
	}
	for _, file := range []string{movie.IdFile(), filepath.Join(movie.GetCacheDir(), "id.txt")} {
		id, err := metadata.ReadNumericOverride(file, false)
		if err != nil {
			return err
		}
		if id != "" {
			ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: id})
			if err != nil {
				return err
			}
		}
	}
	movie, err = parseMoviesFileContext(ctx, mf, extractor)
	if err != nil {
		return err
	}
	var hints []metadata.Ref
	if movie.TMDBID != "" {
		hints = append(hints, metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: movie.TMDBID})
	}
	if movie.TheTVDBID != "" {
		hints = append(hints, metadata.Ref{Provider: "thetvdb", Kind: metadata.Movie, ID: movie.TheTVDBID})
	}
	selected, err := manager.Resolve(ctx, metadata.Request{Kind: metadata.Movie, Ref: ref, Hints: hints, Path: mf.Path, Filename: mf.Filename, Query: metadata.Query{Kind: metadata.Movie, Title: movie.Title, ChineseTitle: movie.ChsTitle, OriginalTitle: movie.EngTitle, Year: movie.Year}}, root)
	if err != nil {
		return err
	}
	detail := selected.Work
	for _, file := range movie.NfoFiles {
		if err := nfo.Write(file, detail, "", config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
			return err
		}
	}
	var items []artwork.Item
	for _, a := range detail.Artwork {
		dest := ""
		switch a.Kind {
		case "poster":
			dest = movie.PosterFile
		case "fanart":
			dest = movie.FanArtFile
		case "clearlogo":
			dest = movie.ClearLogoFile
		}
		if dest != "" {
			items = append(items, artwork.Item{URL: a.URL, Path: dest})
		}
	}
	if err := images.Save(ctx, detail.Ref.Provider, items); err != nil {
		return err
	}
	return nil
}
