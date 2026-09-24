package ai

type BatchFile struct {
	RelativePath string `json:"relative_path"`
}
type BatchInput struct {
	Root  string      `json:"root"`
	Files []BatchFile `json:"files"`
}
type Identity struct {
	ParseResult
	RelativePath string  `json:"relative_path"`
	MediaType    string  `json:"media_type"`
	SeriesRoot   string  `json:"series_root"`
	ContentRole  *string `json:"content_role"`
}
type LocalDescription struct {
	RelativePath string   `json:"relative_path"`
	Title        string   `json:"title"`
	Plot         string   `json:"plot"`
	Genres       []string `json:"genres"`
}
type ClientStats struct {
	AnalyzeRequests  int64
	LocalRequests    int64
	DecisionRequests int64
}

// ParseResult 只保存识别线索，nil 季集号表示未知，季号零仍是特别篇。
type ParseResult struct {
	TMDBID       string `json:"tmdb_id,omitempty"`
	TheTVDBID    string `json:"thetvdb_id,omitempty"`
	Title        string `json:"title"`
	AliasTitle   string `json:"alias_title"`
	ChsTitle     string `json:"chs_title"`
	EngTitle     string `json:"eng_title"`
	Year         int    `json:"year"`
	Season       *int   `json:"season"`
	Episode      *int   `json:"episode"`
	EpisodeTitle string `json:"episode_title,omitempty"`
}
