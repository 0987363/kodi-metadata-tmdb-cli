package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func discFixture(t *testing.T, movieName, discName string) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	disc := filepath.Join(root, movieName, discName)
	stream := filepath.Join(disc, "STREAM")
	require.NoError(t, os.MkdirAll(stream, 0755))
	paths := []string{disc, stream}
	for _, name := range []string{"VIDEO_TS.IFO", "VTS_01_1.VOB", "00001.m2ts", "clip.mp4"} {
		path := filepath.Join(stream, name)
		require.NoError(t, os.WriteFile(path, nil, 0644))
		paths = append(paths, path)
	}
	oldCollector, oldLog, oldLogger := config.Collector, config.Log, utils.Logger
	t.Cleanup(func() { config.Collector, config.Log, utils.Logger = oldCollector, oldLog, oldLogger })
	config.Collector = &config.CollectorConfig{RunMode: config.CollectorRunModeOnce, MoviesDir: []string{root}}
	config.Log = &config.LogConfig{Mode: config.LogModeStdout, Level: config.LogLevelError}
	utils.InitLogger()
	return root, disc, paths
}

func TestDiscScanAndWatchDispatchOneMovie(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(discName, func(t *testing.T) {
			root, disc, paths := discFixture(t, "Movie", discName)
			c := &collector{channel: make(chan *scanTask, 10)}
			var found []*media_file.MediaFile
			c.walkMediaDir([]string{root}, media_file.Movies, func(mf *media_file.MediaFile) { found = append(found, mf) })
			if len(found) != 1 || found[0].Path != disc || found[0].VideoType != media_file.Movies {
				t.Errorf("原盘扫描必须仅派发原盘目录一次：%+v", found)
			}
			for _, path := range paths {
				info, err := os.Stat(path)
				require.NoError(t, err)
				c.watcherCallback(path, info)
			}
			if len(c.channel) != 1 {
				t.Fatalf("原盘监听必须仅派发原盘目录一次，实际 %d 个任务", len(c.channel))
			}
			task := <-c.channel
			if task.file.Path != disc || task.file.TaskType != media_file.TaskWatcher || task.file.VideoType != media_file.Movies {
				t.Fatalf("原盘监听派发错误：%+v", task.file)
			}
		})
	}
}

func TestDiscEntriesRespectFilters(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		for _, filter := range []string{"keyword", "temporary", "skip_folder"} {
			t.Run(discName+"/"+filter, func(t *testing.T) {
				root, disc, paths := discFixture(t, "Excluded.preview", discName)
				switch filter {
				case "keyword":
					config.Collector.SkipKeywords = []string{"Excluded"}
				case "temporary":
					config.Collector.TmpSuffix = []string{".preview"}
				case "skip_folder":
					config.Collector.SkipFolders = []string{discName}
				}
				c := &collector{channel: make(chan *scanTask, 10)}
				c.walkMediaDir([]string{root}, media_file.Movies, func(mf *media_file.MediaFile) { c.channel <- &scanTask{file: mf} })
				for _, path := range paths {
					info, err := os.Stat(path)
					require.NoError(t, err)
					c.watcherCallback(path, info)
				}
				if len(c.channel) != 0 {
					t.Fatalf("排除的原盘 %s 不应派发任务，实际 %d 个", disc, len(c.channel))
				}
			})
		}
	}
}

func TestDiscAncestorOutsideConfiguredRootDoesNotHideMedia(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(discName, func(t *testing.T) {
			_, disc, _ := discFixture(t, "Container", discName)
			library := filepath.Join(disc, "library")
			require.NoError(t, os.Mkdir(library, 0755))
			file := filepath.Join(library, "Movie.mkv")
			require.NoError(t, os.WriteFile(file, nil, 0644))
			config.Collector.MoviesDir = []string{filepath.Dir(disc), library}
			c := &collector{channel: make(chan *scanTask, 4)}
			var found []string
			c.walkMediaDir([]string{library}, media_file.Movies, func(mf *media_file.MediaFile) { found = append(found, mf.Path) })
			require.Equal(t, []string{file}, found, "配置根以外的同名目录不能改变媒体实体")
			info, err := os.Stat(file)
			require.NoError(t, err)
			c.watcherCallback(file, info)
			require.Len(t, c.channel, 1, "监听必须以最长匹配的配置媒体根为范围")
			task := <-c.channel
			require.Equal(t, file, task.file.Path)
			require.Equal(t, media_file.Movies, task.file.VideoType)
		})
	}
}

func TestConfiguredDiscRootAndInteriorScan(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(discName, func(t *testing.T) {
			root, disc, paths := discFixture(t, "Movie", discName)
			config.Collector.MoviesDir = []string{disc}
			c := &collector{channel: make(chan *scanTask, 10)}
			var found []string
			c.walkMediaDir([]string{disc}, media_file.Movies, func(mf *media_file.MediaFile) { found = append(found, mf.Path) })
			require.Equal(t, []string{disc}, found)
			for _, path := range paths {
				info, err := os.Stat(path)
				require.NoError(t, err)
				c.watcherCallback(path, info)
			}
			require.Len(t, c.channel, 1)
			require.Equal(t, disc, (<-c.channel).file.Path)

			config.Collector.RunMode = config.CollectorRunModeSpec
			t.Chdir(filepath.Join(disc, "STREAM"))
			for _, mediaRoot := range []string{root, disc} {
				config.Collector.MoviesDir = []string{mediaRoot}
				c := &collector{channel: make(chan *scanTask, 10)}
				go c.runScan()
				for task := range c.channel {
					t.Errorf("指定目录扫描不得下探原盘内部：%s", task.file.Path)
					task.done.Done()
				}
			}
		})
	}
}

func TestWatcherMediaRootsUseDirectoryComponents(t *testing.T) {
	movie, show := configureMediaRoots(t)
	movieRoot, showRoot := filepath.Dir(movie), filepath.Dir(show)
	for _, tc := range []struct {
		name string
		file string
		kind media_file.VideoType
	}{
		{"movie", movie, media_file.Movies},
		{"show", show, media_file.TvShows},
		{"same_prefix", filepath.Join(movieRoot+"-extra", "Movie.mkv"), 0},
		{"nested_show", filepath.Join(movieRoot, "nested-shows", "Episode.mkv"), media_file.TvShows},
		{"nested_movie", filepath.Join(showRoot, "nested-movies", "Movie.mkv"), media_file.Movies},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.MkdirAll(filepath.Dir(tc.file), 0755))
			require.NoError(t, os.WriteFile(tc.file, nil, 0644))
			config.Collector.MoviesDir = []string{movieRoot, filepath.Join(showRoot, "nested-movies")}
			config.Collector.ShowsDir = []string{showRoot, filepath.Join(movieRoot, "nested-shows")}
			info, err := os.Stat(tc.file)
			require.NoError(t, err)
			c := &collector{channel: make(chan *scanTask, 2)}
			c.watcherCallback(tc.file, info)
			if tc.kind == 0 {
				require.Empty(t, c.channel, "不属于配置媒体根的同名前缀路径不能派发")
				return
			}
			require.Len(t, c.channel, 1)
			require.Equal(t, tc.kind, (<-c.channel).file.VideoType)
		})
	}
}

func TestFullScanNestedRootsEmitOnlyOwner(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, child media_file.VideoType
	}{
		{"movie_in_movies", media_file.Movies, media_file.Movies},
		{"show_in_movies", media_file.Movies, media_file.TvShows},
		{"movie_in_shows", media_file.TvShows, media_file.Movies},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie, show := configureMediaRoots(t)
			parent := filepath.Dir(movie)
			if tc.parent == media_file.TvShows {
				parent = filepath.Dir(show)
			}
			nested := filepath.Join(parent, "nested")
			require.NoError(t, os.Mkdir(nested, 0755))
			file := filepath.Join(nested, "Nested.mkv")
			require.NoError(t, os.WriteFile(file, nil, 0644))
			if tc.child == media_file.Movies {
				config.Collector.MoviesDir = append(config.Collector.MoviesDir, nested)
			} else {
				config.Collector.ShowsDir = append(config.Collector.ShowsDir, nested)
			}
			c := &collector{channel: make(chan *scanTask, 10)}
			go c.runScan()
			found := make(map[string][]media_file.VideoType)
			for task := range c.channel {
				found[task.file.Path] = append(found[task.file.Path], task.file.VideoType)
				task.done.Done()
			}
			require.Equal(t, map[string][]media_file.VideoType{
				movie: {media_file.Movies}, show: {media_file.TvShows}, file: {tc.child},
			}, found, "嵌套媒体目录应只由所属配置根派发一次")
		})
	}
}

func TestRelativeMediaRootsWatchAndSpec(t *testing.T) {
	for _, mode := range []string{"watch_absolute", "watch_relative", "spec", "full_scan"} {
		t.Run(mode, func(t *testing.T) {
			movie, show := configureMediaRoots(t)
			base := filepath.Dir(filepath.Dir(movie))
			t.Chdir(base)
			config.Collector.MoviesDir = []string{"movies"}
			config.Collector.ShowsDir = []string{"shows"}
			c := &collector{channel: make(chan *scanTask, 4)}
			if mode == "watch_absolute" || mode == "watch_relative" {
				path := movie
				if mode == "watch_relative" {
					path = filepath.Join("movies", "video.mkv")
				}
				info, err := os.Stat(path)
				require.NoError(t, err)
				c.watcherCallback(path, info)
				require.Len(t, c.channel, 1, "相对配置根必须匹配实际监听路径")
				task := <-c.channel
				require.Equal(t, path, task.file.Path, "归属比较不得改写输出路径形式")
				require.Equal(t, media_file.Movies, task.file.VideoType)
				return
			}
			want := map[string]media_file.VideoType{
				filepath.Join("movies", filepath.Base(movie)): media_file.Movies,
				filepath.Join("shows", filepath.Base(show)):   media_file.TvShows,
			}
			if mode == "spec" {
				t.Chdir(filepath.Dir(movie))
				config.Collector.RunMode = config.CollectorRunModeSpec
				config.Collector.MoviesDir = []string{"../movies"}
				config.Collector.ShowsDir = []string{"../shows"}
				want = map[string]media_file.VideoType{movie: media_file.Movies}
			}
			go c.runScan()
			found := make(map[string]media_file.VideoType)
			for task := range c.channel {
				found[task.file.Path] = task.file.VideoType
				task.done.Done()
			}
			require.Equal(t, want, found, "扫描应正确解析相对根且保持输出路径形式")
		})
	}
}

func TestRelativeMediaRootIgnoresOutsideDiscAncestor(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(discName, func(t *testing.T) {
			root, disc, _ := discFixture(t, "Container", discName)
			library := filepath.Join(disc, "library")
			require.NoError(t, os.Mkdir(library, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(library, "Movie.mkv"), nil, 0644))
			t.Chdir(root)
			relativeRoot := filepath.Join("Container", discName, "library")
			config.Collector.MoviesDir = []string{relativeRoot}
			c := &collector{channel: make(chan *scanTask, 2)}
			var found []string
			c.walkMediaDir(config.Collector.MoviesDir, media_file.Movies, func(mf *media_file.MediaFile) { found = append(found, mf.Path) })
			require.Equal(t, []string{filepath.Join(relativeRoot, "Movie.mkv")}, found)
		})
	}
}

func TestSpecScanIncludesNestedRootsExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind media_file.VideoType
	}{
		{"movie", media_file.Movies},
		{"show", media_file.TvShows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			childKind := tc.kind
			movie, show := configureMediaRoots(t)
			current := filepath.Join(filepath.Dir(movie), "current")
			nested := filepath.Join(current, "nested")
			require.NoError(t, os.MkdirAll(nested, 0755))
			local := filepath.Join(current, "Local.mkv")
			child := filepath.Join(nested, "Nested.mkv")
			for _, path := range []string{local, child} {
				require.NoError(t, os.WriteFile(path, nil, 0644))
			}
			t.Chdir(current)
			config.Collector.RunMode = config.CollectorRunModeSpec
			if childKind == media_file.Movies {
				config.Collector.MoviesDir = append(config.Collector.MoviesDir, nested, "nested/../nested")
			} else {
				config.Collector.ShowsDir = append(config.Collector.ShowsDir, nested, "nested/../nested")
			}
			c := &collector{channel: make(chan *scanTask, 10)}
			go c.runScan()
			found := make(map[string][]media_file.VideoType)
			for task := range c.channel {
				found[task.file.Path] = append(found[task.file.Path], task.file.VideoType)
				task.done.Done()
			}
			require.Equal(t, map[string][]media_file.VideoType{
				local: {media_file.Movies}, child: {childKind},
			}, found, "当前目录的配置子根不能遗漏或重复，目录外电影和节目不能被扫描")
			_, movieOutside := found[movie]
			_, showOutside := found[show]
			require.False(t, movieOutside || showOutside)
		})
	}
}

func TestSpecScanOutsideMediaRootDoesNotScanDescendantLibraries(t *testing.T) {
	movie, _ := configureMediaRoots(t)
	t.Chdir(filepath.Dir(filepath.Dir(movie)))
	config.Collector.RunMode = config.CollectorRunModeSpec
	c := &collector{channel: make(chan *scanTask, 10)}
	go c.runScan()
	for task := range c.channel {
		t.Errorf("未归属媒体根的当前目录不得扩大到子媒体库：%s", task.file.Path)
		task.done.Done()
	}
}
