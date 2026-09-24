package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestMainProcess(t *testing.T) {
	if os.Getenv("KODI_TEST_MAIN") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	main()
}

func TestMainRuntimeFailureLogsOnlyOnceAndExits(t *testing.T) {
	for _, test := range []struct {
		name, marker string
		level        int
	}{{"status", "AI 请求状态码 503", 1}, {"transport", "AI 请求失败", 1}, {"fatal_transport", "AI 请求失败", 4}} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if strings.Contains(test.name, "transport") {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "movie.mkv"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			cfg := fmt.Sprintf(`{"llms":[{"name":"model","type":"openai","base_url":%q,"api_key":"model-secret","model":"test"}],"scraper":{"providers":["tmdb"],"extract_llm":"model","select_llm":"model"},"tmdb":{"api_key":"tmdb-secret","timeout_seconds":30},"log":{"mode":1,"level":%d}}`, server.URL+"/model-secret", test.level)
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestMainProcess$", "--", "--config", configPath, "--path", root)
			cmd.Env = append(os.Environ(), "KODI_TEST_MAIN=1")
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("运行异常没有非零退出: %v %s", err, output)
			}
			if strings.Count(string(output), test.marker) != 1 || strings.Contains(string(output), "-secret") || calls.Load() != 1 {
				t.Fatalf("异常日志重复、泄密或发生重试: %s 调用=%d", output, calls.Load())
			}
		})
	}
}

func TestMainConfigurationFailureReportsStderrAndExits(t *testing.T) {
	for _, body := range []string{"", `{"tmdb":{"retry_count":0}}`} {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.json")
		if body != "" {
			if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainProcess$", "--", "--config", configPath, "--path", root)
		cmd.Env = append(os.Environ(), "KODI_TEST_MAIN=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() == 0 || stderr.Len() == 0 {
			t.Fatalf("配置异常未向 stderr 报错并退出: %v %s", err, stderr.String())
		}
	}
}
