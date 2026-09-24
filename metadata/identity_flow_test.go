package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestModelHintRequiresSearchConfirmationBeforeSkippingJudge(t *testing.T) {
	p := &testProvider{name: "tmdb"}
	judge := &testJudge{}
	req := Request{Kind: Show, Query: Query{Title: "节目", Year: 2017}, Hints: []Ref{{Provider: "tmdb", Kind: Show, ID: "42"}}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}}
	got, err := NewManager([]Provider{p}, 0, judge).Resolve(context.Background(), req, t.TempDir())
	if err != nil || got == nil || got.Work.Ref.ID != "42" {
		t.Fatalf("搜索核验失败：%+v %v", got, err)
	}
	if p.searchCalls != 1 || judge.calls != 0 || p.fetchCalls != 1 {
		t.Fatalf("应先搜索且命中后只取一次批量详情，无判断调用：search=%d judge=%d fetch=%d", p.searchCalls, judge.calls, p.fetchCalls)
	}
}

func TestRejectedCandidatesExhaustAllSourcesBeforeLocal(t *testing.T) {
	for _, rejection := range []error{ErrNotFound, ErrAmbiguous} {
		t.Run(rejection.Error(), func(t *testing.T) {
			a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
			_, err := NewManager([]Provider{a, b}, 0, &testJudge{err: rejection}).Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "节目"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}}, t.TempDir())
			if !errors.Is(err, ErrSourcesExhausted) || !errors.Is(err, rejection) || b.searchCalls != 1 || b.fetchCalls+a.fetchCalls != 0 {
				t.Fatalf("拒绝未接续全部来源或预取了详情：err=%v a=%+v b=%+v", err, a, b)
			}
		})
	}
}

func TestSeriesFetchedOnlyAfterWorkSelection(t *testing.T) {
	p := &seriesResolveProvider{testProvider: testProvider{name: "tmdb"}}
	_, err := NewManager([]Provider{p}, 0, &testJudge{}).Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "节目"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}, {Season: 2, Episode: 1}}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.batches) != 1 || !p.batches[0].Details {
		t.Fatalf("作品确认前仍请求季集：%+v", p.batches)
	}
}

func TestIdentitySearchDoesNotReusePriorBroadSearchCache(t *testing.T) {
	p := &testProvider{name: "tmdb"}
	root := t.TempDir()
	req := Request{Kind: Movie, Query: Query{Kind: Movie, Title: "作品", Year: 2020}, Hints: []Ref{{Provider: "tmdb", Kind: Movie, ID: "99"}}}
	old := []Candidate{{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "99"}, Title: "旧的宽泛搜索结果", Year: 1990}}
	if err := writeCache(cacheFile(root, p, "search", Request{Kind: Movie, Query: req.Query}), time.Hour, old); err != nil {
		t.Fatal(err)
	}
	judge := &testJudge{}
	got, err := NewManager([]Provider{p}, time.Hour, judge).Resolve(context.Background(), req, root)
	if err != nil || got.Work.Ref.ID != "42" || p.searchCalls != 1 || judge.calls != 1 {
		t.Fatalf("旧搜索语义缓存造成错误编号直通：%+v %v p=%+v judge=%+v", got, err, p, judge)
	}
}

func TestModelHintSelectsMatchedSearchRecordRatherThanFirstCandidate(t *testing.T) {
	p := &controlledProvider{testProvider: &testProvider{name: "tmdb"}, search: func(_ context.Context, q Query) ([]Candidate, error) {
		if q.Title != "作品" || q.Year != 2020 || q.Kind != Movie {
			t.Fatalf("搜索没有使用提取作品信息：%+v", q)
		}
		return []Candidate{{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "41"}, Title: "另一作品", Year: 2020}, {Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}, Title: "作品", Year: 2020}}, nil
	}}
	j := &testJudge{index: 0}
	req := Request{Kind: Movie, Query: Query{Title: "作品", Year: 2020}, Hints: []Ref{{Provider: "tmdb", Kind: Movie, ID: "42"}}}
	got, err := NewManager([]Provider{p}, 0, j).Resolve(context.Background(), req, t.TempDir())
	if err != nil || got.Work.Ref.ID != "42" || p.searchCalls != 1 || p.fetchCalls != 1 || j.calls != 0 {
		t.Fatalf("编号核验选择了首条或跳过真实搜索：%+v %v p=%+v j=%+v", got, err, p, j)
	}
}

func TestInvalidSourceCandidatesAdvanceButInvalidHintsAbort(t *testing.T) {
	cases := []struct {
		name      string
		candidate Candidate
		hints     []Ref
	}{
		{name: "candidate from other source", candidate: Candidate{Ref: Ref{Provider: "thetvdb", Kind: Movie, ID: "42"}, Title: "作品"}},
		{name: "candidate wrong object", candidate: Candidate{Ref: Ref{Provider: "tmdb", Kind: Show, ID: "42"}, Title: "作品"}},
		{name: "candidate missing title", candidate: Candidate{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}}},
		{name: "hint wrong object", candidate: Candidate{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}, Title: "作品"}, hints: []Ref{{Provider: "tmdb", Kind: Show, ID: "42"}}},
		{name: "conflicting hints", candidate: Candidate{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}, Title: "作品"}, hints: []Ref{{Provider: "tmdb", Kind: Movie, ID: "42"}, {Provider: "tmdb", Kind: Movie, ID: "43"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &controlledProvider{testProvider: &testProvider{name: "tmdb"}, search: func(context.Context, Query) ([]Candidate, error) { return []Candidate{tc.candidate}, nil }}
			next := &testProvider{name: "thetvdb"}
			j := &testJudge{}
			got, err := NewManager([]Provider{p, next}, 0, j).Resolve(context.Background(), Request{Kind: Movie, Query: Query{Title: "作品"}, Hints: tc.hints}, t.TempDir())
			if len(tc.hints) > 0 {
				if got != nil || err == nil || errors.Is(err, ErrSourcesExhausted) || p.searchCalls+p.fetchCalls+j.calls+next.searchCalls+next.fetchCalls != 0 {
					t.Fatalf("非法输入未先行终止：%+v %v p=%+v next=%+v", got, err, p, next)
				}
				return
			}
			if err != nil || got == nil || got.Work.Ref.Provider != "thetvdb" || p.fetchCalls != 0 || j.calls != 1 || next.searchCalls != 1 || next.fetchCalls != 1 {
				t.Fatalf("非法来源候选未继续下一来源：%+v %v p=%+v next=%+v", got, err, p, next)
			}
		})
	}
}
