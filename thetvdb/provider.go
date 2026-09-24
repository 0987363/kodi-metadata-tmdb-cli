package thetvdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

const maxResponseBytes = 8 << 20

// Provider 从 TheTVDB 的公开网页读取节目和正式播出顺序的单集。
type Provider struct {
	client      *http.Client
	baseURL     string
	language    string
	interval    time.Duration
	rateMu      sync.Mutex
	nextRequest time.Time
	cacheMu     sync.Mutex
	seasons     map[string]*seasonCache
}

type seasonCache struct {
	ready    chan struct{}
	expires  time.Time
	episodes []episodeEntry
	err      error
}

type episodeEntry struct {
	id     string
	season int
	number int
	title  string
	plot   string
	aired  string
	url    string
}

var _ metadata.Provider = (*Provider)(nil)

// New 复用调用者的超时设置，并将重定向限制在配置站点内。
func New(client *http.Client, baseURL string, language string, interval time.Duration) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://thetvdb.com"
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copied := *client
	priorRedirect := copied.CheckRedirect
	provider := &Provider{client: &copied, baseURL: strings.TrimRight(baseURL, "/"), language: language, interval: interval, seasons: make(map[string]*seasonCache)}
	copied.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !provider.allowedURL(req.URL) {
			return fmt.Errorf("TheTVDB 重定向离开配置站点")
		}
		if len(via) >= 10 {
			return fmt.Errorf("TheTVDB 重定向次数过多")
		}
		if priorRedirect != nil {
			return priorRedirect(req, via)
		}
		return nil
	}
	return provider
}

func (p *Provider) Name() string  { return "thetvdb" }
func (p *Provider) Scope() string { return p.baseURL + "|" + p.language }
func (p *Provider) Supports(kind metadata.Kind) bool {
	return kind == metadata.Show || kind == metadata.Episode
}

func (p *Provider) Fetch(ctx context.Context, req metadata.Request) (*metadata.Record, error) {
	if !p.Supports(req.Kind) || req.Group != "" {
		return nil, metadata.ErrUnsupported
	}
	if req.Ref.Provider != "" && req.Ref.Provider != p.Name() {
		return nil, metadata.ErrUnsupported
	}
	if req.Ref.Kind != "" && req.Ref.Kind != metadata.Show {
		return nil, metadata.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := p.showURL(req.Ref)
	if err != nil {
		return nil, err
	}
	doc, finalURL, err := p.getHTML(ctx, target)
	if err != nil {
		return nil, err
	}
	record, err := parseShow(doc, finalURL, p.language, req.Ref.ID)
	if err != nil {
		return nil, err
	}
	if req.Kind == metadata.Show {
		return record, nil
	}
	if req.Season < 0 || req.Episode <= 0 {
		return nil, fmt.Errorf("TheTVDB 单集必须指定非负季号和正集号")
	}
	episodes, err := p.allSeasons(ctx, record.SourceURL+"/allseasons/official", record.Ref.Slug)
	if err != nil {
		return nil, err
	}
	var match *episodeEntry
	for i := range episodes {
		if episodes[i].season != req.Season || episodes[i].number != req.Episode {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("TheTVDB S%02dE%02d 对应多个单集编号: %w", req.Season, req.Episode, metadata.ErrAmbiguous)
		}
		match = &episodes[i]
	}
	if match == nil {
		return nil, metadata.ErrNotFound
	}
	detail, detailURL, err := p.getHTML(ctx, match.url)
	if err != nil {
		return nil, err
	}
	return parseEpisode(detail, detailURL, p.language, *match)
}

func (p *Provider) showURL(ref metadata.Ref) (string, error) {
	if ref.ID != "" {
		if !positiveID(ref.ID) {
			return "", fmt.Errorf("TheTVDB 节目编号必须是正整数")
		}
		return p.baseURL + "/dereferrer/series/" + ref.ID, nil
	}
	if ref.Slug == "" || strings.ContainsAny(ref.Slug, "/\\?#") || ref.Slug == "." || ref.Slug == ".." {
		return "", fmt.Errorf("TheTVDB 缺少有效的节目编号或 slug")
	}
	return p.baseURL + "/series/" + url.PathEscape(ref.Slug), nil
}

func (p *Provider) allowedURL(target *url.URL) bool {
	base, err := url.Parse(p.baseURL)
	if err != nil || target == nil || target.User != nil {
		return false
	}
	if target.Scheme == base.Scheme && target.Host == base.Host && (target.Scheme == "http" || target.Scheme == "https") {
		return true
	}
	return tvdbHost(base.Hostname()) && target.Scheme == "https" && tvdbHost(target.Hostname()) && (target.Port() == "" || target.Port() == "443")
}

func tvdbHost(host string) bool {
	host = strings.ToLower(host)
	return host == "thetvdb.com" || strings.HasSuffix(host, ".thetvdb.com")
}

func (p *Provider) waitRequest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.rateMu.Lock()
	now := time.Now()
	wait := p.nextRequest.Sub(now)
	if wait < 0 {
		wait = 0
	}
	p.nextRequest = now.Add(wait).Add(p.interval)
	p.rateMu.Unlock()
	if wait == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Provider) request(ctx context.Context, method, target string, body []byte) ([]byte, string, error) {
	parsed, err := url.Parse(target)
	if err != nil || !p.allowedURL(parsed) {
		return nil, "", fmt.Errorf("TheTVDB 请求地址不属于配置站点")
	}
	if err := p.waitRequest(ctx); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "kodi-metadata-tmdb-cli/1.0 (TheTVDB public metadata)")
	req.Header.Set("Accept-Language", p.language)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("TheTVDB 请求: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", metadata.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("TheTVDB HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取 TheTVDB 响应: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, "", fmt.Errorf("TheTVDB 响应超过 8 MiB")
	}
	return data, resp.Request.URL.String(), nil
}

func (p *Provider) getHTML(ctx context.Context, target string) (*html.Node, string, error) {
	data, finalURL, err := p.request(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", err
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("解析 TheTVDB HTML: %w", err)
	}
	return doc, finalURL, nil
}

func (p *Provider) allSeasons(ctx context.Context, target, slug string) ([]episodeEntry, error) {
	key := target + "|" + p.language
	p.cacheMu.Lock()
	cached := p.seasons[key]
	if cached != nil && time.Now().Before(cached.expires) {
		p.cacheMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-cached.ready:
			return cached.episodes, cached.err
		}
	}
	cached = &seasonCache{ready: make(chan struct{}), expires: time.Now().Add(time.Hour)}
	p.seasons[key] = cached
	p.cacheMu.Unlock()
	doc, finalURL, err := p.getHTML(ctx, target)
	if err == nil {
		cached.episodes, err = parseAllSeasons(doc, finalURL, slug)
	}
	p.cacheMu.Lock()
	cached.err = err
	if err != nil {
		delete(p.seasons, key)
	}
	close(cached.ready)
	p.cacheMu.Unlock()
	return cached.episodes, cached.err
}

func positiveID(value string) bool {
	return regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(value)
}
