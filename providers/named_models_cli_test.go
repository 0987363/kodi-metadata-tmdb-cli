package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNamedModelsCLIAreIsolated(t *testing.T) {
	var extracted, selected atomic.Int64
	analyzer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/movie/42" {
			_, _ = w.Write([]byte(`{"id":42,"title":"Named Models","runtime":123,"spoken_languages":[{"iso_639_1":"zh","name":"中文"}]}`))
			return
		}
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer extract-key" {
			t.Error("提取模型地址或凭据错误")
			w.WriteHeader(400)
			return
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "extract-model" || len(body.Messages) < 2 || !strings.Contains(body.Messages[1].Content, "extract_media_identity") {
			t.Error("提取实例接收到错误任务或模型")
			w.WriteHeader(400)
			return
		}
		extracted.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"title":"Named Models","tmdb_id":"42"}`}}}})
	}))
	defer analyzer.Close()
	judge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer select-key" {
			t.Error("判断模型地址或凭据错误")
			w.WriteHeader(400)
			return
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "select-model" || len(body.Messages) < 2 || !strings.Contains(body.Messages[1].Content, "select_metadata_candidate") {
			t.Error("判断实例接收到错误任务或模型")
			w.WriteHeader(400)
			return
		}
		selected.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"choice":"c0"}`}}}})
	}))
	defer judge.Close()
	root := t.TempDir()
	media := filepath.Join(root, "movies")
	if err := os.Mkdir(media, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "Named Models.mkv"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{
		"llms": []any{
			map[string]any{"name": "extract", "type": "openai", "base_url": analyzer.URL, "api_key": "extract-key", "model": "extract-model"},
			map[string]any{"name": "select", "type": "openai", "base_url": judge.URL, "api_key": "select-key", "model": "select-model"},
			map[string]any{"name": "unused", "type": "jev"},
		},
		"scraper":   map[string]any{"providers": []string{"tmdb"}, "extract_llm": "extract", "select_llm": "select"},
		"tmdb":      map[string]any{"api_host": analyzer.URL, "api_key": "source-key"},
		"collector": map[string]any{"run_mode": 2, "movies_dir": []string{media}},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "config.json")
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".", "-config", file, "-mode", "2")
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("具名模型CLI失败：%v\n%s", err, output)
	}
	nfo, err := os.ReadFile(filepath.Join(media, "Named Models.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if extracted.Load() != 1 || selected.Load() != 1 || !strings.Contains(string(nfo), "Named Models") || !strings.Contains(string(nfo), "<runtime>123</runtime>") || strings.Contains(string(nfo), "<languages>") {
		t.Fatalf("任务实例或NFO错误：提取%d 判断%d %s", extracted.Load(), selected.Load(), nfo)
	}
}
