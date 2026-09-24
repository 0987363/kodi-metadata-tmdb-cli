package movies

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"path/filepath"
	"strings"
)

func parseMoviesFileContext(ctx context.Context, mf *media_file.MediaFile, extractor *ai.Client) (*Movie, error) {
	if mf == nil {
		return nil, errors.New("电影文件为空")
	}
	if extractor == nil {
		return nil, errors.New("LLM 提取器未初始化")
	}
	result, err := extractor.ParseMediaContext(ctx, &ai.ParseInput{MediaType: "movie", Path: mf.Path, Filename: mf.Filename})
	if err != nil {
		return nil, err
	}
	movie := newMovieWithPaths(mf)
	movie.Title = result.Title
	movie.AliasTitle = result.AliasTitle
	movie.ChsTitle = result.ChsTitle
	movie.EngTitle = result.EngTitle
	movie.Year = result.Year
	movie.TMDBID = result.TMDBID
	movie.TheTVDBID = result.TheTVDBID
	return movie, nil
}
func newMovieWithPaths(mf *media_file.MediaFile) *Movie {
	movie := &Movie{MediaFile: mf}
	if mf.IsDisc() {
		movie.PosterFile = mf.Dir + "/poster.jpg"
		movie.FanArtFile = mf.Dir + "/fanart.jpg"
		movie.ClearLogoFile = mf.Dir + "/clearlogo.png"
		if mf.IsBluRay() {
			movie.NfoFiles = []string{filepath.Join(mf.Dir, "movie.nfo"), filepath.Join(mf.Path, "index.nfo")}
		} else if mf.IsDvd() {
			movie.NfoFiles = []string{filepath.Join(mf.Dir, "movie.nfo"), filepath.Join(mf.Path, "VIDEO_TS.nfo")}
		}
		return movie
	}

	prefix := mf.Dir + "/" + strings.TrimSuffix(mf.Filename, mf.Suffix)
	movie.PosterFile = prefix + "-poster.jpg"
	movie.FanArtFile = prefix + "-fanart.jpg"
	movie.ClearLogoFile = prefix + "-logo.png"
	movie.NfoFiles = []string{prefix + ".nfo"}
	return movie
}
