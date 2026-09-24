package shows

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

func showEntry(t *testing.T, root, sub, name, relative string, season, episode *int) Entry {
	t.Helper()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	seriesRoot := "."
	if strings.HasPrefix(relative, filepath.Base(root)+"/") {
		seriesRoot = filepath.Base(root)
	}
	return Entry{File: media_file.NewMediaFile(path, name), Identity: ai.Identity{ParseResult: ai.ParseResult{Title: "节目", Season: season, Episode: episode}, RelativePath: relative, MediaType: "tv", SeriesRoot: seriesRoot}}
}
func number(n int) *int { return &n }

func TestPrepareCollectsIDsBeyondFirstEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	a := showEntry(t, root, "Season 01", "one.mkv", "Season 01/one.mkv", number(1), number(1))
	b := showEntry(t, root, "Season 01", "two.mkv", "Season 01/two.mkv", number(1), number(2))
	c := showEntry(t, root, "Season 01", "three.mkv", "Season 01/three.mkv", number(1), number(3))
	b.Identity.TMDBID = "42"
	c.Identity.TMDBID = "43"
	if _, err := Prepare(root, []Entry{a, b, c}); err == nil {
		t.Fatal("首项无编号时吞掉了后续编号冲突")
	}
	c.Identity.TMDBID = "42"
	task, err := Prepare(root, []Entry{a, b, c})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Request.Hints) != 1 || task.Request.Hints[0] != (metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}) {
		t.Fatalf("后续一致编号未保留：%+v", task.Request.Hints)
	}
	c.Identity.TheTVDBID = "100"
	task, err = Prepare(root, []Entry{a, b, c})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Request.Hints) != 2 || task.Request.Hints[1] != (metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "100"}) {
		t.Fatalf("后项 TheTVDB 编号未保留：%+v", task.Request.Hints)
	}
}

func TestPrepareRequiresNameEvidenceWithoutSharedID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	a := showEntry(t, root, "Season 01", "one.mkv", "Season 01/one.mkv", number(1), number(1))
	b := showEntry(t, root, "Season 01", "two.mkv", "Season 01/two.mkv", number(1), number(2))
	a.Identity.Title = "  Dream SHOW "
	b.Identity.Title = "另一部作品"
	b.Identity.ChsTitle = "dreamshow"
	if _, err := Prepare(root, []Entry{a, b}); err != nil {
		t.Fatalf("同名别名未识别：%v", err)
	}
	b.Identity.ChsTitle = ""
	if _, err := Prepare(root, []Entry{a, b}); err == nil {
		t.Fatal("无编号且无标题交集的作品被合并")
	}
	a.Identity.TMDBID, b.Identity.TMDBID = "42", "42"
	if _, err := Prepare(root, []Entry{a, b}); err != nil {
		t.Fatalf("相同来源编号的语言差异被误拒：%v", err)
	}
	b.Identity.TheTVDBID = "100"
	a.Identity.TheTVDBID = "101"
	if _, err := Prepare(root, []Entry{a, b}); err == nil {
		t.Fatal("相同TMDb编号掩盖TheTVDB编号冲突")
	}
}

func TestPrepareManualSeasonGroupJoinAndLocal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	first := showEntry(t, root, "Season 01", "episode-a.mkv", "节目/Season 01/episode-a.mkv", number(1), number(2))
	second := showEntry(t, root, "Season 01", "episode-b.mkv", "节目/Season 01/episode-b.mkv", number(1), number(2))
	seasonRoot := filepath.Join(root, "Season 01")
	if err := os.MkdirAll(filepath.Join(seasonRoot, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmdb", "id.txt"), []byte("42"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmdb", "group.txt"), []byte("root-group"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonRoot, "tmdb", "group.txt"), []byte("season-group"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonRoot, "tmdb", "join.txt"), []byte("1,2,13"), 0644); err != nil {
		t.Fatal(err)
	}
	task, err := Prepare(root, []Entry{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if task.Request.Ref.Provider != "tmdb" || task.Request.Ref.ID != "42" || len(task.Request.Episodes) != 1 || task.Request.Episodes[0] != (metadata.EpisodeKey{Season: 2, Episode: 14, Group: "season-group"}) {
		t.Fatalf("人工约束或共享单集错误：%+v", task.Request)
	}
	if task.Input[0].RelativePath != first.Identity.RelativePath || len(task.Request.Files) != 2 {
		t.Fatal("输入证据丢失")
	}
	paths := task.OutputPaths()
	seasonPoster := filepath.Join(root, "season02-poster.jpg")
	posterCount := 0
	for _, path := range paths {
		if path == seasonPoster {
			posterCount++
		}
	}
	if len(paths) != 9 || posterCount != 1 {
		t.Fatalf("节目固定图片或文件目标数量错误：%+v", paths)
	}
	descriptions := []ai.LocalDescription{{RelativePath: first.Identity.RelativePath, Title: "第二回", Plot: "文件说明"}, {RelativePath: second.Identity.RelativePath, Title: "第二回", Plot: "另一版本"}}
	local, err := task.Local(descriptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(local.Episodes) != 1 || local.Work.Ref.Provider != "local" || local.Episodes[0].Record.Ref.Provider != "local" || local.Episodes[0].Record.SeasonNumber != 2 || !strings.HasPrefix(local.Episodes[0].Record.Plot, "依据目录和文件名整理：") {
		t.Fatalf("本地事实错误：%+v", local)
	}
	if _, err := task.Local(descriptions[:1]); err == nil {
		t.Fatal("接受了缺失描述")
	}
}

func TestPrepareUnknownCoordinateAndManualConflict(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	entry := showEntry(t, root, "Season 01", "unknown.mkv", "Season 01/unknown.mkv", number(1), nil)
	if err := os.MkdirAll(filepath.Join(root, "Season 01", "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Season 01", "tmdb", "join.txt"), []byte("1,2,13"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root, []Entry{entry}); err == nil {
		t.Fatal("未知集号被join映射补造")
	}
	entry.Identity.Episode = number(2)
	if err := os.MkdirAll(filepath.Join(root, ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".metadata", "source.json"), []byte(`{"provider":"thetvdb"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmdb"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmdb", "group.txt"), []byte("group"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root, []Entry{entry}); err == nil {
		t.Fatal("人工分组未约束来源")
	}
}

func TestWriteValidatesAllEpisodesBeforeAnyNFO(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	e1 := showEntry(t, root, "Season 01", "s01e01.mkv", "Season 01/s01e01.mkv", number(1), number(1))
	e2 := showEntry(t, root, "Season 01", "s01e02.mkv", "Season 01/s01e02.mkv", number(1), number(2))
	task, err := Prepare(root, []Entry{e1, e2})
	if err != nil {
		t.Fatal(err)
	}
	old := config.Scraper
	config.Scraper = &config.ScraperConfig{}
	t.Cleanup(func() { config.Scraper = old })
	work := &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "节目"}
	episode := func(n int) *metadata.Record {
		return &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: string(rune('0' + n))}, Title: "单集", SeasonNumber: 1, EpisodeNumber: n}
	}
	incomplete := &metadata.Option{Work: work, Episodes: []metadata.SeriesEpisode{{Key: metadata.EpisodeKey{Season: 1, Episode: 1}, Record: episode(1)}}}
	if err := task.Write(context.Background(), incomplete, nil); err == nil {
		t.Fatal("缺失单集仍写入")
	}
	if _, err := os.Stat(filepath.Join(root, "tvshow.nfo")); !os.IsNotExist(err) {
		t.Fatalf("预检前已写入：%v", err)
	}
	complete := &metadata.Option{Work: work, Episodes: []metadata.SeriesEpisode{{Key: metadata.EpisodeKey{Season: 1, Episode: 1}, Record: episode(1)}, {Key: metadata.EpisodeKey{Season: 1, Episode: 2}, Record: episode(2)}}}
	if err := task.Write(context.Background(), complete, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"s01e01.nfo", "s01e02.nfo"} {
		data, err := os.ReadFile(filepath.Join(root, "Season 01", name))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			XMLName   xml.Name
			ShowTitle string `xml:"showtitle"`
			Season    int    `xml:"season"`
			Episode   int    `xml:"episode"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.XMLName.Local != "episodedetails" || doc.ShowTitle != "节目" || doc.Season != 1 || doc.Episode < 1 {
			t.Fatalf("NFO错误：%s", data)
		}
	}
}

func TestLocalVersionsShareEpisodeFactAndWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "节目")
	a := showEntry(t, root, "Season 00", "special-1080p.mkv", "节目/Season 00/special-1080p.mkv", number(0), number(2))
	b := showEntry(t, root, "Season 00", "special-4k.mkv", "节目/Season 00/special-4k.mkv", number(0), number(2))
	task, err := Prepare(root, []Entry{a, b})
	if err != nil {
		t.Fatal(err)
	}
	option, err := task.Local([]ai.LocalDescription{{RelativePath: a.Identity.RelativePath, Title: "特别篇", Plot: "目录信息"}, {RelativePath: b.Identity.RelativePath, Title: "特别篇", Plot: "目录信息"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(option.Episodes) != 1 || option.Episodes[0].Key.Season != 0 || option.Episodes[0].Record.Ref.ID == "" {
		t.Fatalf("版本未共享单集：%+v", option)
	}
	old := config.Scraper
	config.Scraper = &config.ScraperConfig{}
	t.Cleanup(func() { config.Scraper = old })
	if err := task.Write(context.Background(), option, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"special-1080p.nfo", "special-4k.nfo"} {
		data, err := os.ReadFile(filepath.Join(root, "Season 00", name))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Season  *int `xml:"season"`
			Episode *int `xml:"episode"`
			IDs     []struct {
				Type  string `xml:"type,attr"`
				Value string `xml:",chardata"`
			} `xml:"uniqueid"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Season == nil || *doc.Season != 0 || doc.Episode == nil || *doc.Episode != 2 || len(doc.IDs) != 1 || doc.IDs[0].Value != option.Episodes[0].Record.Ref.ID {
			t.Fatalf("特别篇或共享编号错误：%s", data)
		}
	}
}

func TestOutputPathsPreservesSingleFileCollisions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []string
		conflict string
	}{
		{name: "programme NFO", files: []string{"tvshow.mkv"}, conflict: "tvshow.nfo"},
		{name: "same stem", files: []string{"A.mkv", "A.mp4"}, conflict: "A.nfo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "节目")
			var entries []Entry
			for i, name := range tc.files {
				entries = append(entries, showEntry(t, root, "", name, name, number(1), number(i+1)))
			}
			task, err := Prepare(root, entries)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, path := range task.OutputPaths() {
				if path == filepath.Join(root, tc.conflict) {
					count++
				}
			}
			if count != 2 {
				t.Fatalf("冲突目标应保留两次，实际 %d", count)
			}
			old := config.Scraper
			config.Scraper = &config.ScraperConfig{}
			t.Cleanup(func() { config.Scraper = old })
			option := &metadata.Option{Work: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "42"}, Title: "节目"}}
			for _, key := range task.Request.Episodes {
				option.Episodes = append(option.Episodes, metadata.SeriesEpisode{Key: key, Record: &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: "episode"}, Title: "单集", SeasonNumber: key.Season, EpisodeNumber: key.Episode}})
			}
			if err := task.Write(context.Background(), option, nil); err == nil {
				t.Fatal("目标路径冲突仍写入")
			}
			if _, err := os.Stat(filepath.Join(root, "tvshow.nfo")); !os.IsNotExist(err) {
				t.Fatalf("路径冲突预检前写出：%v", err)
			}
		})
	}
}
