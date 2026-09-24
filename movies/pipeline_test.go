package movies

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func movieIdentity(path string) ai.Identity {
	return ai.Identity{ParseResult: ai.ParseResult{Title: "盗梦空间"}, RelativePath: path, MediaType: "movie"}
}

func TestPrepareManualSourceAndLocalWrite(t *testing.T) {
	root := t.TempDir()
	name := "Inception.2010.mkv"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".metadata", name+".source.json"), []byte(`{"provider":"tmdb","kind":"movie","id":"27205"}`), 0644); err != nil {
		t.Fatal(err)
	}
	mf := media_file.NewMediaFile(path, name)
	task, err := Prepare(root, mf, movieIdentity(name))
	if err != nil {
		t.Fatal(err)
	}
	if task.Request.Ref.ID != "27205" || task.Request.Ref.Provider != "tmdb" || len(task.Request.Files) != 1 || task.Request.Files[0] != path || task.Input[0].RelativePath != name {
		t.Fatalf("人工约束或输入丢失：%+v", task)
	}
	if _, err := task.Local([]ai.LocalDescription{{RelativePath: "other.mkv", Title: "错误"}}); err == nil {
		t.Fatal("接受了其他文件描述")
	}
	option, err := task.Local([]ai.LocalDescription{{RelativePath: name, Title: "盗梦空间", Plot: "梦境题材", Genres: []string{"剧情"}}})
	if err != nil {
		t.Fatal(err)
	}
	if option.Work.Ref.Provider != "local" || option.Work.SourceURL != "" || len(option.Work.Actors) != 0 || len(option.Work.Artwork) != 0 || !strings.HasPrefix(option.Work.Plot, "依据目录和文件名整理：") {
		t.Fatalf("本地事实越界：%+v", option.Work)
	}
	old := config.Scraper
	config.Scraper = &config.ScraperConfig{}
	t.Cleanup(func() { config.Scraper = old })
	if err := task.Write(context.Background(), option, nil); err == nil {
		t.Fatal("本地事实绕过人工来源指定")
	}
	option.Work.Ref = metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "27205"}
	if err := task.Write(context.Background(), option, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "Inception.2010.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		XMLName xml.Name
		Title   string `xml:"title"`
		Plot    string `xml:"plot"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.XMLName.Local != "movie" || doc.Title != "盗梦空间" || !strings.Contains(doc.Plot, "梦境题材") {
		t.Fatalf("NFO不正确：%s", data)
	}
}

func TestPrepareRejectsConflictingManualIDs(t *testing.T) {
	root := t.TempDir()
	name := "Film.mkv"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmdb", "id.txt"), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmdb", name+".id.txt"), []byte("2"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root, media_file.NewMediaFile(path, name), movieIdentity(name)); err == nil {
		t.Fatal("冲突人工编号未拒绝")
	}
}

func TestDiscPathsAndTwoNFOOutputs(t *testing.T) {
	for _, name := range []string{"BDMV", "VIDEO_TS"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			mf := media_file.NewMediaFile(path, name)
			task, err := Prepare(root, mf, movieIdentity(name))
			if err != nil {
				t.Fatal(err)
			}
			if len(task.movie.NfoFiles) != 2 || task.movie.ClearLogoFile != filepath.Join(root, "clearlogo.png") {
				t.Fatalf("原盘路径错误：%+v", task.movie)
			}
			paths := task.OutputPaths()
			if len(paths) != 5 || paths[0] != filepath.Join(root, "movie.nfo") || paths[1] != task.movie.NfoFiles[1] || paths[4] != filepath.Join(root, "clearlogo.png") {
				t.Fatalf("原盘输出范围不完整：%+v", paths)
			}
			old := config.Scraper
			config.Scraper = &config.ScraperConfig{}
			t.Cleanup(func() { config.Scraper = old })
			option := &metadata.Option{Work: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "1"}, Title: "电影"}}
			if err := task.Write(context.Background(), option, nil); err != nil {
				t.Fatal(err)
			}
			for _, file := range task.movie.NfoFiles {
				if _, err := os.Stat(file); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
