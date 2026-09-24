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
	"time"

	"fengqi/kodi-metadata-tmdb-cli/common/httpx"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

type metadataProvider struct {
	config config.TmdbConfig
	client *http.Client
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
	return &metadataProvider{config: settings, client: httpx.NewClient(settings.Proxy, settings.TimeoutSeconds)}
}

func (p *metadataProvider) Name() string { return "tmdb" }

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
	return kind == metadata.Movie || kind == metadata.Show || kind == metadata.Episode
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
		body, requestErr := p.requestOnce(ctx, endpoint.String())
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

func (p *metadataProvider) requestOnce(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("TMDb 请求地址无效")
	}
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
	expectedKind := req.Kind
	if req.Kind == metadata.Episode {
		expectedKind = metadata.Show
	}
	if req.Ref.Provider != p.Name() || req.Ref.Kind != expectedKind {
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
	if req.Kind == metadata.Episode && (req.Season < 0 || req.Episode <= 0) {
		return nil, errors.New("TMDb 单集季集号无效")
	}
	var group *TvEpisodeGroupDetail
	if req.Group != "" {
		group, err = p.fetchGroup(ctx, req.Group, id)
		if err != nil {
			return nil, err
		}
	}
	if req.Kind == metadata.Show {
		return p.fetchShow(ctx, id, group)
	}
	return p.fetchEpisode(ctx, id, req, group)
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

func (p *metadataProvider) fetchEpisode(ctx context.Context, showID int, req metadata.Request, group *TvEpisodeGroupDetail) (*metadata.Record, error) {
	season, episode := req.Season, req.Episode
	expectedID := 0
	if group != nil {
		member, err := groupedEpisode(group, season, episode)
		if err != nil {
			return nil, err
		}
		season, episode, expectedID = member.SeasonNumber, member.EpisodeNumber, member.Id
	}
	var detail TvEpisodeDetail
	args := url.Values{"append_to_response": {"credits,images,external_ids"}}
	if err := p.requestJSON(ctx, fmt.Sprintf(ApiTvEpisode, showID, season, episode), args, &detail); err != nil {
		return nil, err
	}
	if detail.Id <= 0 || detail.Name == "" || detail.SeasonNumber != season || detail.EpisodeNumber != episode || (detail.ShowID != 0 && detail.ShowID != showID) || (expectedID != 0 && detail.Id != expectedID) {
		return nil, errors.New("TMDb 单集详情身份与请求不符")
	}
	record := p.episodeRecord(&detail, showID)
	record.SeasonNumber, record.EpisodeNumber = req.Season, req.Episode
	return record, nil
}

func (p *metadataProvider) imageURL(path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimRight(p.config.ImageHost, "/") + "/t/p/original/" + strings.TrimLeft(path, "/")
}
