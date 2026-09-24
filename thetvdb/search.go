package thetvdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

type searchPayload struct {
	Requests []searchRequest `json:"requests"`
}
type searchRequest struct {
	IndexName string       `json:"indexName"`
	Params    searchParams `json:"params"`
}
type searchParams struct {
	Query       string `json:"query"`
	Filters     string `json:"filters"`
	HitsPerPage int    `json:"hitsPerPage"`
	Page        int    `json:"page"`
}
type searchResponse struct {
	Results []searchResult `json:"results"`
}
type searchResult struct {
	Page             *int         `json:"page"`
	TotalPages       *int         `json:"nbPages"`
	TotalHits        *int         `json:"nbHits"`
	HitsPerPage      *int         `json:"hitsPerPage"`
	ExhaustiveNbHits *bool        `json:"exhaustiveNbHits"`
	Hits             []*searchHit `json:"hits"`
}
type searchHit struct {
	ID           json.Number       `json:"id"`
	Name         string            `json:"name"`
	Slug         string            `json:"slug"`
	Type         string            `json:"type"`
	Year         string            `json:"year"`
	Translations map[string]string `json:"translations"`
}

func (p *Provider) Search(ctx context.Context, query metadata.Query) ([]metadata.Candidate, error) {
	if query.Kind != metadata.Show {
		return nil, metadata.ErrUnsupported
	}
	var titles []string
	seenTitles := make(map[string]bool)
	for _, title := range []string{query.Title, query.ChineseTitle, query.OriginalTitle} {
		title = strings.TrimSpace(title)
		if title != "" && !seenTitles[title] {
			titles = append(titles, title)
			seenTitles[title] = true
		}
	}
	if len(titles) == 0 {
		return nil, fmt.Errorf("TheTVDB 搜索标题不能为空")
	}
	endpoint, err := p.searchEndpoint(ctx, titles[0])
	if err != nil {
		return nil, err
	}
	var candidates []metadata.Candidate
	seen := make(map[string]bool)
	for _, title := range titles {
		results, err := p.searchAllPages(ctx, endpoint, title)
		if err != nil {
			return nil, err
		}
		for _, candidate := range results {
			if seen[candidate.Ref.ID] {
				continue
			}
			if len(candidates) == metadata.MaxDecisionCandidates {
				return nil, fmt.Errorf("TheTVDB 搜索候选超过 %d 条，无法完整判断", metadata.MaxDecisionCandidates)
			}
			seen[candidate.Ref.ID] = true
			candidates = append(candidates, candidate)
		}
	}
	if query.Year > 0 {
		matching := candidates[:0]
		for _, candidate := range candidates {
			if candidate.Year == query.Year {
				matching = append(matching, candidate)
			}
		}
		candidates = matching
	}
	if len(candidates) == 0 {
		return nil, metadata.ErrNotFound
	}
	return candidates, nil
}

func (p *Provider) searchEndpoint(ctx context.Context, title string) (string, error) {
	page, _, err := p.request(ctx, http.MethodGet, p.baseURL+"/search?query="+url.QueryEscape(title), nil)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return "", fmt.Errorf("TheTVDB 搜索入口返回 HTTP 404")
		}
		return "", err
	}
	matches := regexp.MustCompile(`window\.TVDB_SEARCH_URL\s*=\s*['"]([^'"]+)['"]`).FindSubmatch(page)
	if len(matches) != 2 {
		return "", fmt.Errorf("TheTVDB 搜索页缺少公开搜索地址")
	}
	endpoint := string(matches[1])
	parsed, err := url.Parse(endpoint)
	if err != nil || !p.allowedURL(parsed) {
		return "", fmt.Errorf("TheTVDB 搜索地址不属于配置站点")
	}
	return endpoint, nil
}

func (p *Provider) searchAllPages(ctx context.Context, endpoint, title string) ([]metadata.Candidate, error) {
	var candidates []metadata.Candidate
	seen := make(map[string]bool)
	totalPages, totalHits, pageSize, received := 0, 0, 0, 0
	for page := 0; ; page++ {
		result, err := p.searchPage(ctx, endpoint, title, page)
		if err != nil {
			return nil, err
		}
		if page == 0 {
			totalPages, totalHits, pageSize = *result.TotalPages, *result.TotalHits, *result.HitsPerPage
		} else if totalPages != *result.TotalPages || totalHits != *result.TotalHits || pageSize != *result.HitsPerPage {
			return nil, fmt.Errorf("TheTVDB 搜索分页总数或容量发生变化")
		}
		received += len(result.Hits)
		if received > totalHits {
			return nil, fmt.Errorf("TheTVDB 搜索分页条数超过声明总数")
		}
		before := len(seen)
		for _, hit := range result.Hits {
			if hit == nil || hit.Type != "series" {
				return nil, fmt.Errorf("TheTVDB 搜索候选不是有效的节目对象")
			}
			if !positiveID(hit.ID.String()) || strings.TrimSpace(hit.Name) == "" || strings.ContainsAny(hit.Slug, "/\\?#") {
				return nil, fmt.Errorf("TheTVDB 搜索候选缺少有效节目编号或名称")
			}
			if seen[hit.ID.String()] {
				continue
			}
			seen[hit.ID.String()] = true
			title := hit.Translations[languageCode(p.language)]
			if title == "" {
				title = hit.Name
			}
			year, _ := strconv.Atoi(hit.Year)
			candidates = append(candidates, metadata.Candidate{Ref: metadata.Ref{Provider: p.Name(), Kind: metadata.Show, ID: hit.ID.String(), Slug: hit.Slug}, Title: title, OriginalTitle: hit.Name, Year: year})
		}
		if page > 0 && before == len(seen) {
			return nil, fmt.Errorf("TheTVDB 搜索分页重复且没有新增候选")
		}
		if page+1 >= totalPages {
			if received != totalHits {
				return nil, fmt.Errorf("TheTVDB 搜索分页条数与声明总数不符")
			}
			return candidates, nil
		}
	}
}

func (p *Provider) searchPage(ctx context.Context, endpoint, title string, page int) (searchResult, error) {
	body, err := json.Marshal(searchPayload{Requests: []searchRequest{{IndexName: "TVDB", Params: searchParams{Query: title, Filters: "type:series", HitsPerPage: 20, Page: page}}}})
	if err != nil {
		return searchResult{}, fmt.Errorf("编码 TheTVDB 搜索请求: %w", err)
	}
	data, _, err := p.request(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return searchResult{}, fmt.Errorf("TheTVDB 搜索接口第 %d 页返回 HTTP 404", page+1)
		}
		return searchResult{}, err
	}
	var response searchResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return searchResult{}, fmt.Errorf("解析 TheTVDB 搜索响应: %w", err)
	}
	if len(response.Results) != 1 {
		return searchResult{}, fmt.Errorf("TheTVDB 搜索响应缺少候选结构")
	}
	result := response.Results[0]
	if err := result.validate(page); err != nil {
		return searchResult{}, err
	}
	return result, nil
}

func (r searchResult) validate(page int) error {
	if r.Hits == nil || r.Page == nil || r.TotalPages == nil || r.TotalHits == nil || r.HitsPerPage == nil || r.ExhaustiveNbHits == nil {
		return fmt.Errorf("TheTVDB 搜索响应缺少候选结构或分页元数据")
	}
	if *r.Page != page || *r.TotalPages < 0 || *r.TotalHits < 0 || *r.HitsPerPage <= 0 || !*r.ExhaustiveNbHits {
		return fmt.Errorf("TheTVDB 搜索分页元数据无效或总数不完整")
	}
	if *r.TotalPages > metadata.MaxDecisionCandidates || *r.TotalHits > metadata.MaxDecisionCandidates {
		return fmt.Errorf("TheTVDB 搜索页数或条数超过 %d，无法完整判断", metadata.MaxDecisionCandidates)
	}
	if len(r.Hits) == 0 {
		if page != 0 || *r.TotalPages != 0 || *r.TotalHits != 0 {
			return fmt.Errorf("TheTVDB 空搜索页与分页总数矛盾")
		}
		return nil
	}
	pages := 1 + (*r.TotalHits-1) / *r.HitsPerPage
	if *r.TotalHits == 0 || *r.TotalPages != pages || page >= pages || len(r.Hits) != min(*r.HitsPerPage, *r.TotalHits-page**r.HitsPerPage) {
		return fmt.Errorf("TheTVDB 搜索候选与分页总数矛盾")
	}
	return nil
}
