package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestMetadataSearchCollectsEveryTitleAndPage(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
		t.Run(string(kind), func(t *testing.T) {
			var requests []string
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				args := r.URL.Query()
				requests = append(requests, args.Get("query")+"|"+args.Get("page"))
				id := map[string]int{"文件名": 2, "中文名": 3, "Original": 4}[args.Get("query")]
				if args.Get("page") == "1" {
					fmt.Fprint(w, `{"page":1,"total_pages":2,"total_results":3,"results":[{"id":1,"title":"其他","name":"其他"}]}`)
					return
				}
				fmt.Fprintf(w, `{"page":2,"total_pages":2,"total_results":3,"results":[{"id":1,"title":"重复","name":"重复"},{"id":%d,"title":"目标","name":"目标","original_title":"Target","original_name":"Target","release_date":"2024-01-01","first_air_date":"2024-01-01"}]}`, id)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: kind, Title: " 文件名 ", ChineseTitle: "中文名", OriginalTitle: "Original"})
			if err != nil || len(got) != 4 {
				t.Fatalf("未收集三种标题和第二页候选：%+v %v", got, err)
			}
			if got[0].Title != "其他" || got[3].Ref.ID != "4" || got[3].OriginalTitle != "Target" || got[3].Year != 2024 {
				t.Fatalf("去重后候选事实错误：%+v", got)
			}
			want := []string{"文件名|1", "文件名|2", "中文名|1", "中文名|2", "Original|1", "Original|2"}
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("请求未覆盖所有明确标题的全部页：%v", requests)
			}
		})
	}
}

func TestMetadataSearchDeduplicatesTrimmedTitles(t *testing.T) {
	var requests []string
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query().Get("query")+"|"+r.URL.Query().Get("primary_release_year"))
		fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":1,"title":"目标"}]}`)
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: " 目标 ", ChineseTitle: "目标", OriginalTitle: "目标 ", Year: 2024})
	if err != nil || len(got) != 1 || !reflect.DeepEqual(requests, []string{"目标|2024"}) {
		t.Fatalf("标题去重或已知年份约束不正确：%+v %v %v", got, requests, err)
	}
}

func TestMetadataSearchRejectsIncompletePagination(t *testing.T) {
	const hit = `[{"id":1,"title":"目标","name":"目标"}]`
	for _, tc := range []struct {
		name   string
		first  string
		second string
	}{
		{"missing_metadata", `{"results":` + hit + `}`, ""},
		{"missing_total", `{"page":1,"total_pages":1,"results":` + hit + `}`, ""},
		{"wrong_first_page", `{"page":0,"total_pages":1,"total_results":1,"results":` + hit + `}`, ""},
		{"zero_pages_with_hit", `{"page":1,"total_pages":0,"total_results":1,"results":` + hit + `}`, ""},
		{"negative_total", `{"page":1,"total_pages":1,"total_results":-1,"results":` + hit + `}`, ""},
		{"short_total", `{"page":1,"total_pages":1,"total_results":2,"results":` + hit + `}`, ""},
		{"empty_with_total", `{"page":1,"total_pages":1,"total_results":1,"results":[]}`, ""},
		{"page_limit", `{"page":1,"total_pages":255,"total_results":255,"results":` + hit + `}`, ""},
		{"repeated_page_number", `{"page":1,"total_pages":2,"total_results":2,"results":` + hit + `}`, `{"page":1,"total_pages":2,"total_results":2,"results":` + hit + `}`},
		{"repeated_candidates", `{"page":1,"total_pages":2,"total_results":2,"results":` + hit + `}`, `{"page":2,"total_pages":2,"total_results":2,"results":` + hit + `}`},
		{"changed_totals", `{"page":1,"total_pages":2,"total_results":2,"results":` + hit + `}`, `{"page":2,"total_pages":3,"total_results":3,"results":[{"id":2,"title":"目标","name":"目标"}]}`},
		{"empty_next_page", `{"page":1,"total_pages":2,"total_results":2,"results":` + hit + `}`, `{"page":2,"total_pages":2,"total_results":2,"results":[]}`},
	} {
		for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
			t.Run(tc.name+"/"+string(kind), func(t *testing.T) {
				p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("page") == "2" {
						fmt.Fprint(w, tc.second)
						return
					}
					fmt.Fprint(w, tc.first)
				})
				got, err := p.Search(context.Background(), metadata.Query{Kind: kind, Title: "目标"})
				if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
					t.Fatalf("异常分页不得返回部分候选或未找到：%+v %v", got, err)
				}
			})
		}
	}
}

func TestMetadataSearchCandidateLimit(t *testing.T) {
	for _, count := range []int{254, 255} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				var hits []map[string]any
				for id := (page-1)*20 + 1; id <= min(page*20, count); id++ {
					hits = append(hits, map[string]any{"id": id, "title": "候选"})
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"page": page, "total_pages": (count + 19) / 20, "total_results": count, "results": hits}); err != nil {
					t.Error(err)
				}
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "目标"})
			if count == 254 && (err != nil || len(got) != 254) {
				t.Fatalf("上限内候选被截断：%d %v", len(got), err)
			}
			if count == 255 && (err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0) {
				t.Fatalf("超限候选不得截断后成功：%d %v", len(got), err)
			}
		})
	}
}

func TestMetadataSearchLimitsCombinedTitles(t *testing.T) {
	for _, total := range []int{254, 255} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				count, offset := 128, 0
				if r.URL.Query().Get("query") == "Second" {
					count, offset = total-128, 128
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				var hits []map[string]any
				for id := (page-1)*20 + 1; id <= min(page*20, count); id++ {
					hits = append(hits, map[string]any{"id": offset + id, "title": "候选"})
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"page": page, "total_pages": (count + 19) / 20, "total_results": count, "results": hits}); err != nil {
					t.Error(err)
				}
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "First", OriginalTitle: "Second"})
			if total == 254 && (err != nil || len(got) != 254) {
				t.Fatalf("多标题合并的上限内候选被截断：%d %v", len(got), err)
			}
			if total == 255 && (err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0) {
				t.Fatalf("多标题合并超限不得返回部分结果：%d %v", len(got), err)
			}
		})
	}
}

func TestMetadataSearchLaterQueryErrorDiscardsPartialCandidates(t *testing.T) {
	for _, status := range []int{200, 403, 404, 429, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("query") == "First" {
					fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":1,"title":"候选"}]}`)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{`)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "First", OriginalTitle: "Second"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("后续查询失败不得接受前面的候选：%+v %v", got, err)
			}
		})
	}
}

func TestMetadataSearchAllEmptyTitlesReturnNotFound(t *testing.T) {
	var requests []string
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query().Get("query"))
		fmt.Fprint(w, `{"page":1,"total_pages":0,"total_results":0,"results":[]}`)
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, ChineseTitle: "中文", OriginalTitle: "Original"})
	if !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 || !reflect.DeepEqual(requests, []string{"中文", "Original"}) {
		t.Fatalf("应查询全部可用标题后明确报告未找到：%+v %v %v", got, requests, err)
	}
}

func TestMetadataSearch404OnLaterPageIsFailure(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `{"page":1,"total_pages":2,"total_results":2,"results":[{"id":1,"title":"候选"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	got, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "Target"})
	if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
		t.Fatalf("第二页404应为搜索故障而非无候选：%+v %v", got, err)
	}
}
