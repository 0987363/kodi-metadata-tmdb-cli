package nfo

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestWriteObjectFieldsAndPreserveSource(t *testing.T) {
	cases := []struct {
		name        string
		kind        metadata.Kind
		objectID    string
		crossSiteID string
		season      int
		episode     int
		runtime     int
		wantRoot    string
		wantRuntime int
	}{
		{name: "电影", kind: metadata.Movie, objectID: "100", crossSiteID: "tt100", runtime: 123, wantRoot: "movie", wantRuntime: 123},
		{name: "节目", kind: metadata.Show, objectID: "200", crossSiteID: "tt200", runtime: 48, wantRoot: "tvshow"},
		{name: "普通单集", kind: metadata.Episode, objectID: "201", crossSiteID: "tt201", season: 1, episode: 2, runtime: 45, wantRoot: "episodedetails", wantRuntime: 45},
		{name: "特别篇", kind: metadata.Episode, objectID: "202", crossSiteID: "tt202", season: 0, episode: 1, wantRoot: "episodedetails"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &metadata.Record{
				Ref: metadata.Ref{Provider: "tmdb", Kind: tc.kind, ID: tc.objectID},
				ExternalIDs: []metadata.Identifier{
					{Type: "tmdb", Value: tc.objectID},
					{Type: "imdb", Value: tc.crossSiteID},
				},
				Title: "中文 & <标题>", OriginalTitle: "原名 & <别名>", Plot: "简介 & <内容>",
				Premiered: "2020-01-02", Status: "已完结", Certification: "PG-13",
				Languages: []string{"中文", "English"}, Genres: []string{"剧情", "喜剧"},
				Studios: []string{"制作 & 公司"}, Countries: []string{"中国"},
				Directors: []string{"导演"}, Credits: []string{"编剧"},
				Actors:      []metadata.Actor{{Name: "演员 & 甲", Role: "角色 <乙>", Order: 1, Thumb: "https://example.invalid/actor.jpg?a=1&b=2"}},
				Ratings:     []metadata.Rating{{Source: "tmdb", Max: 10, Value: 8.5, Votes: 123}},
				SeasonCount: 2, EpisodeCount: 12, SeasonNumber: tc.season, EpisodeNumber: tc.episode, RuntimeMinutes: tc.runtime,
			}
			if tc.kind == metadata.Show {
				r.Seasons = []metadata.Season{{Number: 0, Title: "特别篇 & 花絮"}, {Number: 1, Title: "第一季"}}
			}
			before, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "sample.nfo")
			if err := Write(path, r, "节目 & <原名>", true, true); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				XMLName       xml.Name
				Title         string     `xml:"title"`
				OriginalTitle string     `xml:"originaltitle"`
				ShowTitle     string     `xml:"showtitle"`
				Plot          string     `xml:"plot"`
				UniqueIDs     []UniqueID `xml:"uniqueid"`
				Premiered     string     `xml:"premiered"`
				Aired         string     `xml:"aired"`
				Status        string     `xml:"status"`
				MPAA          string     `xml:"mpaa"`
				Genres        []string   `xml:"genre"`
				Tags          []string   `xml:"tag"`
				Studios       []string   `xml:"studio"`
				Countries     []string   `xml:"country"`
				Languages     []string   `xml:"languages"`
				Language      []string   `xml:"language"`
				Actors        []actor    `xml:"actor"`
				Directors     []string   `xml:"director"`
				Credits       []string   `xml:"credits"`
				Ratings       []rating   `xml:"ratings>rating"`
				Seasons       []season   `xml:"namedseason"`
				Season        *int       `xml:"season"`
				Episode       *int       `xml:"episode"`
				Runtime       *int       `xml:"runtime"`
			}
			if err := xml.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got.XMLName.Local != tc.wantRoot {
				t.Errorf("根元素错误：%q", got.XMLName.Local)
			}
			if got.Title != "中文 & <标题>" || got.OriginalTitle != "原名 & <别名>" || got.ShowTitle != "节目 & <原名>" || got.Plot != "简介 & <内容>" {
				t.Errorf("中文或 XML 转义损坏：%s", data)
			}
			wantIDs := []UniqueID{{Type: "tmdb", Default: true, Value: tc.objectID}, {Type: "imdb", Value: tc.crossSiteID}}
			if !reflect.DeepEqual(got.UniqueIDs, wantIDs) {
				t.Errorf("当前对象网站编号错误：%+v", got.UniqueIDs)
			}
			if tc.wantRuntime > 0 {
				if got.Runtime == nil || *got.Runtime != tc.wantRuntime {
					t.Errorf("来源标称时长未按分钟输出：%s", data)
				}
			} else if got.Runtime != nil {
				t.Errorf("不应输出对象时长：%d", *got.Runtime)
			}
			if len(got.Languages) != 0 || len(got.Language) != 0 {
				t.Errorf("不应输出 languages 或 language：%s", data)
			}
			if tc.kind == metadata.Episode {
				if got.Season == nil || *got.Season != tc.season || got.Episode == nil || *got.Episode != tc.episode || got.Aired != "2020-01-02" {
					t.Errorf("单集季集坐标或播出日期错误：%s", data)
				}
			} else if got.Season != nil || got.Episode != nil || got.Aired != "" {
				t.Errorf("非单集对象不应输出季集坐标或来源总量：%s", data)
			}
			if got.Premiered != "2020-01-02" || got.Status != "已完结" || got.MPAA != "PG-13" || !reflect.DeepEqual(got.Genres, []string{"剧情", "喜剧"}) || !reflect.DeepEqual(got.Tags, []string{"剧情", "喜剧"}) {
				t.Errorf("基本元数据丢失：%s", data)
			}
			if !reflect.DeepEqual(got.Studios, []string{"制作 & 公司"}) || !reflect.DeepEqual(got.Countries, []string{"中国"}) || !reflect.DeepEqual(got.Directors, []string{"导演"}) || !reflect.DeepEqual(got.Credits, []string{"编剧"}) {
				t.Errorf("制作信息丢失：%s", data)
			}
			if !reflect.DeepEqual(got.Actors, []actor{{Name: "演员 & 甲", Role: "角色 <乙>", Order: 1, Thumb: "https://example.invalid/actor.jpg?a=1&b=2"}}) || !reflect.DeepEqual(got.Ratings, []rating{{Name: "tmdb", Max: 10, Value: 8.5, Votes: 123}}) {
				t.Errorf("演员或单条评分映射改变：%s", data)
			}
			if tc.kind == metadata.Show && !reflect.DeepEqual(got.Seasons, []season{{Number: 0, Title: "特别篇 & 花絮"}, {Number: 1, Title: "第一季"}}) {
				t.Errorf("命名季丢失：%s", data)
			}
			after, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("写入 NFO 修改了来源事实：%s", after)
			}
		})
	}
}

func TestWriteOmitsNonpositiveRuntime(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Episode} {
		for _, runtime := range []int{0, -1} {
			r := &metadata.Record{Ref: metadata.Ref{Provider: "tmdb", Kind: kind, ID: "100"}, Title: "未知时长", RuntimeMinutes: runtime}
			path := filepath.Join(t.TempDir(), "sample.nfo")
			if err := Write(path, r, "", false, false); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "<runtime>") {
				t.Errorf("%s 的非正时长 %d 不应输出：%s", kind, runtime, data)
			}
		}
	}
}

func TestWritePreservesObjectIDsAndIndependentFlags(t *testing.T) {
	for _, tagEnabled := range []bool{false, true} {
		for _, genreEnabled := range []bool{false, true} {
			r := &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "371065"}, Title: "坑王驾到", ExternalIDs: []metadata.Identifier{{Type: "tvdb", Value: "371065"}, {Type: "tmdb", Value: "74747"}}, Genres: []string{"Talk Show"}, EpisodeCount: 52}
			path := filepath.Join(t.TempDir(), "tvshow.nfo")
			if err := Write(path, r, "", tagEnabled, genreEnabled); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				UniqueIDs []UniqueID `xml:"uniqueid"`
				Genres    []string   `xml:"genre"`
				Tags      []string   `xml:"tag"`
				Episode   *int       `xml:"episode"`
			}
			if err := xml.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.UniqueIDs, []UniqueID{{Type: "tvdb", Default: true, Value: "371065"}, {Type: "tmdb", Value: "74747"}}) || got.Episode != nil {
				t.Errorf("节目自身编号或媒体库数量语义错误：%s", data)
			}
			var wantTags, wantGenres []string
			if tagEnabled {
				wantTags = []string{"Talk Show"}
			}
			if genreEnabled {
				wantGenres = []string{"Talk Show"}
			}
			if !reflect.DeepEqual(got.Tags, wantTags) || !reflect.DeepEqual(got.Genres, wantGenres) {
				t.Errorf("分类和标签开关互相干扰，tag=%v genre=%v：%s", tagEnabled, genreEnabled, data)
			}
		}
	}
}

func TestWritePreservesArtworkSourceURLs(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show, metadata.Episode} {
		t.Run(string(kind), func(t *testing.T) {
			r := &metadata.Record{
				Ref: metadata.Ref{Provider: "tmdb", Kind: kind, ID: "100"}, Title: "图片样本",
				Artwork: []metadata.Artwork{
					{Kind: "poster", URL: "https://example.invalid/poster.jpg"},
					{Kind: "thumb", URL: "https://example.invalid/thumb.jpg"},
					{Kind: "clearlogo", URL: "https://example.invalid/logo.png?a=1&b=2"},
					{Kind: "fanart", URL: "https://example.invalid/fanart.jpg"},
				},
			}
			path := filepath.Join(t.TempDir(), "sample.nfo")
			if err := Write(path, r, "", false, false); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Thumbs []thumb `xml:"thumb"`
				Fanart []thumb `xml:"fanart>thumb"`
			}
			if err := xml.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			wantThumbs := []thumb{
				{Aspect: "poster", URL: "https://example.invalid/poster.jpg"},
				{Aspect: "thumb", URL: "https://example.invalid/thumb.jpg"},
				{Aspect: "clearlogo", URL: "https://example.invalid/logo.png?a=1&b=2"},
			}
			if !reflect.DeepEqual(got.Thumbs, wantThumbs) || !reflect.DeepEqual(got.Fanart, []thumb{{URL: "https://example.invalid/fanart.jpg"}}) {
				t.Errorf("图片来源 URL 或用途映射错误：%s", data)
			}
		})
	}
}
