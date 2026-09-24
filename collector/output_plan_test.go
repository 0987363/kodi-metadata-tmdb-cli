package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputPlanRejectsEscapingTargetsAndCacheLinks(t *testing.T) {
	root := t.TempDir()
	root, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	outside := t.TempDir()
	if err := validateOutputPlan(root, []preparedTask{{cacheRoot: filepath.Join(root, ".metadata"), paths: []string{filepath.Join(outside, "movie.nfo")}}}); err == nil {
		t.Fatal("允许越界输出")
	}
	if err := os.Symlink(outside, filepath.Join(root, ".metadata")); err != nil {
		t.Fatal(err)
	}
	if err := validateOutputPlan(root, []preparedTask{{cacheRoot: filepath.Join(root, ".metadata"), paths: []string{filepath.Join(root, "movie.nfo")}}}); err == nil {
		t.Fatal("允许缓存目录经符号链接越界")
	}
}

func TestOutputPlanRejectsThumbnailMarkerDirectoryEscape(t *testing.T) {
	root := t.TempDir()
	root, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	outside := t.TempDir()
	season := filepath.Join(root, "Season 1")
	if err := os.Mkdir(season, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(season, ".metadata")); err != nil {
		t.Fatal(err)
	}
	err := validateOutputPlan(root, []preparedTask{{cacheRoot: filepath.Join(root, ".metadata"), paths: []string{filepath.Join(season, "E01-thumb.jpg")}}})
	if err == nil || !strings.Contains(err.Error(), "图片来源标记") {
		t.Fatalf("图片来源标记边界未验证: %v", err)
	}
}
