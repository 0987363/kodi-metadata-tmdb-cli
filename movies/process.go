package movies

import (
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"path/filepath"
	"strings"
)

func newMovieWithPaths(mf *media_file.MediaFile) *Movie {
	movie := &Movie{MediaFile: mf}
	if mf.IsDisc() {
		movie.PosterFile = filepath.Join(mf.Dir, "poster.jpg")
		movie.FanArtFile = filepath.Join(mf.Dir, "fanart.jpg")
		movie.ClearLogoFile = filepath.Join(mf.Dir, "clearlogo.png")
		if mf.IsBluRay() {
			movie.NfoFiles = []string{filepath.Join(mf.Dir, "movie.nfo"), filepath.Join(mf.Path, "index.nfo")}
		}
		if mf.IsDvd() {
			movie.NfoFiles = []string{filepath.Join(mf.Dir, "movie.nfo"), filepath.Join(mf.Path, "VIDEO_TS.nfo")}
		}
		return movie
	}
	prefix := filepath.Join(mf.Dir, strings.TrimSuffix(mf.Filename, mf.Suffix))
	movie.PosterFile = prefix + "-poster.jpg"
	movie.FanArtFile = prefix + "-fanart.jpg"
	movie.ClearLogoFile = prefix + "-logo.png"
	movie.NfoFiles = []string{prefix + ".nfo"}
	return movie
}
