package movies

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMovieWithPaths_RealMedia(t *testing.T) {
	oldCollector := config.Collector
	t.Cleanup(func() { config.Collector = oldCollector })
	config.Collector = &config.CollectorConfig{RunMode: config.CollectorRunModeOnce}
	for _, tc := range []struct {
		name, path, poster, fanart, logo, nfo string
		disc                                  bool
	}{
		{"video", "Inception.2010.mkv", "Inception.2010-poster.jpg", "Inception.2010-fanart.jpg", "Inception.2010-logo.png", "Inception.2010.nfo", false},
		{"bluray", "BDMV", "poster.jpg", "fanart.jpg", "clearlogo.png", "BDMV/index.nfo", true},
		{"dvd", "VIDEO_TS", "poster.jpg", "fanart.jpg", "clearlogo.png", "VIDEO_TS/VIDEO_TS.nfo", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "Movie")
			require.NoError(t, os.Mkdir(dir, 0755))
			path := filepath.Join(dir, tc.path)
			if tc.disc {
				require.NoError(t, os.Mkdir(path, 0755))
			} else {
				require.NoError(t, os.WriteFile(path, nil, 0644))
			}
			mf := media_file.NewMediaFile(path, filepath.Base(path), media_file.Movies)
			movie := newMovieWithPaths(mf)
			require.NotNil(t, movie)
			assert.Equal(t, filepath.Join(dir, tc.poster), movie.PosterFile)
			assert.Equal(t, filepath.Join(dir, tc.fanart), movie.FanArtFile)
			assert.Equal(t, filepath.Join(dir, tc.logo), movie.ClearLogoFile)
			wantNfo := []string{filepath.Join(dir, tc.nfo)}
			if tc.disc {
				wantNfo = []string{filepath.Join(dir, "movie.nfo"), filepath.Join(dir, tc.nfo)}
			}
			assert.Equal(t, wantNfo, movie.NfoFiles)
		})
	}
}
