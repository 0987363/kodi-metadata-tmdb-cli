package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectorFiltersDuringScanAndWatch(t *testing.T) {
	movie, _ := configureMediaRoots(t)
	dir := filepath.Dir(movie)
	config.Collector.SkipKeywords = []string{"纯享"}
	config.Collector.TmpSuffix = []string{".preview.mkv"}
	c := &collector{channel: make(chan *scanTask, 10)}
	for _, name := range []string{"纯享.mkv", "clip.preview.mkv", "unfinished.part"} {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		c.watcherCallback(file, info)
	}
	if len(c.channel) != 0 {
		t.Error("监听未排除关键词或临时文件")
	}
	var found []string
	c.walkMediaDir([]string{dir}, media_file.Movies, func(mf *media_file.MediaFile) { found = append(found, mf.Filename) })
	if len(found) != 1 || found[0] != "video.mkv" {
		t.Errorf("扫描过滤错误：%v", found)
	}
}
