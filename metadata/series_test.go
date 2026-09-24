package metadata

import "testing"

func validSeriesFixture() (SeriesRequest, *SeriesResult) {
	req := SeriesRequest{Ref: Ref{Provider: "tmdb", Kind: Show, ID: "10"}, Episodes: []EpisodeKey{{Season: 0, Episode: 1}, {Season: 2, Episode: 4, Group: "group-a"}}}
	result := &SeriesResult{Work: &Record{Ref: req.Ref, Title: "节目"}, Episodes: []SeriesEpisode{
		{Key: req.Episodes[0], Record: &Record{Ref: Ref{Provider: "tmdb", Kind: Episode, ID: "101"}, Title: "特别篇", SeasonNumber: 0, EpisodeNumber: 1}},
		{Key: req.Episodes[1], Record: &Record{Ref: Ref{Provider: "tmdb", Kind: Episode, ID: "204"}, Title: "第四集", SeasonNumber: 2, EpisodeNumber: 4}},
	}}
	return req, result
}

func TestSeriesValidationAcceptsCompleteSpecialsAndGroupedCoordinates(t *testing.T) {
	req, result := validSeriesFixture()
	if err := ValidateSeriesRequest(req); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSeriesResult(req, result); err != nil {
		t.Fatal(err)
	}
	req.Episodes = append(req.Episodes, req.Episodes[0])
	if err := ValidateSeriesResult(req, result); err != nil {
		t.Fatalf("重复请求应共用事实: %v", err)
	}
}

func TestSeriesValidationRejectsIncompleteOrMisboundFacts(t *testing.T) {
	cases := []struct {
		name   string
		change func(*SeriesResult)
	}{
		{"missing", func(r *SeriesResult) { r.Episodes = r.Episodes[:1] }},
		{"duplicate", func(r *SeriesResult) { r.Episodes[1] = r.Episodes[0] }},
		{"wrong source", func(r *SeriesResult) { r.Episodes[0].Record.Ref.Provider = "thetvdb" }},
		{"wrong object", func(r *SeriesResult) { r.Episodes[0].Record.Ref.Kind = Show }},
		{"wrong coordinate", func(r *SeriesResult) { r.Episodes[0].Record.SeasonNumber = 1 }},
		{"wrong key", func(r *SeriesResult) { r.Episodes[1].Key.Episode = 5 }},
		{"wrong group", func(r *SeriesResult) { r.Episodes[1].Key.Group = "other" }},
		{"nil record", func(r *SeriesResult) { r.Episodes[0].Record = nil }},
		{"missing record identity", func(r *SeriesResult) { r.Episodes[0].Record.Ref.ID = "" }},
		{"blank title", func(r *SeriesResult) { r.Episodes[0].Record.Title = " " }},
		{"wrong show", func(r *SeriesResult) { r.Work.Ref.ID = "99" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			req, result := validSeriesFixture()
			test.change(result)
			if err := ValidateSeriesResult(req, result); err == nil {
				t.Fatal("错误批量事实被接受")
			}
		})
	}
}

func TestSeriesValidationRejectsInvalidRequests(t *testing.T) {
	cases := []SeriesRequest{
		{},
		{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "10"}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}},
		{Ref: Ref{Provider: "tmdb", Kind: Show}, Episodes: []EpisodeKey{{Season: 1, Episode: 1}}},
		{Ref: Ref{Provider: "tmdb", Kind: Show, ID: "10"}, Episodes: []EpisodeKey{{Season: -1, Episode: 1}}},
		{Ref: Ref{Provider: "tmdb", Kind: Show, ID: "10"}, Episodes: []EpisodeKey{{Season: 1, Episode: 0}}},
	}
	for _, req := range cases {
		if err := ValidateSeriesRequest(req); err == nil {
			t.Errorf("无效请求被接受: %+v", req)
		}
	}
}
