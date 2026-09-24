package metadata

// JudgmentInput 保留作品上下文与整组单集约束；候选序位不是网站编号。
type JudgmentInput struct {
	Path     string       `json:"path,omitempty"`
	Filename string       `json:"filename,omitempty"`
	Files    []string     `json:"files,omitempty"`
	Hints    []Ref        `json:"hints,omitempty"`
	Kind     Kind         `json:"kind"`
	Ref      Ref          `json:"ref"`
	Query    Query        `json:"query"`
	Episodes []EpisodeKey `json:"episodes,omitempty"`
}

func JudgmentInputFor(request Request) JudgmentInput {
	return JudgmentInput{Path: request.Path, Filename: request.Filename, Files: request.Files, Hints: request.Hints, Kind: request.Kind, Ref: request.Ref, Query: request.Query, Episodes: UniqueEpisodeKeys(request.Episodes)}
}

type EpisodeEvidence struct {
	Key    EpisodeKey      `json:"requested"`
	Record *RecordEvidence `json:"record"`
}

type OptionEvidence struct {
	Work     *RecordEvidence   `json:"work"`
	Episodes []EpisodeEvidence `json:"episodes,omitempty"`
}

// RecordEvidence 仅保留判断所需事实，不发送完整演员和图片集合。
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
	out := OptionEvidence{Work: recordEvidence(option.Work)}
	for _, episode := range option.Episodes {
		out.Episodes = append(out.Episodes, EpisodeEvidence{Key: episode.Key, Record: recordEvidence(episode.Record)})
	}
	return out
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
