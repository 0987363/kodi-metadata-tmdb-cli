package movies

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/nfo"
)

type Task struct {
	Request   metadata.Request
	CacheRoot string
	Input     []ai.Identity
	movie     *Movie
}

func (t *Task) OutputPaths() []string {
	if t == nil || t.movie == nil {
		return nil
	}
	paths := append([]string(nil), t.movie.NfoFiles...)
	return append(paths, t.movie.PosterFile, t.movie.FanArtFile, t.movie.ClearLogoFile)
}

func (t *Task) validateOutputPaths() error {
	seen := map[string]bool{}
	for _, path := range t.OutputPaths() {
		clean := filepath.Clean(path)
		if seen[clean] {
			return fmt.Errorf("电影输出目标路径重复：%s", clean)
		}
		seen[clean] = true
	}
	return nil
}

func Prepare(root string, mf *media_file.MediaFile, identity ai.Identity) (*Task, error) {
	if mf == nil || mf.Path == "" || mf.Filename == "" || identity.MediaType != "movie" || strings.TrimSpace(identity.Title) == "" || identity.RelativePath == "" {
		return nil, errors.New("电影输入或识别身份无效")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(mf.Path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("电影文件不在输入目录内")
	}
	if filepath.ToSlash(relative) != identity.RelativePath {
		return nil, errors.New("电影文件路径与识别身份不一致")
	}
	if identity.Season != nil || identity.Episode != nil || identity.TheTVDBID != "" || identity.SeriesRoot != "" {
		return nil, errors.New("电影识别身份含剧集字段")
	}
	movie := newMovieWithPaths(mf)
	cacheRoot := filepath.Join(mf.Dir, ".metadata")
	ref, err := metadata.LoadReference(filepath.Join(cacheRoot, mf.Filename+".source.json"), metadata.Movie)
	if err != nil {
		return nil, err
	}
	for _, file := range []string{movie.IdFile(), filepath.Join(movie.GetCacheDir(), "id.txt")} {
		id, err := metadata.ReadNumericOverride(file, false)
		if err != nil {
			return nil, err
		}
		if id != "" {
			ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: id})
			if err != nil {
				return nil, err
			}
		}
	}
	var hints []metadata.Ref
	if identity.TMDBID != "" {
		hints = append(hints, metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: identity.TMDBID})
	}
	request := metadata.Request{Kind: metadata.Movie, Ref: ref, Hints: hints, Path: mf.Path, Filename: mf.Filename, Query: metadata.Query{Kind: metadata.Movie, Title: identity.Title, ChineseTitle: identity.ChsTitle, OriginalTitle: identity.EngTitle, Year: identity.Year}, Files: []string{mf.Path}}
	return &Task{Request: request, CacheRoot: cacheRoot, Input: []ai.Identity{identity}, movie: movie}, nil
}

func (t *Task) Local(descriptions []ai.LocalDescription) (*metadata.Option, error) {
	if t == nil || len(t.Input) != 1 || len(descriptions) != 1 || descriptions[0].RelativePath != t.Input[0].RelativePath || strings.TrimSpace(descriptions[0].Title) == "" {
		return nil, errors.New("电影本地描述未完整对应输入")
	}
	d := descriptions[0]
	return &metadata.Option{Work: &metadata.Record{Ref: metadata.LocalRef(metadata.Movie, t.movie.MediaFile.Path), Title: d.Title, Plot: "依据目录和文件名整理：" + strings.TrimSpace(d.Plot), Genres: append([]string(nil), d.Genres...)}}, nil
}

func (t *Task) Write(ctx context.Context, selected *metadata.Option, images *artwork.Downloader) error {
	if t == nil || t.movie == nil || selected == nil || selected.Work == nil {
		return errors.New("电影输出缺少作品事实")
	}
	if err := t.validateOutputPaths(); err != nil {
		return err
	}
	r := selected.Work
	if r.Ref.Kind != metadata.Movie || r.Ref.Provider == "" || r.Ref.ID == "" || strings.TrimSpace(r.Title) == "" || len(selected.Episodes) != 0 {
		return errors.New("电影输出身份无效")
	}
	if t.Request.Ref.Provider != "" && (r.Ref.Provider != t.Request.Ref.Provider || t.Request.Ref.ID != "" && r.Ref.ID != t.Request.Ref.ID) {
		return errors.New("电影事实与人工来源指定不一致")
	}
	if r.Ref.Provider == "local" && r.Ref != metadata.LocalRef(metadata.Movie, t.movie.MediaFile.Path) {
		return errors.New("本地电影身份与文件不一致")
	}
	if config.Scraper == nil {
		return errors.New("刮削配置 scraper 未初始化")
	}
	for _, file := range t.movie.NfoFiles {
		if err := nfo.Write(file, r, "", config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
			return err
		}
	}
	var items []artwork.Item
	for _, a := range r.Artwork {
		var dest string
		switch a.Kind {
		case "poster":
			dest = t.movie.PosterFile
		case "fanart":
			dest = t.movie.FanArtFile
		case "clearlogo":
			dest = t.movie.ClearLogoFile
		}
		if dest != "" {
			items = append(items, artwork.Item{URL: a.URL, Path: dest})
		}
	}
	return images.Save(ctx, r.Ref.Provider, items)
}
