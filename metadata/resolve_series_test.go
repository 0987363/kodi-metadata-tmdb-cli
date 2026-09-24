package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type seriesResolveProvider struct {
	testProvider
	batches    []SeriesRequest
	batchError error
	incomplete bool
}

func (p *seriesResolveProvider) FetchSeries(_ context.Context, req SeriesRequest) (*SeriesResult, error) {
	p.batches = append(p.batches, req)
	if p.batchError != nil {
		return nil, p.batchError
	}
	result := &SeriesResult{Work: &Record{Ref: req.Ref, Title: "节目"}}
	for _, key := range UniqueEpisodeKeys(req.Episodes) {
		result.Episodes = append(result.Episodes, SeriesEpisode{Key: key, Record: &Record{Ref: Ref{Provider: p.name, Kind: Episode, ID: fmt.Sprintf("%d%02d", key.Season, key.Episode)}, Title: "单集", SeasonNumber: key.Season, EpisodeNumber: key.Episode}})
	}
	if p.incomplete {
		result.Episodes = nil
	}
	return result, nil
}

func TestResolveSeriesJudgesOnceAndFetchesFullSelectedSource(t *testing.T) {
	a := &seriesResolveProvider{testProvider: testProvider{name: "tmdb"}}
	b := &seriesResolveProvider{testProvider: testProvider{name: "thetvdb"}}
	judge := &testJudge{}
	manager := NewManager([]Provider{a, b}, time.Hour, judge)
	req := Request{Kind: Show, Query: Query{Title: "节目"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}, {Season: 2, Episode: 1}}, Files: []string{"Season 1/S01E01.mkv", "Season 2/S02E01.mkv"}}
	root := t.TempDir()
	for range 2 {
		option, err := manager.Resolve(context.Background(), req, root)
		if err != nil {
			t.Fatal(err)
		}
		if len(option.Episodes) != 2 {
			t.Fatalf("未返回整剧批量单集: %+v", option)
		}
	}
	if len(a.batches) != 1 || !a.batches[0].Details || judge.calls != 2 || a.fetchCalls != 0 || b.searchCalls != 0 || len(b.batches) != 0 {
		t.Fatalf("批量获取/判断次数错误: a=%+v b=%+v judge=%+v", a, b, judge)
	}
}

func TestResolveSeriesErrorsAdvanceUnlessManuallyConstrained(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		incomplete bool
		manual     bool
	}{
		{name: "protocol", err: errors.New("bad batch")},
		{name: "incomplete", incomplete: true},
		{name: "explicit missing", err: ErrNotFound, manual: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &seriesResolveProvider{testProvider: testProvider{name: "tmdb"}, batchError: test.err, incomplete: test.incomplete}
			b := &seriesResolveProvider{testProvider: testProvider{name: "thetvdb"}}
			judge := &testJudge{}
			req := Request{Kind: Show, Query: Query{Title: "节目"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}}
			if test.manual {
				req.Ref = Ref{Provider: "tmdb", Kind: Show, ID: "42"}
			}
			got, err := NewManager([]Provider{a, b}, 0, judge).Resolve(context.Background(), req, t.TempDir())
			if test.manual {
				if !errors.Is(err, ErrConstraintMismatch) || errors.Is(err, ErrSourcesExhausted) || judge.calls != 1 || b.searchCalls != 0 {
					t.Fatalf("人工约束失效: %v %+v", err, judge)
				}
				return
			}
			if err != nil || got == nil || got.Work.Ref.Provider != "thetvdb" || judge.calls != 2 || b.searchCalls != 1 {
				t.Fatalf("整剧事实错误未接续下一来源: %+v %v %+v", got, err, judge)
			}
		})
	}
}

func TestExplicitWorkRejectedByJudgeDoesNotPermitLocalMetadata(t *testing.T) {
	p := &testProvider{name: "tmdb"}
	_, err := NewManager([]Provider{p}, 0, &testJudge{err: ErrNotFound}).Resolve(context.Background(), Request{Kind: Movie, Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}}, t.TempDir())
	if !errors.Is(err, ErrConstraintMismatch) || errors.Is(err, ErrSourcesExhausted) {
		t.Fatalf("人工明确作品被绕过: %v", err)
	}
}
