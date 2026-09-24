package thetvdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func serveSearch(t *testing.T, handler func(http.ResponseWriter, searchParams)) *Provider {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL='%s/web/search/queries'</script>`, server.URL)
			return
		}
		var body searchPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Requests) != 1 {
			t.Errorf("无效公开搜索请求：%+v %v", body, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler(w, body.Requests[0].Params)
	}))
	t.Cleanup(server.Close)
	return New(server.Client(), server.URL, "en", 0)
}

func TestSearchCollectsEveryTitleAndPage(t *testing.T) {
	var requests []string
	p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
		requests = append(requests, params.Query+"|"+strconv.Itoa(params.Page))
		var hits []map[string]any
		if params.Page == 0 {
			for id := 1; id <= 20; id++ {
				hits = append(hits, map[string]any{"id": id, "name": "Other", "slug": "original", "type": "series"})
			}
		} else {
			id := map[string]int{"文件名": 21, "中文名": 22, "Original": 23}[params.Query]
			hits = []map[string]any{{"id": 20, "name": "Other", "slug": "changed", "type": "series"}, {"id": id, "name": "Target", "slug": "target", "type": "series", "year": "2024"}}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"page": params.Page, "nbPages": 2, "nbHits": 22, "hitsPerPage": 20, "exhaustiveNbHits": true, "hits": hits}}}); err != nil {
			t.Error(err)
		}
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: " 文件名 ", ChineseTitle: "中文名", OriginalTitle: "Original"})
	if err != nil || len(got) != 23 {
		t.Fatalf("未按节目编号收集全部标题和分页：%d %+v %v", len(got), got, err)
	}
	if got[19].Ref.Slug != "original" || got[22].Ref.ID != "23" || got[22].Year != 2024 {
		t.Fatalf("去重后节目事实错误：%+v", got)
	}
	want := []string{"文件名|0", "文件名|1", "中文名|0", "中文名|1", "Original|0", "Original|1"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("请求未覆盖全部标题和分页：%v", requests)
	}
}

func TestSearchDeduplicatesTrimmedTitles(t *testing.T) {
	var requests []string
	p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
		requests = append(requests, params.Query)
		fmt.Fprint(w, `{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[{"id":1,"name":"Target","type":"series"}]}]}`)
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: " Target ", ChineseTitle: "Target", OriginalTitle: "Target "})
	if err != nil || len(got) != 1 || !reflect.DeepEqual(requests, []string{"Target"}) {
		t.Fatalf("标题去重不正确：%+v %v %v", got, requests, err)
	}
}

func TestSearchRejectsIncompletePagination(t *testing.T) {
	const hit = `[{"id":1,"name":"Target","type":"series"}]`
	for _, tc := range []struct{ name, first, second string }{
		{"missing_metadata", `{"exhaustiveNbHits":true,"hits":` + hit + `}`, ""},
		{"missing_page", `{"exhaustiveNbHits":true,"nbPages":1,"nbHits":1,"hitsPerPage":20,"hits":` + hit + `}`, ""},
		{"wrong_page", `{"exhaustiveNbHits":true,"page":1,"nbPages":1,"nbHits":1,"hitsPerPage":20,"hits":` + hit + `}`, ""},
		{"zero_pages_with_hit", `{"exhaustiveNbHits":true,"page":0,"nbPages":0,"nbHits":1,"hitsPerPage":20,"hits":` + hit + `}`, ""},
		{"wrong_page_capacity", `{"exhaustiveNbHits":true,"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":0,"hits":` + hit + `}`, ""},
		{"short_total", `{"exhaustiveNbHits":true,"page":0,"nbPages":1,"nbHits":2,"hitsPerPage":20,"hits":` + hit + `}`, ""},
		{"empty_with_total", `{"exhaustiveNbHits":true,"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"hits":[]}`, ""},
		{"missing_exhaustive", `{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"hits":` + hit + `}`, ""},
		{"non_exhaustive", `{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":false,"hits":` + hit + `}`, ""},
		{"page_limit", `{"exhaustiveNbHits":true,"page":0,"nbPages":255,"nbHits":255,"hitsPerPage":1,"hits":` + hit + `}`, ""},
		{"repeated_page_number", `{"exhaustiveNbHits":true,"page":0,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`, `{"exhaustiveNbHits":true,"page":0,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`},
		{"repeated_candidates", `{"exhaustiveNbHits":true,"page":0,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`, `{"exhaustiveNbHits":true,"page":1,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`},
		{"changed_totals", `{"exhaustiveNbHits":true,"page":0,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`, `{"exhaustiveNbHits":true,"page":1,"nbPages":3,"nbHits":3,"hitsPerPage":1,"hits":[{"id":2,"name":"Target","type":"series"}]}`},
		{"empty_next_page", `{"exhaustiveNbHits":true,"page":0,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":` + hit + `}`, `{"exhaustiveNbHits":true,"page":1,"nbPages":2,"nbHits":2,"hitsPerPage":1,"hits":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
				body := tc.first
				if params.Page == 1 {
					body = tc.second
				}
				fmt.Fprintf(w, `{"results":[%s]}`, body)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Target"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("异常分页不得返回部分候选或未找到：%+v %v", got, err)
			}
		})
	}
}

func TestSearchCandidateLimit(t *testing.T) {
	for _, count := range []int{254, 255} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
				var hits []map[string]any
				for id := params.Page*20 + 1; id <= min((params.Page+1)*20, count); id++ {
					hits = append(hits, map[string]any{"id": id, "name": "Target", "type": "series"})
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"page": params.Page, "nbPages": (count + 19) / 20, "nbHits": count, "hitsPerPage": 20, "exhaustiveNbHits": true, "hits": hits}}}); err != nil {
					t.Error(err)
				}
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Target"})
			if count == 254 && (err != nil || len(got) != 254) {
				t.Fatalf("上限内候选被截断：%d %v", len(got), err)
			}
			if count == 255 && (err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0) {
				t.Fatalf("超限候选不得截断后成功：%d %v", len(got), err)
			}
		})
	}
}

func TestSearchLimitsCombinedTitles(t *testing.T) {
	for _, total := range []int{254, 255} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
				count, offset := 128, 0
				if params.Query == "Second" {
					count, offset = total-128, 128
				}
				var hits []map[string]any
				for id := params.Page*20 + 1; id <= min((params.Page+1)*20, count); id++ {
					hits = append(hits, map[string]any{"id": offset + id, "name": "Target", "type": "series"})
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"page": params.Page, "nbPages": (count + 19) / 20, "nbHits": count, "hitsPerPage": 20, "exhaustiveNbHits": true, "hits": hits}}}); err != nil {
					t.Error(err)
				}
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "First", OriginalTitle: "Second"})
			if total == 254 && (err != nil || len(got) != 254) {
				t.Fatalf("多标题合并的上限内候选被截断：%d %v", len(got), err)
			}
			if total == 255 && (err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0) {
				t.Fatalf("多标题合并超限不得返回部分结果：%d %v", len(got), err)
			}
		})
	}
}

func TestSearchLaterQueryErrorDiscardsPartialCandidates(t *testing.T) {
	for _, status := range []int{200, 403, 404, 429, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
				if params.Query == "First" {
					fmt.Fprint(w, `{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[{"id":1,"name":"Target","type":"series"}]}]}`)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{`)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "First", OriginalTitle: "Second"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("后续查询失败不得接受前面的候选：%+v %v", got, err)
			}
		})
	}
}

func TestSearchAllEmptyTitlesReturnNotFound(t *testing.T) {
	var requests []string
	p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
		requests = append(requests, params.Query)
		fmt.Fprint(w, `{"results":[{"page":0,"nbPages":0,"nbHits":0,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[]}]}`)
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, ChineseTitle: "中文", OriginalTitle: "Original"})
	if !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 || !reflect.DeepEqual(requests, []string{"中文", "Original"}) {
		t.Fatalf("应查询全部可用标题后明确报告未找到：%+v %v %v", got, requests, err)
	}
}

func TestSearch404AtEntryOrFirstPageIsFailure(t *testing.T) {
	for _, missingPath := range []string{"/search", "/web/search/queries"} {
		t.Run(missingPath, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == missingPath {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL='%s/web/search/queries'</script>`, server.URL)
			}))
			defer server.Close()
			got, err := New(server.Client(), server.URL, "en", 0).Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Target"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("搜索入口或首个接口404应为故障：%+v %v", got, err)
			}
		})
	}
}

func TestSearch404OnLaterPageIsFailure(t *testing.T) {
	p := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
		if params.Page == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var hits []map[string]any
		for id := 1; id <= 20; id++ {
			hits = append(hits, map[string]any{"id": id, "type": "series", "name": "Target"})
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"page": 0, "nbPages": 2, "nbHits": 21, "hitsPerPage": 20, "exhaustiveNbHits": true, "hits": hits}}}); err != nil {
			t.Error(err)
		}
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Target"})
	if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
		t.Fatalf("第二页404应为搜索故障而非无候选：%+v %v", got, err)
	}
}
