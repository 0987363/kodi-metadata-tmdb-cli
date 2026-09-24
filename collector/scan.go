package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// collector 运行扫描
func (c *collector) runScan() {
	producerWG := &sync.WaitGroup{}
	scanTaskWG := &sync.WaitGroup{}

	if config.Collector.RunMode == config.CollectorRunModeSpec {
		pwd, err := os.Getwd()
		if err != nil {
			utils.Logger.ErrorF("get pwd error: %s", err)
			return
		}

		if root, _ := configuredMediaRoot(pwd); root != "" {
			roots := append([]string{pwd}, config.Collector.MoviesDir...)
			roots = append(roots, config.Collector.ShowsDir...)
			scheduled := make(map[string]bool)
			for _, candidate := range roots {
				path, err := filepath.Abs(candidate)
				if err != nil {
					continue
				}
				path = filepath.Clean(path)
				relative, err := filepath.Rel(pwd, path)
				if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || scheduled[path] {
					continue
				}
				// 当前目录和其配置子根分别负责各自范围，同一路径只调度一次。
				scheduled[path] = true
				_, videoType := configuredMediaRoot(path)
				producerWG.Add(1)
				go c.scanDir([]string{path}, videoType, producerWG, scanTaskWG)
			}
		}
	} else {
		producerWG.Add(2)
		go c.scanDir(config.Collector.MoviesDir, media_file.Movies, producerWG, scanTaskWG)
		go c.scanDir(config.Collector.ShowsDir, media_file.TvShows, producerWG, scanTaskWG)
	}

	producerWG.Wait()
	scanTaskWG.Wait()

	// 单次模式，关闭channel
	if config.Collector.RunMode == config.CollectorRunModeOnce || config.Collector.RunMode == config.CollectorRunModeSpec {
		c.closeOnce.Do(func() { close(c.channel) })
	}
}

// runCronScan 运行定时扫描
func (c *collector) runCronScan() {
	if !config.Collector.CronScan || config.Collector.CronSeconds <= 0 {
		return
	}

	ticker := time.NewTicker(time.Second * time.Duration(config.Collector.CronSeconds))
	defer ticker.Stop()
	for range ticker.C {
		c.runScan()
	}
}

// registerWatcherDirs 仅注册 watcher 目录，不产生扫描任务
// watcher 目录原本只在扫描时注册，定时扫描和启动扫描都关闭时需在此注册，否则 watcher 失效
func (c *collector) registerWatcherDirs() {
	c.walkMediaDir(config.Collector.MoviesDir, media_file.Movies, nil)
	c.walkMediaDir(config.Collector.ShowsDir, media_file.TvShows, nil)
}

// scanDir 扫描目录
func (c *collector) scanDir(roots []string, videoType media_file.VideoType, producerWG, scanTaskWG *sync.WaitGroup) {
	defer producerWG.Done()

	c.walkMediaDir(roots, videoType, func(mf *media_file.MediaFile) {
		scanTaskWG.Add(1)
		c.channel <- &scanTask{file: mf, done: scanTaskWG}
	})
}

// walkMediaDir 遍历媒体目录，统一处理隐藏目录、skip_folders、watcher 注册和原盘目录识别
// emit 接收发现的原盘目录和视频文件，nil 表示只注册watcher不投递
func (c *collector) walkMediaDir(roots []string, videoType media_file.VideoType, emit func(mf *media_file.MediaFile)) {
	for _, root := range roots {
		mediaRoot, _ := configuredMediaRoot(root)
		if mediaRoot != "" && insideDiscDirectory(root, mediaRoot) {
			continue
		}
		if f, err := os.Stat(root); err != nil || !f.IsDir() {
			utils.Logger.WarningF("%s is not a directory", root)
			continue
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// 隐藏文件跳过自身，隐藏目录跳过整个子树
			// 文件不能返回SkipDir，否则会跳过同目录剩余文件
			if d.Name()[0:1] == "." {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			if d.IsDir() {
				// 嵌套媒体根由自身扫描负责，外层根不重复派发。
				if owner, _ := configuredMediaRoot(path); owner != mediaRoot {
					return fs.SkipDir
				}
			}

			mf := media_file.NewMediaFile(path, d.Name(), videoType)
			disc := d.IsDir() && (mf.IsBluRay() || mf.IsDvd())
			if disc && c.shouldSkipDisc(mf) {
				return fs.SkipDir
			}

			if d.IsDir() {
				if c.skipFolders(path, d.Name()) {
					utils.Logger.DebugF("skip folder by config: %s", d.Name())
					return fs.SkipDir
				}
				c.watcher.Add(path)
			}

			if !d.IsDir() && c.shouldSkipFile(d.Name()) {
				return nil
			}

			// 原盘只派发目录本身，不下探内部流文件。
			if disc {
				if emit != nil {
					emit(mf)
				}
				return fs.SkipDir
			}

			if emit != nil && mf.IsVideo() {
				emit(mf)
			}

			return nil
		})

		if err != nil {
			utils.Logger.WarningF("walk dir %s error: %s", root, err)
		}
	}
}

// skipFolders 检查是否跳过目录
func (c *collector) skipFolders(path, filename string) bool {
	base := filepath.Base(path)
	return slices.Contains(config.Collector.SkipFolders, base) ||
		slices.Contains(config.Collector.SkipFolders, filename)
}
