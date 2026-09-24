package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/utils"
)

// Run 只处理本次目录快照，分类和任务完成后退出，不保留监听或后台扫描。
func Run(ctx context.Context, path string, extractor *ai.Client, manager *metadata.Manager, images *artwork.Downloader) (runErr error) {
	defer func() {
		if manager != nil && utils.Logger != nil {
			if encoded, err := json.Marshal(manager.Statistics()); err == nil {
				utils.Logger.InfoF("来源请求统计：%s", encoded)
			}
		}
		if runErr != nil && utils.Logger != nil {
			utils.Logger.Error(redactRunError(runErr))
		}
	}()
	if extractor == nil || manager == nil {
		return errors.New("缺少提取器或元数据管理器")
	}
	discovery, err := Discover(path)
	if err != nil {
		return fmt.Errorf("发现媒体: %w", err)
	}
	if utils.Logger != nil {
		utils.Logger.InfoF("扫描完成：目录=%s 媒体=%d 排除项=%d", discovery.Root, len(discovery.Files), discovery.Excluded)
	}
	if len(discovery.Files) == 0 {
		return nil
	}
	input, files, err := classificationInput(discovery)
	if err != nil {
		return err
	}
	identities, err := extractor.Analyze(ctx, input)
	if err != nil {
		return fmt.Errorf("批量分类: %w", err)
	}
	tasks, err := prepareTasks(discovery.Root, files, identities)
	if err != nil {
		return fmt.Errorf("组织作品任务: %w", err)
	}
	var failures []error
	var unmatched []preparedTask
	succeeded := 0
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		selected, err := manager.Resolve(ctx, task.request, task.cacheRoot)
		if errors.Is(err, metadata.ErrNoMatch) {
			unmatched = append(unmatched, task)
			continue
		}
		if err == nil {
			err = task.write(ctx, selected, images)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("作品 %s: %w", task.request.Path, err))
			continue
		}
		succeeded += len(task.input)
		if utils.Logger != nil {
			utils.Logger.InfoF("网站编目完成：作品=%s 来源=%s 视频=%d", task.request.Path, selected.Work.Ref.Provider, len(task.input))
		}
	}
	if len(unmatched) > 0 {
		localInput := ai.BatchInput{Root: discovery.Root}
		for _, task := range unmatched {
			for _, identity := range task.input {
				localInput.Files = append(localInput.Files, ai.BatchFile{RelativePath: identity.RelativePath})
			}
		}
		descriptions, err := extractor.DescribeLocal(ctx, localInput)
		if err != nil {
			failures = append(failures, fmt.Errorf("本地整理: %w", err))
		} else {
			byPath := make(map[string]ai.LocalDescription, len(descriptions))
			for _, description := range descriptions {
				byPath[description.RelativePath] = description
			}
			for _, task := range unmatched {
				var own []ai.LocalDescription
				for _, identity := range task.input {
					own = append(own, byPath[identity.RelativePath])
				}
				selected, err := task.local(own)
				if err == nil {
					err = task.write(ctx, selected, images)
				}
				if err != nil {
					failures = append(failures, fmt.Errorf("本地作品 %s: %w", task.request.Path, err))
					continue
				}
				succeeded += len(task.input)
				if utils.Logger != nil {
					utils.Logger.InfoF("本地编目完成：作品=%s 视频=%d", task.request.Path, len(task.input))
				}
			}
		}
	}
	if utils.Logger != nil {
		stats := extractor.Stats()
		utils.Logger.InfoF("处理结束：媒体=%d 成功=%d 失败=%d 提取请求=%d 本地整理请求=%d", len(discovery.Files), succeeded, len(discovery.Files)-succeeded, stats.AnalyzeRequests, stats.LocalRequests)
	}
	return errors.Join(failures...)
}

func redactRunError(err error) string {
	message := err.Error()
	for _, model := range config.LLMs {
		if model.ApiKey != "" {
			message = strings.ReplaceAll(message, model.ApiKey, "[已隐藏]")
		}
	}
	if config.Tmdb != nil && config.Tmdb.ApiKey != "" {
		message = strings.ReplaceAll(message, config.Tmdb.ApiKey, "[已隐藏]")
	}
	return message
}
