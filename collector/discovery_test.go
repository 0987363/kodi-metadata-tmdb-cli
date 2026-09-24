package collector

import (
	"fengqi/kodi-metadata-tmdb-cli/config"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverRecursiveVideosAndExplicitFilters(t *testing.T) {
	root := t.TempDir()
	old := config.Collector
	config.Collector = &config.CollectorConfig{SkipFolders: []string{"ignored"}, SkipKeywords: []string{"omit"}, TmpSuffix: []string{".preview.mkv"}}
	t.Cleanup(func() { config.Collector = old })
	for _, name := range []string{"A.mkv", "series/S01E01.mp4", "sample/clip.mkv", "trailers/trailer.mp4", "extras/feature.mkv", "ignored/no.mkv", "omit.mkv", "clip.preview.mkv", ".hidden/no.mkv", "poster.jpg"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != wantRoot {
		t.Fatalf("Root = %q", got.Root)
	}
	if len(got.Files) != 5 || got.Excluded != 4 {
		t.Fatalf("发现数量或排除数量异常: %+v", got)
	}
	for _, mf := range got.Files {
		if !mf.IsVideo() {
			t.Fatalf("非视频进入结果: %+v", mf)
		}
	}
}

func TestDiscoverDiscAsOneUnit(t *testing.T) {
	for _, discName := range []string{"BDMV", "VIDEO_TS", "HDVD_TS", "DVD"} {
		t.Run(discName, func(t *testing.T) {
			root := t.TempDir()
			path, interior := makeDiscFixture(t, root, discName)
			got, err := Discover(root)
			if err != nil {
				t.Fatal(err)
			}
			wantRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Files) != 1 || got.Files[0].Path != filepath.Join(wantRoot, "Movie", discName) || !got.Files[0].IsDisc() {
				t.Fatalf("原盘未合并: %+v", got)
			}
			for _, forbidden := range []string{path, interior} {
				if _, err := Discover(forbidden); err == nil || !strings.Contains(err.Error(), "原盘结构") || !strings.Contains(err.Error(), "请从父目录 "+filepath.Join(wantRoot, "Movie")) {
					t.Fatalf("原盘根或内部目录未返回结构错误: %s %v", forbidden, err)
				}
			}
		})
	}
}

func makeDiscFixture(t *testing.T, root, discName string) (string, string) {
	t.Helper()
	disc := filepath.Join(root, "Movie", discName)
	interior := disc
	marker := ""
	switch discName {
	case "BDMV":
		interior = filepath.Join(disc, "STREAM")
		marker = filepath.Join(interior, "00001.m2ts")
	case "VIDEO_TS":
		marker = filepath.Join(disc, "VIDEO_TS.IFO")
	case "HDVD_TS":
		interior = filepath.Join(disc, "ADV_OBJ")
		marker = filepath.Join(disc, "FEATURE.EVO")
	case "DVD":
		interior = filepath.Join(disc, "VIDEO_TS")
		marker = filepath.Join(interior, "VIDEO_TS.IFO")
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(interior, 0755); err != nil {
		t.Fatal(err)
	}
	return disc, interior
}

func TestDiscoverAllowsOrdinaryDiscNamedDirectories(t *testing.T) {
	for _, name := range []string{"BDMV", "VIDEO_TS", "HDVD_TS", "DVD"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, name)
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "clip.mkv"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{root, dir} {
				got, err := Discover(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Files) != 1 || !got.Files[0].IsVideo() {
					t.Fatalf("普通同名目录被误判原盘: %s %+v %v", path, got, err)
				}
			}
		})
	}
}

func TestDiscoverRejectsNonDirectoryAndDoesNotFollowOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	if _, err := Discover(filepath.Join(root, "missing")); err == nil {
		t.Fatal("不存在目录应失败")
	}
	file := filepath.Join(root, "a.mkv")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(file); err == nil {
		t.Fatal("文件路径应失败")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.mkv"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != filepath.Join(wantRoot, "a.mkv") {
		t.Fatalf("符号链接外逸: %+v", got)
	}
}

func TestDiscoverPropagatesWalkError(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0700) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("当前账户仍可读取零权限目录")
	}
	if _, err := Discover(root); err == nil {
		t.Fatal("遍历错误被静默忽略")
	}
}
