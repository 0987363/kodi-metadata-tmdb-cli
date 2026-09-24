package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestResolveSourceAttemptFailuresAdvanceToCompleteOption(t *testing.T) {
	for _, stage := range []string{"search", "judge_rejected", "judge_low_score", "judge_protocol", "choice", "movie_detail", "series_incomplete"} {
		t.Run(stage, func(t *testing.T) {
			a := &seriesResolveProvider{testProvider: testProvider{name: "thetvdb"}}
			b := &seriesResolveProvider{testProvider: testProvider{name: "tmdb"}}
			req := Request{Kind: Movie, Query: Query{Title: "作品"}}
			switch stage {
			case "search":
				a.err = errors.New("HTTP 503")
			case "movie_detail":
				a.fetchError = errors.New("电影详情缺失")
			case "series_incomplete":
				req.Kind = Show
				req.Episodes = []EpisodeKey{{Season: 1, Episode: 1}, {Season: 2, Episode: 1}}
				a.incomplete = true
			}
			judge := judgeFunc(func(_ context.Context, _ Request, options []Candidate) (int, error) {
				if options[0].Ref.Provider == "thetvdb" {
					switch stage {
					case "judge_rejected":
						return -1, ErrNotFound
					case "judge_low_score":
						return -1, ErrAmbiguous
					case "judge_protocol":
						return -1, errors.New("invalid JSON")
					case "choice":
						return 99, nil
					}
				}
				return 0, nil
			})
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), req, t.TempDir())
			if err != nil || got == nil || got.Work.Ref.Provider != "tmdb" || b.searchCalls != 1 {
				t.Fatalf("来源未成功后没有接续完整结果: option=%+v err=%v a=%+v b=%+v", got, err, a, b)
			}
			if req.Kind == Show && (len(got.Episodes) != 2 || got.Episodes[0].Record.Ref.Provider != "tmdb" || len(a.batches) != 1 || len(b.batches) != 1) {
				t.Fatalf("整剧事实未完整切换: %+v a=%+v b=%+v", got, a.batches, b.batches)
			}
		})
	}
}

func TestResolveValidatesAllInputBeforeSourceRequests(t *testing.T) {
	for _, req := range []Request{
		{Kind: Movie},
		{Kind: Movie, Query: Query{Title: "作品"}, Hints: []Ref{{Provider: "tmdb", Kind: Show, ID: "42"}}},
		{Kind: Movie, Query: Query{Title: "作品"}, Hints: []Ref{{Provider: "tmdb", Kind: Movie, ID: "42"}, {Provider: "tmdb", Kind: Movie, ID: "43"}}},
	} {
		a, b := &testProvider{name: "thetvdb"}, &testProvider{name: "tmdb"}
		got, err := NewManager([]Provider{a, b}, 0, &testJudge{}).Resolve(context.Background(), req, t.TempDir())
		if err == nil || got != nil || a.searchCalls+a.fetchCalls+b.searchCalls+b.fetchCalls != 0 {
			t.Fatalf("输入错误未先行终止: req=%+v got=%+v err=%v a=%+v b=%+v", req, got, err, a, b)
		}
	}
}

func TestResolveManualSourceMissCannotPermitLocal(t *testing.T) {
	a, b := &testProvider{name: "thetvdb", err: ErrNotFound}, &testProvider{name: "tmdb"}
	_, err := NewManager([]Provider{a, b}, 0, &testJudge{}).Resolve(context.Background(), Request{Kind: Movie, Ref: Ref{Provider: "thetvdb", Kind: Movie}, Query: Query{Title: "作品"}}, t.TempDir())
	if !errors.Is(err, ErrConstraintMismatch) || b.searchCalls+b.fetchCalls != 0 {
		t.Fatalf("人工指定来源被静默替换: %v b=%+v", err, b)
	}
}

type unsupportedMovieProvider struct{ testProvider }

func (p *unsupportedMovieProvider) Supports(kind Kind) bool { return kind == Show }

func TestResolveNoApplicableWebsitePermitsLocalUnlessConstrained(t *testing.T) {
	for _, manual := range []bool{false, true} {
		p := &unsupportedMovieProvider{testProvider{name: "thetvdb"}}
		req := Request{Kind: Movie, Query: Query{Title: "电影"}}
		if manual {
			req.Ref = Ref{Provider: "thetvdb", Kind: Movie}
		}
		_, err := NewManager([]Provider{p}, 0, &testJudge{}).Resolve(context.Background(), req, t.TempDir())
		if errors.Is(err, ErrSourcesExhausted) == manual || p.searchCalls+p.fetchCalls != 0 {
			t.Fatalf("不适用来源与人工约束边界错误: manual=%t err=%v source=%+v", manual, err, p)
		}
	}
	_, err := NewManager(nil, 0, &testJudge{}).Resolve(context.Background(), Request{Kind: Movie, Query: Query{Title: "电影"}}, t.TempDir())
	if err == nil || errors.Is(err, ErrSourcesExhausted) {
		t.Fatalf("没有配置来源仍进入本地整理: %v", err)
	}
}

func TestResolveParentCancellationDuringDetailStops(t *testing.T) {
	for _, kind := range []Kind{Movie, Show} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &controlledProvider{testProvider: &testProvider{name: "thetvdb"}, fetch: func(context.Context, Request) (*Record, error) {
				cancel()
				return nil, ErrNotFound
			}}
			b := &testProvider{name: "tmdb"}
			req := Request{Kind: kind, Query: Query{Title: "作品"}}
			if kind == Show {
				req.Episodes = []EpisodeKey{{Season: 1, Episode: 1}}
			}
			_, err := NewManager([]Provider{a, b}, 0, &testJudge{}).Resolve(ctx, req, t.TempDir())
			if !errors.Is(err, context.Canceled) || errors.Is(err, ErrSourcesExhausted) || b.searchCalls+b.fetchCalls != 0 {
				t.Fatalf("详情期间取消未终止: %v b=%+v", err, b)
			}
		})
	}
}

func TestResolveParentDeadlineStopsBeforeLaterSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	a := &controlledProvider{testProvider: &testProvider{name: "thetvdb"}, search: func(ctx context.Context, _ Query) ([]Candidate, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	b := &testProvider{name: "tmdb"}
	_, err := NewManager([]Provider{a, b}, 0, &testJudge{}).Resolve(ctx, Request{Kind: Movie, Query: Query{Title: "作品"}}, t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrSourcesExhausted) || b.searchCalls+b.fetchCalls != 0 {
		t.Fatalf("任务超时被当作单源失败: %v b=%+v", err, b)
	}
}
