package shows

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadGroupId_SeasonRootFirst(t *testing.T) {
	showsDir := filepath.Join(t.TempDir(), "shows")
	showRoot := filepath.Join(showsDir, "Money.Heist")
	seasonRoot := filepath.Join(showRoot, "Season 02")
	require.NoError(t, os.MkdirAll(filepath.Join(seasonRoot, "tmdb"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(showRoot, "tmdb"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(seasonRoot, "tmdb", "group.txt"), []byte("season-group"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(showRoot, "tmdb", "group.txt"), []byte("tv-root-group"), 0644))

	mf := &media_file.MediaFile{
		Path:      filepath.Join(seasonRoot, "episode-a.mkv"),
		Dir:       seasonRoot,
		Filename:  "episode-a.mkv",
		Suffix:    ".mkv",
		MediaType: media_file.VIDEO,
		VideoType: media_file.TvShows,
	}

	show := &Show{MediaFile: mf, TvRoot: showRoot, SeasonRoot: seasonRoot}
	show.ReadGroupId()
	assert.Equal(t, "season-group", show.GroupId)
}

func TestReadGroupId_FallbackTvRoot(t *testing.T) {
	showsDir := filepath.Join(t.TempDir(), "shows")
	showRoot := filepath.Join(showsDir, "Money.Heist")
	seasonRoot := filepath.Join(showRoot, "Season 02")
	require.NoError(t, os.MkdirAll(filepath.Join(seasonRoot, "tmdb"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(showRoot, "tmdb"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(showRoot, "tmdb", "group.txt"), []byte(" 5eb7353b0cb3350020ce402e \r\n"), 0644))

	mf := &media_file.MediaFile{
		Path:      filepath.Join(seasonRoot, "episode-a.mkv"),
		Dir:       seasonRoot,
		Filename:  "episode-a.mkv",
		Suffix:    ".mkv",
		MediaType: media_file.VIDEO,
		VideoType: media_file.TvShows,
	}

	show := &Show{MediaFile: mf, TvRoot: showRoot, SeasonRoot: seasonRoot}
	show.ReadGroupId()
	assert.Equal(t, "5eb7353b0cb3350020ce402e", show.GroupId)
}

// buildGroupDetail 构造测试用的剧集分组缓存：
// 组1(order=1)有13集，组2(order=2)有9集，组内剧集指向原始编号（模拟纸房子 Netflix 分组）

func TestRelativeLibraryRootKeepsAbsoluteShowRoot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	old := config.Collector
	t.Cleanup(func() { config.Collector = old })
	config.Collector = &config.CollectorConfig{ShowsDir: []string{"."}}
	showRoot := filepath.Join(root, "Example")
	seasonRoot := filepath.Join(showRoot, "Season 01")
	require.NoError(t, os.MkdirAll(filepath.Join(showRoot, "tmdb"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(showRoot, "tmdb", "id.txt"), []byte("42"), 0644))
	show := &Show{MediaFile: &media_file.MediaFile{Path: filepath.Join(seasonRoot, "Example.S01E02.mkv")}}
	require.NoError(t, fillShowPathMeta(show))
	require.Equal(t, showRoot, show.TvRoot)
	require.Equal(t, seasonRoot, show.SeasonRoot)
	require.NoError(t, show.readMetadataOverrides())
	require.Equal(t, 42, show.TvId)
}
