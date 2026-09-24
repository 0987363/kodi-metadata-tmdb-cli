package collector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateOutputPlan 在外部取数之前检查所有写入目标，防止不同任务相互覆盖。
func validateOutputPlan(root string, tasks []preparedTask) error {
	seen := make(map[string][]string)
	caseSensitivity := make(map[string]bool)
	for _, task := range tasks {
		if err := validateOutputLocation(root, task.cacheRoot); err != nil {
			return fmt.Errorf("缓存目录: %w", err)
		}
		for _, target := range task.paths {
			if err := validateOutputLocation(root, target); err != nil {
				return err
			}
			if !strings.EqualFold(filepath.Ext(target), ".nfo") {
				marker := filepath.Join(filepath.Dir(target), ".metadata", "artwork", filepath.Base(target)+".url")
				if err := validateOutputLocation(root, marker); err != nil {
					return fmt.Errorf("图片来源标记: %w", err)
				}
			}
			key := strings.ToLower(filepath.Clean(target))
			for _, prior := range seen[key] {
				conflict, err := outputTargetsAlias(prior, target, caseSensitivity)
				if err != nil {
					return err
				}
				if conflict {
					return fmt.Errorf("输出路径冲突: %s 与 %s", prior, target)
				}
			}
			seen[key] = append(seen[key], target)
		}
	}
	return nil
}

func validateOutputLocation(root, target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("输出路径为空")
	}
	target = filepath.Clean(target)
	if !withinDirectory(root, target) {
		return fmt.Errorf("输出路径超出 --path 范围: %s", target)
	}
	ancestor := target
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("无法确定输出路径的现有父目录")
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return err
	}
	if !withinDirectory(root, resolved) {
		return fmt.Errorf("输出路径经符号链接离开 --path 范围: %s", target)
	}
	return nil
}

func withinDirectory(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func outputTargetsAlias(left, right string, caseSensitivity map[string]bool) (bool, error) {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if left == right {
		return true, nil
	}
	leftDir, rightDir := filepath.Dir(left), filepath.Dir(right)
	leftInfo, err := os.Stat(leftDir)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(rightDir)
	if err != nil {
		return false, err
	}
	if !os.SameFile(leftInfo, rightInfo) {
		return false, nil
	}
	if filepath.Base(left) == filepath.Base(right) {
		return true, nil
	}
	sensitive, known := caseSensitivity[leftDir]
	if !known {
		sensitive, err = directoryCaseSensitive(leftDir)
		if err != nil {
			return false, err
		}
		caseSensitivity[leftDir] = sensitive
	}
	return !sensitive, nil
}

// 仅遇到大小写相近的输出时探测所在目录，临时文件在返回前删除。
func directoryCaseSensitive(directory string) (sensitive bool, err error) {
	file, err := os.CreateTemp(directory, ".nfo-case-probe-A-")
	if err != nil {
		return false, err
	}
	name := file.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil {
			err = errors.Join(err, removeErr)
		}
	}()
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		return false, errors.Join(statErr, closeErr)
	}
	alias := filepath.Join(directory, strings.ToUpper(filepath.Base(name)))
	other, aliasErr := os.Stat(alias)
	if errors.Is(aliasErr, os.ErrNotExist) {
		return true, nil
	}
	if aliasErr != nil {
		return false, aliasErr
	}
	return !os.SameFile(info, other), nil
}
