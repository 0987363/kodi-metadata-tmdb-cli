package thetvdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func batchPages() map[string]string {
	base := "/series/26882341-show"
	return map[string]string{
		base: fixtureSeries,
		base + "/allseasons/official": `<h1>All Seasons</h1><ul class="list-group">` +
			`<li class="list-group-item"><h4><span class="episode-label">S01E01</span><a href="/series/26882341-show/episodes/101">One</a></h4><ul class="list-inline"><li>January 2, 2020</li><li>ABC</li></ul><div class="list-group-item-text"><p>English plot one</p><img data-src="https://artworks.thetvdb.com/banners/episodes/101.jpg"></div></li>` +
			`<li class="list-group-item"><h4><span class="episode-label">S01E02</span><a href="/series/26882341-show/episodes/102">Two</a></h4><ul class="list-inline"><li>ABC</li></ul><div class="list-group-item-text"><p>English plot two</p></div></li>` +
			`<li class="list-group-item"><h4><span class="episode-label">S02E01</span><a href="/series/26882341-show/episodes/201">Three</a></h4><ul class="list-inline"><li>XYZ</li></ul><div class="list-group-item-text"><p>English plot three</p></div></li></ul>`,
		base + "/seasons/official/1": `<div id="episodes"><table><thead><tr><th></th><th>Name</th><th>First Aired</th><th>Runtime</th><th>Image</th></tr></thead><tbody><tr><td>S01E01</td><td><a href="/series/26882341-show/episodes/101">One</a></td><td></td><td>22</td><td></td></tr><tr><td>S01E02</td><td><a href="/series/26882341-show/episodes/102">Two</a></td><td></td><td>23</td><td></td></tr></tbody></table></div>`,
		base + "/seasons/official/2": `<div id="episodes"><table><thead><tr><th></th><th>Name</th><th>First Aired</th><th>Runtime</th><th>Image</th></tr></thead><tbody><tr><td>S02E01</td><td><a href="/series/26882341-show/episodes/201">Three</a></td><td></td><td>24</td><td></td></tr></tbody></table></div>`,
		base + "/episodes/101":       `<a data-type="episode" data-id="101"></a><h1 class="translated_title">One</h1><div id="translations"><div class="change_translation_text" data-language="zho" data-title="第一集"><p></p></div></div><div id="general"><li><strong>On Other Sites</strong><a href="https://www.imdb.com/title/tt1234567">IMDb</a></li></div>`,
		base + "/episodes/102":       `<a data-type="episode" data-id="102"></a><h1 class="translated_title">Two</h1><div id="general"></div>`,
		base + "/episodes/201":       `<a data-type="episode" data-id="201"></a><h1 class="translated_title">Three</h1><div id="general"></div>`,
	}
}

var fixtureSeries = `<h1 id="series_title">Show</h1><div id="series_basic_info"><li><strong>TheTVDB.com Series ID</strong><span>371065</span></li></div>`

func batchServer(t *testing.T, pages map[string]string) (*httptest.Server, map[string]int) {
	t.Helper()
	counts := make(map[string]int)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		page, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)
	return server, counts
}

func TestFetchSeriesBatchAndDetailsReuse(t *testing.T) {
	server, counts := batchServer(t, batchPages())
	p := New(server.Client(), server.URL, "zh-CN", 0)
	req := metadata.SeriesRequest{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 2, Episode: 1}, {Season: 1, Episode: 1}}}
	basic, err := p.FetchSeries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if basic.Work.Ref.ID != "371065" || len(basic.Episodes) != 3 {
		t.Fatalf("结果错误: %+v", basic)
	}
	first := basic.Episodes[0].Record
	if first.Ref.ID != "101" || first.Plot != "English plot one" || first.Premiered != "2020-01-02" || first.RuntimeMinutes != 22 || len(first.Artwork) != 1 || first.Studios[0] != "ABC" {
		t.Fatalf("批量事实错误: %+v", first)
	}
	if counts["/series/26882341-show"] != 1 || counts["/series/26882341-show/allseasons/official"] != 1 || counts["/series/26882341-show/seasons/official/1"] != 1 || counts["/series/26882341-show/seasons/official/2"] != 1 || counts["/series/26882341-show/episodes/101"] != 0 {
		t.Fatalf("基础请求次数错误: %v", counts)
	}
	req.Details = true
	req.Ref = basic.Work.Ref
	detailed, err := p.FetchSeries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	first = detailed.Episodes[0].Record
	if first.Title != "第一集" || first.Plot != "English plot one" || first.RuntimeMinutes != 22 || len(first.Artwork) != 1 || len(first.ExternalIDs) != 2 {
		t.Fatalf("详情合并错误: %+v", first)
	}
	if _, err := p.FetchSeries(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for path, count := range counts {
		if count != 1 {
			t.Fatalf("重复抓取 %s: %d", path, count)
		}
	}
}

func TestFetchSeriesRejectsInvalidJoins(t *testing.T) {
	base := "/series/26882341-show"
	for name, change := range map[string]func(map[string]string){
		"id mismatch": func(p map[string]string) {
			p[base+"/seasons/official/1"] = strings.Replace(p[base+"/seasons/official/1"], "/episodes/101", "/episodes/999", 1)
		},
		"missing row": func(p map[string]string) {
			p[base+"/seasons/official/1"] = strings.Replace(p[base+"/seasons/official/1"], "<tr><td>S01E02", "<tr><td>S01E99", 1)
		},
		"duplicate row": func(p map[string]string) {
			p[base+"/seasons/official/1"] = strings.Replace(p[base+"/seasons/official/1"], "</tbody>", `<tr><td>S01E01</td><td><a href="/series/26882341-show/episodes/101">One</a></td><td></td><td>22</td><td></td></tr></tbody>`, 1)
		},
		"malformed page": func(p map[string]string) {
			p[base+"/seasons/official/1"] = `<h1>Login</h1>`
		},
	} {
		t.Run(name, func(t *testing.T) {
			pages := batchPages()
			change(pages)
			server, _ := batchServer(t, pages)
			_, err := New(server.Client(), server.URL, "en", 0).FetchSeries(context.Background(), metadata.SeriesRequest{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}}})
			if err == nil {
				t.Fatal("应拒绝无效关联")
			}
		})
	}
}

func TestFetchSeriesRejectsAmbiguousEpisodeCoordinates(t *testing.T) {
	pages := batchPages()
	path := "/series/26882341-show/allseasons/official"
	pages[path] = strings.TrimSuffix(pages[path], "</ul>") + `<li class="list-group-item"><h4><span class="episode-label">S01E01</span><a href="/series/26882341-show/episodes/999">Other</a></h4></li></ul>`
	server, counts := batchServer(t, pages)
	_, err := New(server.Client(), server.URL, "en", 0).FetchSeries(context.Background(), metadata.SeriesRequest{
		Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}},
	})
	if !errors.Is(err, metadata.ErrAmbiguous) || counts["/series/26882341-show/seasons/official/1"] != 0 {
		t.Fatalf("重复季集号应在读取季详情前拒绝：%v %v", err, counts)
	}
}

func TestFetchSeriesRejectsEpisodeDetailIdentityMismatch(t *testing.T) {
	pages := batchPages()
	pages["/series/26882341-show/episodes/101"] = strings.Replace(pages["/series/26882341-show/episodes/101"], `data-id="101"`, `data-id="999"`, 1)
	server, _ := batchServer(t, pages)
	_, err := New(server.Client(), server.URL, "zh-CN", 0).FetchSeries(context.Background(), metadata.SeriesRequest{
		Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}, Details: true,
	})
	if err == nil || errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("详情编号错配应为结构错误：%v", err)
	}
}

func TestFetchSeriesPreservesSpecialEpisodeCoordinates(t *testing.T) {
	pages := batchPages()
	base := "/series/26882341-show"
	pages[base+"/allseasons/official"] = strings.TrimSuffix(pages[base+"/allseasons/official"], "</ul>") + `<li class="list-group-item"><h4><span class="episode-label">S00E01</span><a href="/series/26882341-show/episodes/9000000">Special</a></h4><ul class="list-inline"><li>September 25, 2016</li></ul></li></ul>`
	pages[base+"/seasons/official/0"] = `<div id="episodes"><table><thead><tr><th></th><th>Name</th><th>First Aired</th><th>Runtime</th><th>Image</th></tr></thead><tbody><tr><td>S00E01</td><td><a href="/series/26882341-show/episodes/9000000">Special</a></td><td></td><td>30</td><td></td></tr></tbody></table></div>`
	server, _ := batchServer(t, pages)
	got, err := New(server.Client(), server.URL, "en", 0).FetchSeries(context.Background(), metadata.SeriesRequest{
		Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 0, Episode: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := got.Episodes[0].Record
	if record.Ref.ID != "9000000" || record.SeasonNumber != 0 || record.EpisodeNumber != 1 || record.Premiered != "2016-09-25" {
		t.Fatalf("特别篇身份或坐标错误：%+v", record)
	}
}

func TestFetchSeriesRejectsGroupsAndMissingSource(t *testing.T) {
	server, counts := batchServer(t, batchPages())
	p := New(server.Client(), server.URL, "en", 0)
	ref := metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: ref, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1, Group: "alternate"}}})
	if !errors.Is(err, metadata.ErrUnsupported) || len(counts) != 0 {
		t.Fatalf("分组应在请求前拒绝: %v %v", err, counts)
	}
	_, err = p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: ref, Episodes: []metadata.EpisodeKey{{Season: 9, Episode: 1}}})
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("缺失坐标应终止: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("缺失坐标不应继续抓季页: %v", counts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.FetchSeries(ctx, metadata.SeriesRequest{Ref: ref, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}})
	if !errors.Is(err, context.Canceled) || len(counts) != 2 {
		t.Fatalf("取消后不应发起请求: %v %v", err, counts)
	}
}
