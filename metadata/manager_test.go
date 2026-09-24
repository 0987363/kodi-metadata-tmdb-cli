package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testProvider struct {
	name, scope             string
	searchCalls, fetchCalls int
	err                     error
	fetchError              error
}

func (p *testProvider) Name() string         { return p.name }
func (p *testProvider) Scope() string        { return p.scope }
func (p *testProvider) Supports(k Kind) bool { return k == Movie || k == Show || k == Episode }
func (p *testProvider) Search(ctx context.Context, q Query) ([]Candidate, error) {
	p.searchCalls++
	if p.err != nil {
		return nil, p.err
	}
	return []Candidate{{Ref: Ref{Provider: p.name, Kind: q.Kind, ID: "42"}, Title: q.Title}}, nil
}
func (p *testProvider) Fetch(ctx context.Context, r Request) (*Record, error) {
	p.fetchCalls++
	if p.fetchError != nil {
		return nil, p.fetchError
	}
	ref := r.Ref
	if r.Kind == Episode {
		ref.Kind = Episode
		ref.ID += "-episode"
	}
	return &Record{Ref: ref, Title: p.name, SeasonNumber: r.Season, EpisodeNumber: r.Episode}, nil
}

func (p *testProvider) FetchSeries(ctx context.Context, req SeriesRequest) (*SeriesResult, error) {
	p.fetchCalls++
	if p.fetchError != nil {
		return nil, p.fetchError
	}
	result := &SeriesResult{Work: &Record{Ref: req.Ref, Title: p.name}}
	for _, key := range UniqueEpisodeKeys(req.Episodes) {
		result.Episodes = append(result.Episodes, SeriesEpisode{Key: key, Record: &Record{Ref: Ref{Provider: p.name, Kind: Episode, ID: req.Ref.ID + "-episode"}, Title: p.name, SeasonNumber: key.Season, EpisodeNumber: key.Episode}})
	}
	return result, nil
}

type testJudge struct {
	calls, index int
	seen         []Candidate
	err          error
}

func (j *testJudge) Select(ctx context.Context, r Request, options []Candidate) (int, error) {
	j.calls++
	j.seen = options
	return j.index, j.err
}
func TestResolveStopsAfterFirstSourceConfirmation(t *testing.T) {
	a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
	judge := &testJudge{index: 0}
	m := NewManager([]Provider{a, b}, time.Hour, judge)
	got, err := m.Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 0, Episode: 2}}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if judge.calls != 1 || len(judge.seen) != 1 || got.Work.Ref.Provider != "tmdb" || got.Episodes[0].Record.Ref.Provider != "tmdb" || got.Episodes[0].Record.Ref.Kind != Episode || got.Episodes[0].Record.SeasonNumber != 0 {
		t.Fatalf("候选与单集未整体选择：%+v %+v", got, judge)
	}
	if a.searchCalls != 1 || b.searchCalls != 0 || a.fetchCalls != 1 || b.fetchCalls != 0 {
		t.Fatalf("来源确认后仍访问后续来源：%+v %+v", a, b)
	}
}
func TestResolveKnownIDsSearchAndSkipJudgeOnlyOnExactMatch(t *testing.T) {
	a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
	j := new(testJudge)
	req := Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}, Hints: []Ref{{Provider: "tmdb", Kind: Show, ID: "42"}, {Provider: "thetvdb", Kind: Show, ID: "22"}}}
	got, err := NewManager([]Provider{a, b}, time.Hour, j).Resolve(context.Background(), req, t.TempDir())
	if err != nil || got.Work.Ref.ID != "42" || a.searchCalls != 1 || b.searchCalls != 0 || a.fetchCalls != 1 || b.fetchCalls != 0 || j.calls != 0 {
		t.Fatalf("编号未通过真实搜索核验或未跳过判断：%+v %v a=%+v b=%+v judge=%+v", got, err, a, b, j)
	}
}
func TestResolveManualSourceConstrainsHintsAndJudgesSingleCandidate(t *testing.T) {
	a, b := &testProvider{name: "tmdb"}, &testProvider{name: "thetvdb"}
	j := new(testJudge)
	m := NewManager([]Provider{a, b}, time.Hour, j)
	req := Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}, Ref: Ref{Provider: "thetvdb", Kind: Show}, Hints: []Ref{{Provider: "tmdb", Kind: Show, ID: "11"}, {Provider: "thetvdb", Kind: Show, ID: "22"}}}
	got, err := m.Resolve(context.Background(), req, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.searchCalls+a.fetchCalls != 0 || b.searchCalls != 1 || j.calls != 1 || got.Work.Ref.ID != "42" {
		t.Fatalf("人工来源未约束候选或绕过Jev：%+v %+v", got, j)
	}
}
func TestResolveCachesSourceDataButAlwaysJudges(t *testing.T) {
	p := &testProvider{name: "tmdb", scope: "zh"}
	j := new(testJudge)
	m := NewManager([]Provider{p}, time.Hour, j)
	root := t.TempDir()
	req := Request{Kind: Show, Query: Query{Title: "作品"}, Episodes: []EpisodeKey{{Season: 1, Episode: 2}}}
	for range 2 {
		if _, err := m.Resolve(context.Background(), req, root); err != nil {
			t.Fatal(err)
		}
	}
	if p.searchCalls != 1 || p.fetchCalls != 1 || j.calls != 2 {
		t.Fatalf("数据缓存或Jev阶段错误：%+v %+v", p, j)
	}
	req.Episodes[0].Group = "new-group"
	if _, err := m.Resolve(context.Background(), req, root); err != nil {
		t.Fatal(err)
	}
	if p.fetchCalls != 2 {
		t.Fatal("跨分组复用了详情")
	}
	p.scope = "en"
	if _, err := m.Resolve(context.Background(), req, root); err != nil {
		t.Fatal(err)
	}
	if p.searchCalls != 2 || p.fetchCalls != 3 {
		t.Fatal("跨语言复用了缓存")
	}
}
func TestResolveErrorsNeverBecomeFirstCandidate(t *testing.T) {
	for _, tc := range []struct {
		name                string
		sourceErr, judgeErr error
		index               int
	}{{"source", errors.New("HTTP 403"), nil, 0}, {"judge", nil, errors.New("Jev rejects"), 0}, {"invalid choice", nil, nil, 99}} {
		t.Run(tc.name, func(t *testing.T) {
			p := &testProvider{name: "tmdb", err: tc.sourceErr}
			j := &testJudge{err: tc.judgeErr, index: tc.index}
			m := NewManager([]Provider{p}, 0, j)
			got, err := m.Resolve(context.Background(), Request{Kind: Movie, Query: Query{Title: "作品"}}, t.TempDir())
			if err == nil || got != nil {
				t.Fatalf("错误被吞掉：%+v %v", got, err)
			}
			if tc.sourceErr != nil && j.calls != 0 {
				t.Fatal("未获取事实就调用判断")
			}
		})
	}
}
func TestResolveCancellationAndNoResults(t *testing.T) {
	p := &testProvider{name: "tmdb", err: ErrNotFound}
	j := new(testJudge)
	m := NewManager([]Provider{p}, 0, j)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Resolve(ctx, Request{Kind: Show}, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.Resolve(context.Background(), Request{Kind: Show, Query: Query{Title: "missing"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}}, t.TempDir()); !errors.Is(err, ErrNoMatch) || j.calls != 0 {
		t.Fatalf("空候选处理错误：%v", err)
	}
}
