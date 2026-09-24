package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Discovery struct {
	Root     string
	Files    []*media_file.MediaFile
	Excluded int
}

// Discover 递归发现指定目录内的媒体实体，原盘目录按单个实体返回。
func Discover(path string) (*Discovery, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("媒体目录不能为空")
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("媒体路径不是目录: %s", root)
	}
	disc, err := containingDisc(root)
	if err != nil {
		return nil, err
	}
	if disc != "" {
		return nil, fmt.Errorf("媒体路径位于原盘结构内: %s；请从父目录 %s 扫描", root, filepath.Dir(disc))
	}

	result := &Discovery{Root: root, Files: []*media_file.MediaFile{}}
	seen := make(map[string]struct{})
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if current != root && skipFolder(entry.Name()) {
				result.Excluded++
				return fs.SkipDir
			}
			mf := media_file.NewMediaFile(current, entry.Name())
			if mf != nil && mf.IsDisc() {
				valid, err := isRawDiscDirectory(current)
				if err != nil {
					return err
				}
				if !valid {
					return nil
				}
				if skipFile(filepath.Base(filepath.Dir(current))) || skipFile(entry.Name()) {
					result.Excluded++
					return fs.SkipDir
				}
				appendDiscovered(result, seen, mf)
				return fs.SkipDir
			}
			return nil
		}
		if skipFile(entry.Name()) {
			result.Excluded++
			return nil
		}
		mf := media_file.NewMediaFile(current, entry.Name())
		if mf != nil && mf.IsVideo() {
			appendDiscovered(result, seen, mf)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func containingDisc(root string) (string, error) {
	var outermost string
	for current := root; ; current = filepath.Dir(current) {
		valid, err := isRawDiscDirectory(current)
		if err != nil {
			return "", err
		}
		if valid {
			outermost = current
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	return outermost, nil
}

// isRawDiscDirectory 同时检查目录名和原盘标志，避免误判普通同名目录。
func isRawDiscDirectory(dir string) (bool, error) {
	name := strings.ToLower(filepath.Base(dir))
	if name != media_file.BDMVType && name != media_file.VideoTsType && name != media_file.HvdvdType && name != media_file.DVDType {
		return false, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		entryName := strings.ToLower(entry.Name())
		switch name {
		case media_file.BDMVType:
			if entry.Type().IsRegular() && (entryName == "index.bdmv" || entryName == "movieobject.bdmv") {
				return true, nil
			}
			if entry.IsDir() && entryName == "stream" {
				streams, err := os.ReadDir(filepath.Join(dir, entry.Name()))
				if err != nil {
					return false, err
				}
				for _, stream := range streams {
					if stream.Type().IsRegular() && strings.EqualFold(filepath.Ext(stream.Name()), ".m2ts") {
						return true, nil
					}
				}
			}
		case media_file.VideoTsType:
			if entry.Type().IsRegular() && (entryName == "video_ts.ifo" || strings.HasPrefix(entryName, "vts_") && (strings.HasSuffix(entryName, ".ifo") || strings.HasSuffix(entryName, ".vob") || strings.HasSuffix(entryName, ".bup"))) {
				return true, nil
			}
		case media_file.HvdvdType:
			if entry.Type().IsRegular() && (strings.HasSuffix(entryName, ".evo") || strings.HasPrefix(entryName, "hv") && strings.HasSuffix(entryName, ".ifo")) {
				return true, nil
			}
		case media_file.DVDType:
			if entry.IsDir() && entryName == media_file.VideoTsType {
				return isRawDiscDirectory(filepath.Join(dir, entry.Name()))
			}
		}
	}
	return false, nil
}

func appendDiscovered(result *Discovery, seen map[string]struct{}, mf *media_file.MediaFile) {
	path := filepath.Clean(mf.Path)
	if _, exists := seen[path]; exists {
		return
	}
	seen[path] = struct{}{}
	result.Files = append(result.Files, mf)
}
