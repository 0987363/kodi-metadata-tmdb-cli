package tmdb

import (
	"fmt"
	"time"
)

const (
	ApiSearchTv       = "/3/search/tv"
	ApiSearchMovie    = "/3/search/movie"
	ApiTvDetail       = "/3/tv/%d"
	ApiTvEpisodeGroup = "/3/tv/episode_group/%s"
	ApiMovieDetail    = "/3/movie/%d"
)

// statusError 由 Provider 请求层返回，保留状态码与限流等待时间。
type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e *statusError) Error() string { return fmt.Sprintf("request tmdb status code: %d", e.code) }
