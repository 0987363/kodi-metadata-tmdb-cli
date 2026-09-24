package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type judgeFunc func(context.Context, Request, []Candidate) (int, error)

func (f judgeFunc) Select(ctx context.Context, req Request, options []Candidate) (int, error) {
	return f(ctx, req, options)
}

type controlledProvider struct {
	*testProvider
	search func(context.Context, Query) ([]Candidate, error)
	fetch  func(context.Context, Request) (*Record, error)
}

func (p *controlledProvider) Search(ctx context.Context, query Query) ([]Candidate, error) {
	if p.search != nil {
		p.searchCalls++
		return p.search(ctx, query)
	}
	return p.testProvider.Search(ctx, query)
}

func (p *controlledProvider) Fetch(ctx context.Context, req Request) (*Record, error) {
	if p.fetch != nil {
		p.fetchCalls++
		return p.fetch(ctx, req)
	}
	return p.testProvider.Fetch(ctx, req)
}

func (p *controlledProvider) FetchSeries(ctx context.Context, req SeriesRequest) (*SeriesResult, error) {
	if p.fetch == nil {
		return p.testProvider.FetchSeries(ctx, req)
	}
	p.fetchCalls++
	work, err := p.fetch(ctx, Request{Kind: Show, Ref: req.Ref})
	if err != nil {
		return nil, err
	}
	result := &SeriesResult{Work: work}
	for _, key := range UniqueEpisodeKeys(req.Episodes) {
		episode, err := p.fetch(ctx, Request{Kind: Episode, Ref: req.Ref, Season: key.Season, Episode: key.Episode, Group: key.Group})
		if err != nil {
			return nil, err
		}
		result.Episodes = append(result.Episodes, SeriesEpisode{Key: key, Record: episode})
	}
	return result, nil
}

func TestResolveUnconfirmedSourcesExhaustWithoutFetchingDetails(t *testing.T) {
	for _, unconfirmed := range []error{ErrNotFound, ErrAmbiguous} {
		t.Run(unconfirmed.Error(), func(t *testing.T) {
			a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
			calls := 0
			judge := judgeFunc(func(_ context.Context, _ Request, candidates []Candidate) (int, error) {
				calls++
				if len(candidates) != 1 || a.fetchCalls+b.fetchCalls != 0 || calls == 1 && b.searchCalls != 0 {
					t.Fatalf("判断前访问了详情或后续来源：%+v a=%+v b=%+v", candidates, a, b)
				}
				return -1, fmt.Errorf("未确认: %w", unconfirmed)
			})
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 2}}}, t.TempDir())
			if got != nil || !errors.Is(err, unconfirmed) || !errors.Is(err, ErrSourcesExhausted) || calls != 2 || b.searchCalls != 1 {
				t.Fatalf("逐源判断拒绝后未保留耗尽原因：%+v %v calls=%d", got, err, calls)
			}
		})
	}
}

func TestResolveModelHintsAreSourceScopedAndUnconfirmedHintUsesJudge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hints     []Ref
		wantJudge int
	}{
		{"same source matches", []Ref{{Provider: "tmdb", Kind: Show, ID: "42"}}, 0},
		{"other source same id", []Ref{{Provider: "thetvdb", Kind: Show, ID: "42"}}, 1},
		{"wrong id uses search results", []Ref{{Provider: "tmdb", Kind: Show, ID: "11"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
			j := &testJudge{}
			req := Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}, Hints: tc.hints}
			got, err := NewManager([]Provider{a, b}, 0, j).Resolve(context.Background(), req, t.TempDir())
			if err != nil || got.Work.Ref.ID != "42" || a.searchCalls != 1 || a.fetchCalls != 1 || j.calls != tc.wantJudge || b.searchCalls+b.fetchCalls != 0 {
				t.Fatalf("编号作用域或搜索优先错误：%+v %v a=%+v b=%+v judge=%+v", got, err, a, b, j)
			}
		})
	}
}
func TestResolveEmptySearchAdvances(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			a := &controlledProvider{testProvider: &testProvider{name: "tmdb"}, search: func(context.Context, Query) ([]Candidate, error) {
				if empty {
					return nil, nil
				}
				return nil, ErrNotFound
			}}
			b := &testProvider{name: "thetvdb"}
			j := &testJudge{}
			got, err := NewManager([]Provider{a, b}, 0, j).Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 2}}}, t.TempDir())
			if err != nil || got.Work.Ref.Provider != "thetvdb" || j.calls != 1 || a.fetchCalls != 0 || b.fetchCalls != 1 {
				t.Fatalf("正常空搜索未继续：%+v %v a=%+v b=%+v", got, err, a, b)
			}
		})
	}
}
func TestResolveSelectedDetailsFailureAdvances(t *testing.T) {
	for _, failure := range []error{ErrNotFound, ErrConstraintMismatch, errors.New("HTTP 500")} {
		t.Run(failure.Error(), func(t *testing.T) {
			a, b := &testProvider{name: "tmdb", fetchError: failure}, &testProvider{name: "thetvdb"}
			j := &testJudge{}
			got, err := NewManager([]Provider{a, b}, 0, j).Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}}, t.TempDir())
			if err != nil || got == nil || got.Work.Ref.Provider != "thetvdb" || j.calls != 2 || b.searchCalls != 1 || b.fetchCalls != 1 {
				t.Fatalf("选中详情错误未接续：%+v %v a=%+v b=%+v", got, err, a, b)
			}
		})
	}
}
func TestResolveErrorsAreRetainedAfterSourcesExhausted(t *testing.T) {
	requestError := errors.New("HTTP 403")
	protocolError := errors.New("invalid JSON")
	for _, tc := range []struct {
		name      string
		searchErr error
		fetchErr  error
		judgeErr  error
		choice    int
	}{
		{name: "search_request", searchErr: requestError},
		{name: "search_ambiguous", searchErr: ErrAmbiguous},
		{name: "fetch_request", fetchErr: requestError},
		{name: "fetch_ambiguous", fetchErr: ErrAmbiguous},
		{name: "fetch_protocol", fetchErr: protocolError},
		{name: "judge_request", judgeErr: requestError},
		{name: "judge_protocol", judgeErr: protocolError},
		{name: "choice_out_of_bounds", choice: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &testProvider{name: "tmdb", err: tc.searchErr, fetchError: tc.fetchErr}
			b, judge := &testProvider{name: "thetvdb", err: tc.searchErr, fetchError: tc.fetchErr}, &testJudge{err: tc.judgeErr, index: tc.choice}
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), Request{Kind: Movie, Query: Query{Title: "作品"}}, t.TempDir())
			if got != nil || !errors.Is(err, ErrSourcesExhausted) || b.searchCalls != 1 {
				t.Fatalf("来源故障没有接续并保留耗尽信号：%+v %v %+v", got, err, b)
			}
			for _, want := range []error{tc.searchErr, tc.fetchErr, tc.judgeErr} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("原始错误丢失：%v，期望 %v", err, want)
				}
			}
		})
	}
}

func TestResolveManualSourceNeverFallsThrough(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ref       Ref
		searchErr error
		fetchErr  error
		judgeErr  error
		wantErr   error
	}{
		{name: "source_rejected", ref: Ref{Provider: "tmdb", Kind: Show}, judgeErr: ErrNotFound, wantErr: ErrNotFound},
		{name: "source_uncertain", ref: Ref{Provider: "tmdb", Kind: Show}, judgeErr: ErrAmbiguous, wantErr: ErrAmbiguous},
		{name: "source_missing", ref: Ref{Provider: "tmdb", Kind: Show}, searchErr: ErrNotFound, wantErr: ErrConstraintMismatch},
		{name: "fixed_work_missing", ref: Ref{Provider: "tmdb", Kind: Show, ID: "11"}, fetchErr: ErrNotFound, wantErr: ErrConstraintMismatch},
		{name: "fixed_work_constraint_mismatch", ref: Ref{Provider: "tmdb", Kind: Show, ID: "11"}, fetchErr: ErrConstraintMismatch, wantErr: ErrConstraintMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &testProvider{name: "tmdb", err: tc.searchErr, fetchError: tc.fetchErr}
			b, judge := &testProvider{name: "thetvdb"}, &testJudge{err: tc.judgeErr}
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), Request{Kind: Show, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}, Ref: tc.ref, Query: Query{Title: "作品"}}, t.TempDir())
			if got != nil || !errors.Is(err, tc.wantErr) || b.searchCalls+b.fetchCalls != 0 || tc.ref.ID != "" && a.searchCalls != 0 {
				t.Fatalf("人工约束失效：%+v %v a=%+v b=%+v", got, err, a, b)
			}
		})
	}
}

func TestResolveManualGroupsReachAllBatches(t *testing.T) {
	p := &seriesResolveProvider{testProvider: testProvider{name: "tmdb"}}
	req := Request{Kind: Show, Ref: Ref{Provider: "tmdb", Kind: Show, ID: "11"}, Episodes: []EpisodeKey{{Season: 2, Episode: 3, Group: "group-a"}, {Season: 3, Episode: 1, Group: "group-b"}}}
	got, err := NewManager([]Provider{p}, 0, new(testJudge)).Resolve(context.Background(), req, t.TempDir())
	if err != nil || len(got.Episodes) != 2 || len(p.batches) != 1 {
		t.Fatalf("分组批量请求失败: %+v %v", got, err)
	}
	for _, batch := range p.batches {
		if batch.Ref.ID != "11" || len(batch.Episodes) != 2 || batch.Episodes[0].Group != "group-a" || batch.Episodes[1].Group != "group-b" {
			t.Fatalf("各季分组被覆盖: %+v", batch)
		}
	}
}

func TestResolveCancellationCannotBecomeUnconfirmed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cancelStage string
		resultErr   error
		lastSource  bool
	}{
		{name: "search_then_next", cancelStage: "search", resultErr: ErrNotFound},
		{name: "search_then_exhausted", cancelStage: "search", resultErr: ErrNotFound, lastSource: true},
		{name: "judge_rejected", cancelStage: "judge", resultErr: ErrNotFound},
		{name: "judge_uncertain", cancelStage: "judge", resultErr: ErrAmbiguous},
		{name: "judge_confirmed", cancelStage: "judge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &controlledProvider{testProvider: &testProvider{name: "tmdb"}}
			if tc.cancelStage == "search" {
				a.search = func(context.Context, Query) ([]Candidate, error) {
					cancel()
					return nil, tc.resultErr
				}
			}
			calls := 0
			judge := judgeFunc(func(context.Context, Request, []Candidate) (int, error) {
				calls++
				cancel()
				return 0, tc.resultErr
			})
			b := &testProvider{name: "thetvdb"}
			providers := []Provider{a, b}
			if tc.lastSource {
				providers = providers[:1]
			}
			got, err := NewManager(providers, 0, judge).Resolve(ctx, Request{Kind: Movie, Query: Query{Title: "作品"}}, t.TempDir())
			if got != nil || !errors.Is(err, context.Canceled) || b.searchCalls+b.fetchCalls != 0 || tc.cancelStage == "search" && calls != 0 {
				t.Fatalf("取消被吞掉或取消后仍请求后续来源：%+v %v %+v calls=%d", got, err, b, calls)
			}
		})
	}
}

func TestResolveCandidateLimitAppliesPerSource(t *testing.T) {
	for _, count := range []int{254, 255} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			makeProvider := func(name string) *controlledProvider {
				return &controlledProvider{testProvider: &testProvider{name: name}, search: func(_ context.Context, query Query) ([]Candidate, error) {
					candidates := make([]Candidate, count)
					for i := range candidates {
						candidates[i] = Candidate{Ref: Ref{Provider: name, Kind: query.Kind, ID: fmt.Sprint(i + 1)}, Title: "作品"}
					}
					return candidates, nil
				}}
			}
			a, b := makeProvider("tmdb"), makeProvider("thetvdb")
			calls := 0
			judge := judgeFunc(func(_ context.Context, _ Request, options []Candidate) (int, error) {
				calls++
				if len(options) != 254 {
					t.Fatalf("候选集合被截断或跨来源累加：%d", len(options))
				}
				return 253, nil
			})
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), Request{Kind: Movie, Query: Query{Title: "作品"}}, t.TempDir())
			if count == 255 {
				if got != nil || err == nil || a.fetchCalls+b.fetchCalls+calls != 0 || b.searchCalls != 1 || !errors.Is(err, ErrSourcesExhausted) {
					t.Fatalf("超限候选仍被处理：%+v %v a=%+v b=%+v calls=%d", got, err, a, b, calls)
				}
				return
			}
			if err != nil || got.Work.Ref.Provider != "tmdb" || got.Work.Ref.ID != "254" || calls != 1 || b.searchCalls != 0 || a.fetchCalls != 1 {
				t.Fatalf("候选上限被跨来源累计：%+v %v calls=%d", got, err, calls)
			}
		})
	}
}
