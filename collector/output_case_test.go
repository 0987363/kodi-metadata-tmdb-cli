package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputPlanUsesActualFilesystemCaseSemantics(t *testing.T) {
	root := t.TempDir()
	root, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	upper := filepath.Join(root, "A.marker")
	lower := filepath.Join(root, "a.marker")
	if err := os.WriteFile(upper, []byte("upper"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lower, []byte("lower"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(upper)
	if err != nil {
		t.Fatal(err)
	}
	caseSensitive := string(data) == "upper"
	err = validateOutputPlan(root, []preparedTask{{cacheRoot: filepath.Join(root, ".metadata"), paths: []string{filepath.Join(root, "A.nfo"), filepath.Join(root, "a.nfo")}}})
	if caseSensitive && err != nil {
		t.Fatalf("区分大小写文件系统上的不同目标被误拒绝: %v", err)
	}
	if !caseSensitive && err == nil {
		t.Fatal("大小写别名目标未拒绝")
	}
}
