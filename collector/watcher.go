package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"os"
)

// watcherCallback 监听文件变化的回调函数
// todo 部分逻辑和scanDir重复，考虑复用
func (c *collector) watcherCallback(filename string, fi os.FileInfo) {
	root, videoType := configuredMediaRoot(filename)
	if root == "" || fi.Name()[0:1] == "." || insideDiscDirectory(filename, root) {
		return
	}

	mf := media_file.NewMediaFile(filename, fi.Name(), videoType)
	disc := fi.IsDir() && (mf.IsBluRay() || mf.IsDvd())
	if disc && c.shouldSkipDisc(mf) {
		return
	}

	if fi.IsDir() {
		if c.skipFolders(filename, fi.Name()) {
			utils.Logger.DebugF("skip folder by config: %s", fi.Name())
			return
		}

		c.watcher.Add(filename)
	}

	if !fi.IsDir() && c.shouldSkipFile(fi.Name()) {
		return
	}

	mf.TaskType = media_file.TaskWatcher
	if disc || mf.IsVideo() {
		c.channel <- &scanTask{file: mf}
	}
}
