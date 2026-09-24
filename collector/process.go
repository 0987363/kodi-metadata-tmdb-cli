package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/movies"
	"fengqi/kodi-metadata-tmdb-cli/shows"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"fmt"
)

func (c *collector) runProcess() error {
	var lastError error
	for task := range c.channel {
		if task == nil {
			continue
		}
		if task.file != nil {
			var err error
			switch task.file.VideoType {
			case media_file.Movies:
				err = movies.ProcessMetadata(c.ctx, task.file, c.extractor, c.metadata, c.images)
			case media_file.TvShows:
				err = shows.ProcessMetadata(c.ctx, task.file, c.extractor, c.metadata, c.images)
			}
			if err != nil {
				lastError = fmt.Errorf("处理 %s: %w", task.file.Path, err)
				c.failures.Add(1)
				utils.Logger.Error(lastError)
			}
		}
		if task.done != nil {
			task.done.Done()
		}
	}
	return lastError
}
