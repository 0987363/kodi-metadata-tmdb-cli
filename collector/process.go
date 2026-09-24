package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/movies"
	"fengqi/kodi-metadata-tmdb-cli/shows"
)

type preparedTask struct {
	request   metadata.Request
	cacheRoot string
	input     []ai.Identity
	paths     []string
	write     func(context.Context, *metadata.Option, *artwork.Downloader) error
	local     func([]ai.LocalDescription) (*metadata.Option, error)
}

func classificationInput(discovery *Discovery) (ai.BatchInput, map[string]*media_file.MediaFile, error) {
	input := ai.BatchInput{Root: discovery.Root}
	files := make(map[string]*media_file.MediaFile, len(discovery.Files))
	for _, file := range discovery.Files {
		relative, err := filepath.Rel(discovery.Root, file.Path)
		if err != nil {
			return input, nil, err
		}
		relative = filepath.ToSlash(relative)
		if _, exists := files[relative]; exists {
			return input, nil, errors.New("发现重复媒体路径")
		}
		files[relative] = file
		input.Files = append(input.Files, ai.BatchFile{RelativePath: relative})
	}
	return input, files, nil
}

func prepareTasks(root string, files map[string]*media_file.MediaFile, identities []ai.Identity) ([]preparedTask, error) {
	groups := make(map[string][]shows.Entry)
	var tasks []preparedTask
	seen := make(map[string]bool)
	for _, identity := range identities {
		file, ok := files[identity.RelativePath]
		if !ok || seen[identity.RelativePath] {
			return nil, errors.New("分类结果包含未知或重复媒体")
		}
		seen[identity.RelativePath] = true
		switch identity.MediaType {
		case "movie":
			task, err := movies.Prepare(root, file, identity)
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, preparedTask{request: task.Request, cacheRoot: task.CacheRoot, input: task.Input, paths: task.OutputPaths(), write: task.Write, local: task.Local})
		case "tv":
			if file.IsDisc() {
				return nil, errors.New("剧集原盘尚不能确定单个视频的季集映射")
			}
			showRoot := filepath.Clean(filepath.Join(root, filepath.FromSlash(identity.SeriesRoot)))
			relative, err := filepath.Rel(root, showRoot)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, errors.New("节目目录超出输入范围")
			}
			info, err := os.Stat(showRoot)
			if err != nil || !info.IsDir() {
				return nil, fmt.Errorf("节目目录不存在: %s", showRoot)
			}
			groups[showRoot] = append(groups[showRoot], shows.Entry{File: file, Identity: identity})
		default:
			return nil, errors.New("AI 未返回可处理的媒体类型")
		}
	}
	if len(seen) != len(files) {
		return nil, errors.New("分类结果遗漏媒体")
	}
	roots := make([]string, 0, len(groups))
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, showRoot := range roots {
		task, err := shows.Prepare(showRoot, groups[showRoot])
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, preparedTask{request: task.Request, cacheRoot: task.CacheRoot, input: task.Input, paths: task.OutputPaths(), write: task.Write, local: task.Local})
	}
	if err := validateOutputPlan(root, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}
