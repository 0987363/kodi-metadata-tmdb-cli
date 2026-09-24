package thetvdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestSearchPublicEndpointReturnsAllCandidates(t *testing.T) {
	results := fixture(t, "search.json")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("query") != "Alone" {
				t.Errorf("搜索标题错误：%s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `<html><script>window.TVDB_SEARCH_URL = '%s/web/search/queries';</script></html>`, server.URL)
		case "/web/search/queries":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") == "" {
				t.Errorf("搜索请求协议错误：%s %+v", r.Method, r.Header)
			}
			var body struct {
				Requests []struct {
					IndexName string `json:"indexName"`
					Params    struct {
						Query       string `json:"query"`
						Filters     string `json:"filters"`
						HitsPerPage int    `json:"hitsPerPage"`
						Page        int    `json:"page"`
					} `json:"params"`
				} `json:"requests"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Requests) != 1 || body.Requests[0].IndexName != "TVDB" || body.Requests[0].Params.Query != "Alone" || body.Requests[0].Params.Filters != "type:series" || body.Requests[0].Params.HitsPerPage != 20 || body.Requests[0].Params.Page != 0 {
				t.Errorf("搜索body错误：%+v", body)
			}
			fmt.Fprint(w, results)
		default:
			t.Errorf("意外请求：%s", r.URL.Path)
		}
	}))
	defer server.Close()
	provider := New(server.Client(), server.URL, "en-US", 0)
	result, err := provider.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Alone"})
	if err != nil {
		t.Fatal(err)
	}
	want := []metadata.Candidate{
		{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "295936", Slug: "alone"}, Title: "Alone", OriginalTitle: "Alone", Year: 2015},
		{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "336604", Slug: "alone-together"}, Title: "Alone Together", OriginalTitle: "Alone Together", Year: 2018},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("候选未按真实响应完整返回：got=%+v want=%+v", result, want)
	}
}

func TestSearchRejectsEpisodeBeforeNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("单集搜索不应请求网络")
	}))
	defer server.Close()
	_, err := New(server.Client(), server.URL, "en-US", 0).Search(context.Background(), metadata.Query{Kind: metadata.Episode, Title: "Alone"})
	if !errors.Is(err, metadata.ErrUnsupported) {
		t.Fatalf("单集仅支持通过节目定位获取详情：%v", err)
	}
}

func TestSearchDistinguishesEmptyResultsAndBrokenResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		notFound   bool
	}{{"空候选", `{"results":[{"page":0,"nbPages":0,"nbHits":0,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[]}]}`, true}, {"缺少结构", `{"results":[{}]}`, false}, {"登录页面", `<h1>Login</h1>`, false}, {"错误节目编号", `{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[{"id":0,"type":"series","name":"Alone"}]}]}`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search" {
					fmt.Fprintf(w, `<script>window.TVDB_SEARCH_URL='%s/web/search/queries'</script>`, server.URL)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := New(server.Client(), server.URL, "en", 0).Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Alone"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) != tc.notFound {
				t.Fatalf("错误分类不符：%v", err)
			}
		})
	}
}

func TestRejectSearchEndpointOutsideSite(t *testing.T) {
	server := servePages(t, map[string]string{"/search": `<script>window.TVDB_SEARCH_URL='https://thetvdb.com.evil.example/web/search/queries'</script>`})
	_, err := New(server.Client(), server.URL, "en", 0).Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Alone"})
	if err == nil || errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("不得向非站点搜索接口发请求：%v", err)
	}
}

func TestLanguageNeverInventsMissingTranslation(t *testing.T) {
	server := servePages(t, map[string]string{"/series/26882341-show": fixture(t, "series.html")})
	result, err := New(server.Client(), server.URL, "en-US", 0).Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Slug: "26882341-show"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "坑王驾到" || result.Plot != "" || strings.Join(result.Languages, ",") != "Chinese - China" {
		t.Fatalf("不应虚构英语翻译：%+v", result)
	}
}

func TestSearchRejectsMalformedFilteredHits(t *testing.T) {
	for _, hit := range []string{`null`, `{}`, `{"id":1,"name":"Target"}`, `{"id":1,"name":"Target","type":"unknown"}`, `{"id":1,"name":"Target","type":"movie"}`} {
		t.Run(hit, func(t *testing.T) {
			provider := serveSearch(t, func(w http.ResponseWriter, params searchParams) {
				fmt.Fprintf(w, `{"results":[{"page":0,"nbPages":1,"nbHits":1,"hitsPerPage":20,"exhaustiveNbHits":true,"hits":[%s]}]}`, hit)
			})
			got, err := provider.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "Target"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("畸形节目候选不得视为空结果：%+v %v", got, err)
			}
		})
	}
}
