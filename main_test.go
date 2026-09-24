package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCLIAcceptsOnePath(t *testing.T) {
	for _, args := range [][]string{{"--path", "/library"}, {"--path=/library", "-config", "settings.json"}} {
		options, err := parseCLI(args)
		if err != nil || options.path != "/library" {
			t.Fatalf("单目录参数解析失败: %v %+v", err, options)
		}
	}
	if options, err := parseCLI([]string{"-version"}); err != nil || !options.version {
		t.Fatalf("版本查询无需媒体目录: %v %+v", err, options)
	}
}

func TestParseCLIRejectsAmbiguousPathsAndOldMode(t *testing.T) {
	for _, args := range [][]string{
		{}, {"/library"}, {"--path", ""}, {"--path="},
		{"--path", "/a", "--path=/b"}, {"--path=/a", "--path", "/b"},
		{"--path", "/a", "/b"}, {"--path", "/a", "-mode", "2"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("歧义或旧参数被接受: %v", args)
		}
	}
}

func TestValidateMediaPathRequiresDirectory(t *testing.T) {
	root := t.TempDir()
	if err := validateMediaPath(root); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "a.mkv")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", file, filepath.Join(root, "missing")} {
		if err := validateMediaPath(path); err == nil {
			t.Fatalf("非目录被接受: %q", path)
		}
	}
}
