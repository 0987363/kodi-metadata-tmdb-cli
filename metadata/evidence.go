package metadata

// JudgmentInput 保留原始上下文及人工约束，不遗漏特别篇的零季号。
type JudgmentInput struct {
	Path         string `json:"path,omitempty"`
	Filename     string `json:"filename,omitempty"`
	Hints        []Ref  `json:"hints,omitempty"`
	EpisodeTitle string `json:"episode_title,omitempty"`
	Kind         Kind   `json:"kind"`
	Ref          Ref    `json:"ref"`
	Query        Query  `json:"query"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	Group        string `json:"group,omitempty"`
}

func JudgmentInputFor(request Request) JudgmentInput {
	return JudgmentInput{Path: request.Path, Filename: request.Filename, Hints: request.Hints, EpisodeTitle: request.EpisodeTitle, Kind: request.Kind, Ref: request.Ref, Query: request.Query, Season: request.Season, Episode: request.Episode, Group: request.Group}
}

type OptionEvidence struct {
	Work    *RecordEvidence `json:"work"`
	Episode *RecordEvidence `json:"episode,omitempty"`
}

// RecordEvidence 仅包含身份核验所需事实，排除演员和图库等无关大字段。
type RecordEvidence struct {
	ExternalIDs   []Identifier `json:"external_ids,omitempty"`
	Source        string       `json:"source"`
	Kind          Kind         `json:"kind"`
	ID            string       `json:"id,omitempty"`
	Slug          string       `json:"slug,omitempty"`
	SourceURL     string       `json:"source_url,omitempty"`
	Title         string       `json:"title"`
	OriginalTitle string       `json:"original_title,omitempty"`
	Plot          string       `json:"plot,omitempty"`
	Premiered     string       `json:"premiered,omitempty"`
	SeasonNumber  *int         `json:"season_number,omitempty"`
	EpisodeNumber *int         `json:"episode_number,omitempty"`
}

func EvidenceForOption(option Option) OptionEvidence {
	return OptionEvidence{Work: recordEvidence(option.Work), Episode: recordEvidence(option.Episode)}
}
func recordEvidence(record *Record) *RecordEvidence {
	if record == nil {
		return nil
	}
	out := &RecordEvidence{ExternalIDs: record.ExternalIDs, Source: record.Ref.Provider, Kind: record.Ref.Kind, ID: record.Ref.ID, Slug: record.Ref.Slug, SourceURL: record.SourceURL, Title: record.Title, OriginalTitle: record.OriginalTitle, Plot: record.Plot, Premiered: record.Premiered}
	if record.Ref.Kind == Episode {
		out.SeasonNumber = &record.SeasonNumber
		out.EpisodeNumber = &record.EpisodeNumber
	}
	return out
}
