package metadata

import "context"

// EpisodeKey 是本地编排中的季集坐标；Group 是可选的 TMDb 分组记录编号。
type EpisodeKey struct {
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Group   string `json:"group,omitempty"`
}

// SeriesRequest 在同一来源和节目内请求多个单集，Details 表示补齐输出所需扩展事实。
type SeriesRequest struct {
	Ref      Ref          `json:"ref"`
	Episodes []EpisodeKey `json:"episodes"`
	Details  bool         `json:"details"`
}

// SeriesEpisode 显式绑定请求坐标与来源事实，避免按响应顺序错误合并。
type SeriesEpisode struct {
	Key    EpisodeKey `json:"key"`
	Record *Record    `json:"record"`
}

type SeriesResult struct {
	Work     *Record         `json:"work"`
	Episodes []SeriesEpisode `json:"episodes"`
}

// SeriesProvider 由剧集消费端定义；批量事实仍属于一个来源的一部节目。
type SeriesProvider interface {
	FetchSeries(context.Context, SeriesRequest) (*SeriesResult, error)
}
