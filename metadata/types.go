package metadata

import (
	"context"
	"errors"
)

type Kind string

const (
	Movie   Kind = "movie"
	Show    Kind = "show"
	Episode Kind = "episode"
)

var (
	ErrNoMatch            = errors.New("所有适用来源正常完成但未确认匹配")
	ErrConstraintMismatch = errors.New("候选不满足人工约束")
	ErrNotFound           = errors.New("未找到元数据")
	ErrAmbiguous          = errors.New("元数据匹配存在歧义")
	ErrUnsupported        = errors.New("数据源不支持此操作")
)

// Ref 的编号只在所属数据源和对象类型内有效；Slug 仅定位 TheTVDB 节目网页。
type Ref struct {
	Provider string `json:"provider"`
	Kind     Kind   `json:"kind"`
	ID       string `json:"id,omitempty"`
	Slug     string `json:"slug,omitempty"`
}
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type Query struct {
	Kind          Kind   `json:"kind"`
	Title         string `json:"title"`
	ChineseTitle  string `json:"chinese_title,omitempty"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
}
type Candidate struct {
	Ref           Ref    `json:"ref"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
}
type Request struct {
	Episodes     []EpisodeKey `json:"episodes,omitempty"`
	Files        []string     `json:"files,omitempty"`
	Path         string       `json:"path,omitempty"`
	Filename     string       `json:"filename,omitempty"`
	Hints        []Ref        `json:"hints,omitempty"` // 通用 LLM 提供的各来源作品定位提示
	EpisodeTitle string       `json:"episode_title,omitempty"`
	Kind         Kind         `json:"kind"`
	Ref          Ref          `json:"ref"`
	Query        Query        `json:"query"`
	Season       int          `json:"season,omitempty"`
	Episode      int          `json:"episode,omitempty"`
	Group        string       `json:"group,omitempty"`
}

// Record 保存单一对象的事实；单集的 ExternalIDs 不包含父节目的编号。
type Record struct {
	Ref            Ref          `json:"ref"`
	ExternalIDs    []Identifier `json:"external_ids,omitempty"`
	SourceURL      string       `json:"source_url"`
	Title          string       `json:"title"`
	OriginalTitle  string       `json:"original_title,omitempty"`
	Plot           string       `json:"plot,omitempty"`
	Premiered      string       `json:"premiered,omitempty"`
	LastAired      string       `json:"last_aired,omitempty"`
	Status         string       `json:"status,omitempty"`
	Certification  string       `json:"certification,omitempty"`
	Genres         []string     `json:"genres,omitempty"`
	Studios        []string     `json:"studios,omitempty"`
	Countries      []string     `json:"countries,omitempty"`
	Languages      []string     `json:"languages,omitempty"`
	Actors         []Actor      `json:"actors,omitempty"`
	Directors      []string     `json:"directors,omitempty"`
	Credits        []string     `json:"credits,omitempty"`
	Ratings        []Rating     `json:"ratings,omitempty"`
	Artwork        []Artwork    `json:"artwork,omitempty"`
	Seasons        []Season     `json:"seasons,omitempty"`
	SeasonCount    int          `json:"season_count,omitempty"`
	EpisodeCount   int          `json:"episode_count,omitempty"`
	SeasonNumber   int          `json:"season_number,omitempty"`
	EpisodeNumber  int          `json:"episode_number,omitempty"`
	RuntimeMinutes int          `json:"runtime_minutes,omitempty"`
}
type Actor struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Order int    `json:"order"`
	Thumb string `json:"thumb,omitempty"`
}
type Rating struct {
	Source string  `json:"source"`
	Value  float32 `json:"value"`
	Votes  int     `json:"votes"`
	Max    int     `json:"max"`
}
type Artwork struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Season int    `json:"season,omitempty"`
}
type Season struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type Provider interface {
	Name() string
	// Scope 包含影响响应的站点和语言配置，用于隔离缓存，不得包含密钥。
	Scope() string
	Supports(Kind) bool
	Search(context.Context, Query) ([]Candidate, error)
	Fetch(context.Context, Request) (*Record, error)
}

// Option 将节目与目标单集绑定为一个来源候选，电影只包含 Work。
type Option struct {
	Episodes []SeriesEpisode `json:"episodes,omitempty"`
	Work     *Record         `json:"work"`
}

// Judge 只能返回本次候选集合中的序位；不生成或替换元数据。
type Judge interface {
	Select(context.Context, Request, []Option) (int, error)
}
