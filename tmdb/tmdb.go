package tmdb

import "fmt"

const (
	ApiSearchTv       = "/3/search/tv"
	ApiSearchMovie    = "/3/search/movie"
	ApiTvDetail       = "/3/tv/%d"
	ApiTvEpisodeGroup = "/3/tv/episode_group/%s"
	ApiMovieDetail    = "/3/movie/%d"
)

// statusError 由 Provider 请求层返回，保留请求失败的状态码。
type statusError struct {
	code int
}

func (e *statusError) Error() string { return fmt.Sprintf("request tmdb status code: %d", e.code) }
