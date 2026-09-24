package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func configureMediaRoots(t *testing.T) (string, string) {
	t.Helper()
	oldCollector := config.Collector
	t.Cleanup(func() { config.Collector = oldCollector })
	root := t.TempDir()
	movies, shows := filepath.Join(root, "movies"), filepath.Join(root, "shows")
	for _, dir := range []string{movies, shows} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"video.mkv", "theme.mp3", "video.nfo", "poster.jpg"} {
			if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	config.Collector = &config.CollectorConfig{RunMode: config.CollectorRunModeOnce, MoviesDir: []string{movies}, ShowsDir: []string{shows}}
	return filepath.Join(movies, "video.mkv"), filepath.Join(shows, "video.mkv")
}

func TestMovieAndShowScanCompletes(t *testing.T) {
	for _, mode := range []string{"all", "movie_directory", "show_directory"} {
		t.Run(mode, func(t *testing.T) {
			movie, show := configureMediaRoots(t)
			want := map[string]media_file.VideoType{movie: media_file.Movies, show: media_file.TvShows}
			if mode != "all" {
				config.Collector.RunMode = config.CollectorRunModeSpec
				if mode == "movie_directory" {
					t.Chdir(filepath.Dir(movie))
					delete(want, show)
				} else {
					t.Chdir(filepath.Dir(show))
					delete(want, movie)
				}
			}
			c := &collector{channel: make(chan *scanTask, 4)}
			go c.runScan()
			timeout := time.NewTimer(3 * time.Second)
			defer timeout.Stop()
			for {
				select {
				case task, ok := <-c.channel:
					if !ok {
						if len(want) != 0 {
							t.Fatalf("扫描遗漏电影或剧集：%v", want)
						}
						return
					}
					if kind, exists := want[task.file.Path]; !exists || task.file.VideoType != kind {
						t.Errorf("错误的扫描任务：%+v", task.file)
					}
					delete(want, task.file.Path)
					task.done.Done()
				case <-timeout.C:
					t.Fatal("扫描完成后未关闭任务队列")
				}
			}
		})
	}
}

func TestMovieAndShowWatcherDispatch(t *testing.T) {
	movie, show := configureMediaRoots(t)
	c := &collector{channel: make(chan *scanTask, 4)}
	for _, tc := range []struct {
		file string
		kind media_file.VideoType
	}{{movie, media_file.Movies}, {show, media_file.TvShows}} {
		fi, err := os.Stat(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		c.watcherCallback(tc.file, fi)
		select {
		case task := <-c.channel:
			if task.file.Path != tc.file || task.file.VideoType != tc.kind || task.file.TaskType != media_file.TaskWatcher {
				t.Fatalf("错误的监听任务：%+v", task.file)
			}
		default:
			t.Fatal("监听遗漏电影或剧集")
		}
	}
	audio := filepath.Join(filepath.Dir(movie), "theme.mp3")
	fi, err := os.Stat(audio)
	if err != nil {
		t.Fatal(err)
	}
	c.watcherCallback(audio, fi)
	if len(c.channel) != 0 {
		t.Fatal("音频附件不应进入视频处理队列")
	}
}
