package metadata

// SourceStats 记录单个来源实例已发起的 HTTP 请求及判断次数。
type SourceStats struct {
	HTTPAttempts   int64 `json:"http_attempts"`
	SearchRequests int64 `json:"search_requests"`
	BatchRequests  int64 `json:"batch_requests"`
	DetailRequests int64 `json:"detail_requests"`
	Decisions      int64 `json:"decisions"`
}

// StatisticsProvider 提供单个来源实例的请求计数快照。
type StatisticsProvider interface {
	Statistics() SourceStats
}
