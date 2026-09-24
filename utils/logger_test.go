package utils

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
)

func TestErrorRemainsVisibleAtFatalThreshold(t *testing.T) {
	for _, mode := range []int{config.LogModeStdout, config.LogModeLogfile, config.LogModeBoth} {
		t.Run(string(rune('0'+mode)), func(t *testing.T) {
			var output bytes.Buffer
			oldOutput := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(oldOutput) })
			file, err := os.Create(filepath.Join(t.TempDir(), "error.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			logger := &logger{level: FATAL, lock: new(sync.Mutex), file: file, mode: mode}
			logger.Info("普通进度")
			logger.DebugF("调试进度 %d", 1)
			logger.Error("运行异常")
			logger.ErrorF("请求异常 %d", 503)
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, sink := range []struct {
				name, text string
				enabled    bool
			}{{"标准日志", output.String(), mode != config.LogModeLogfile}, {"文件日志", string(data), mode != config.LogModeStdout}} {
				want := 0
				if sink.enabled {
					want = 1
				}
				if strings.Count(sink.text, "运行异常") != want || strings.Count(sink.text, "请求异常 503") != want || strings.Contains(sink.text, "进度") {
					t.Fatalf("%s 过滤了异常或改变普通日志级别: %s", sink.name, sink.text)
				}
			}
		})
	}
}
