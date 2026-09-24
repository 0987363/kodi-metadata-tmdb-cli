package ai

type ParseInput struct {
	MediaType string `json:"media_type"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
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
