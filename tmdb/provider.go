package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/common/httpx"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

type metadataProvider struct {
	config       config.TmdbConfig
	client       *http.Client
	seriesGate   chan struct{}
	seriesShows  map[int]*seriesFacts
	seriesGroups map[string]*TvEpisodeGroupDetail
	statsMu      sync.Mutex
	stats        metadata.SourceStats
}

var _ metadata.Provider = (*metadataProvider)(nil)

const maxResponseBytes = 8 << 20

var errResponseTooLarge = errors.New("TMDb 响应超过 8 MiB")

// NewMetadataProvider 为配置快照创建独立的请求客户端。
func NewMetadataProvider(c *config.TmdbConfig) metadata.Provider {
	settings := *c
	if settings.TimeoutSeconds <= 0 {
		settings.TimeoutSeconds = 30
	}
	settings.RetryCount = max(0, settings.RetryCount)
	client := httpx.NewClient(settings.Proxy, settings.TimeoutSeconds)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &metadataProvider{config: settings, client: client, seriesGate: make(chan struct{}, 1)}
}

func (p *metadataProvider) Name() string { return "tmdb" }

func (p *metadataProvider) Statistics() metadata.SourceStats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

func (p *metadataProvider) Scope() string {
	data, _ := json.Marshal(struct {
		API      string `json:"api"`
		Images   string `json:"images"`
		Language string `json:"language"`
		Rating   string `json:"rating"`
	}{p.config.ApiHost, p.config.ImageHost, p.config.Language, p.config.Rating})
	return string(data)
}

func (p *metadataProvider) Supports(kind metadata.Kind) bool {
	return kind == metadata.Movie || kind == metadata.Show
}

func (p *metadataProvider) requestJSON(ctx context.Context, path string, args url.Values, target any) error {
	endpoint, err := url.Parse(strings.TrimRight(p.config.ApiHost, "/") + path)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return errors.New("TMDb 请求地址无效")
	}
	if args == nil {
		args = make(url.Values)
	}
	args.Set("api_key", p.config.ApiKey)
	args.Set("language", p.config.Language)
	endpoint.RawQuery = args.Encode()
	for attempt := 0; ; attempt++ {
		body, requestErr := p.requestOnce(ctx, endpoint.String(), attempt > 0)
		if requestErr == nil {
			if err := json.Unmarshal(body, target); err != nil {
				return fmt.Errorf("解析 TMDb %s 响应：%w", path, err)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var status *statusError
		hasStatus := errors.As(requestErr, &status)
		if hasStatus && status.code == http.StatusNotFound {
			return fmt.Errorf("TMDb %s：%w", path, metadata.ErrNotFound)
		}
		retryable := !errors.Is(requestErr, errResponseTooLarge) && (!hasStatus || status.code == http.StatusTooManyRequests || status.code >= 500)
		if attempt >= p.config.RetryCount || !retryable {
			return fmt.Errorf("TMDb %s：%w", path, requestErr)
		}
		wait := time.Second << uint(min(attempt, 5))
		if status != nil && status.code == http.StatusTooManyRequests && status.retryAfter > 0 {
			wait = min(status.retryAfter, 10*time.Second)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *metadataProvider) requestOnce(ctx context.Context, endpoint string, retry bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("TMDb 请求地址无效")
	}
	p.statsMu.Lock()
	p.stats.HTTPAttempts++
	if retry {
		p.stats.Retries++
	}
	if strings.HasPrefix(req.URL.Path, "/3/search/") {
		p.stats.SearchRequests++
	} else if suffix, ok := strings.CutPrefix(req.URL.Path, "/3/tv/"); ok && suffix != "" && !strings.Contains(suffix, "/") && req.URL.Query().Get("append_to_response") != "" {
		p.stats.BatchRequests++
	}
	p.statsMu.Unlock()
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// url.Error 的 URL 含密钥，只保留底层传输错误。
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			return nil, requestErr.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		retryAfter := time.Duration(0)
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(min(seconds, 10)) * time.Second
		} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
			retryAfter = min(time.Until(date), 10*time.Second)
		}
		return nil, &statusError{code: resp.StatusCode, retryAfter: retryAfter}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errResponseTooLarge
	}
	return body, nil
}

func (p *metadataProvider) Fetch(ctx context.Context, req metadata.Request) (*metadata.Record, error) {
	if !p.Supports(req.Kind) {
		return nil, metadata.ErrUnsupported
	}
	if req.Ref.Provider != p.Name() || req.Ref.Kind != req.Kind {
		return nil, errors.New("TMDb 引用的数据源或对象类型不匹配")
	}
	id, err := strconv.Atoi(req.Ref.ID)
	if err != nil || id <= 0 || strconv.Itoa(id) != req.Ref.ID {
		return nil, errors.New("TMDb 对象编号必须为正整数")
	}
	if req.Kind == metadata.Movie {
		if req.Group != "" {
			return nil, errors.New("TMDb 电影不支持单集分组")
		}
		return p.fetchMovie(ctx, id)
	}
	var group *TvEpisodeGroupDetail
	if req.Group != "" {
		group, err = p.fetchGroup(ctx, req.Group, id)
		if err != nil {
			return nil, err
		}
	}
	return p.fetchShow(ctx, id, group)
}

func (p *metadataProvider) fetchMovie(ctx context.Context, id int) (*metadata.Record, error) {
	var movie MovieDetail
	args := url.Values{"append_to_response": {"credits,release_dates,images"}, "include_image_language": {"zh,en,null"}}
	if err := p.requestJSON(ctx, fmt.Sprintf(ApiMovieDetail, id), args, &movie); err != nil {
		return nil, err
	}
	if movie.Id != id || movie.Title == "" {
		return nil, errors.New("TMDb 电影详情缺少有效身份或与请求不符")
	}
	return p.movieRecord(&movie), nil
}

func (p *metadataProvider) fetchShow(ctx context.Context, id int, group *TvEpisodeGroupDetail) (*metadata.Record, error) {
	var show TvDetail
	args := url.Values{"append_to_response": {"aggregate_credits,content_ratings,images,external_ids"}, "include_image_language": {"zh,en,null"}}
	if err := p.requestJSON(ctx, fmt.Sprintf(ApiTvDetail, id), args, &show); err != nil {
		return nil, err
	}
	if show.Id != id || show.Name == "" {
		return nil, errors.New("TMDb 节目详情缺少有效身份或与请求不符")
	}
	record := p.showRecord(&show)
	if group != nil {
		record.Seasons = nil
		record.SeasonCount = len(group.Groups)
		record.EpisodeCount = 0
		for _, season := range group.Groups {
			record.Seasons = append(record.Seasons, metadata.Season{Number: season.Order, Title: season.Name})
			record.EpisodeCount += len(season.Episodes)
		}
		// 原季海报不能直接冒充重排后的分组季海报。
		artwork := record.Artwork[:0]
		for _, item := range record.Artwork {
			if item.Kind != "season_poster" {
				artwork = append(artwork, item)
			}
		}
		record.Artwork = artwork
	}
	return record, nil
}

func (p *metadataProvider) imageURL(path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimRight(p.config.ImageHost, "/") + "/t/p/original/" + strings.TrimLeft(path, "/")
}
