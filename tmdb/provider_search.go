package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

type providerSearchResponse struct {
	Page         *int                 `json:"page"`
	TotalResults *int                 `json:"total_results"`
	TotalPages   *int                 `json:"total_pages"`
	Results      []*providerSearchHit `json:"results"`
}

type providerSearchHit struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate   string `json:"release_date"`
	Name          string `json:"name"`
	OriginalName  string `json:"original_name"`
	FirstAirDate  string `json:"first_air_date"`
}

func (p *metadataProvider) Search(ctx context.Context, query metadata.Query) ([]metadata.Candidate, error) {
	if query.Kind != metadata.Movie && query.Kind != metadata.Show {
		return nil, metadata.ErrUnsupported
	}
	titles := uniqueStrings(strings.TrimSpace(query.Title), strings.TrimSpace(query.ChineseTitle), strings.TrimSpace(query.OriginalTitle))
	if len(titles) == 0 {
		return nil, errors.New("TMDb 搜索标题为空")
	}
	var candidates []metadata.Candidate
	seen := make(map[string]bool)
	for _, title := range titles {
		args := url.Values{"query": {title}, "include_adult": {"true"}}
		if query.Year > 0 {
			field := "primary_release_year"
			if query.Kind == metadata.Show {
				field = "first_air_date_year"
			}
			args.Set(field, strconv.Itoa(query.Year))
		}
		results, err := p.searchAllPages(ctx, query.Kind, args)
		if err != nil {
			return nil, err
		}
		for _, candidate := range results {
			if seen[candidate.Ref.ID] {
				continue
			}
			if len(candidates) == metadata.MaxDecisionCandidates {
				return nil, fmt.Errorf("TMDb 搜索候选超过 %d 条，无法完整判断", metadata.MaxDecisionCandidates)
			}
			seen[candidate.Ref.ID] = true
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil, metadata.ErrNotFound
	}
	return candidates, nil
}

func (p *metadataProvider) searchAllPages(ctx context.Context, kind metadata.Kind, args url.Values) ([]metadata.Candidate, error) {
	path := ApiSearchMovie
	if kind == metadata.Show {
		path = ApiSearchTv
	}
	var candidates []metadata.Candidate
	seen := make(map[int]bool)
	totalPages, totalResults, received := 0, 0, 0
	for page := 1; ; page++ {
		args.Set("page", strconv.Itoa(page))
		var response providerSearchResponse
		if err := p.requestJSON(ctx, path, args, &response); err != nil {
			if errors.Is(err, metadata.ErrNotFound) {
				return nil, fmt.Errorf("TMDb 搜索接口第 %d 页返回 HTTP 404", page)
			}
			return nil, err
		}
		if err := response.validate(page); err != nil {
			return nil, err
		}
		if page == 1 {
			totalPages, totalResults = *response.TotalPages, *response.TotalResults
		} else if totalPages != *response.TotalPages || totalResults != *response.TotalResults {
			return nil, errors.New("TMDb 搜索分页总数发生变化")
		}
		received += len(response.Results)
		if received > totalResults {
			return nil, errors.New("TMDb 搜索分页条数超过声明总数")
		}
		before := len(candidates)
		for _, hit := range response.Results {
			if hit == nil || hit.ID <= 0 {
				return nil, errors.New("TMDb 搜索候选缺少有效编号")
			}
			title, originalTitle, date := hit.Title, hit.OriginalTitle, hit.ReleaseDate
			if kind == metadata.Show {
				title, originalTitle, date = hit.Name, hit.OriginalName, hit.FirstAirDate
			}
			if strings.TrimSpace(title) == "" {
				return nil, errors.New("TMDb 搜索候选缺少有效标题")
			}
			if seen[hit.ID] {
				continue
			}
			seen[hit.ID] = true
			candidates = append(candidates, metadata.Candidate{Ref: metadata.Ref{Provider: p.Name(), Kind: kind, ID: strconv.Itoa(hit.ID)}, Title: title, OriginalTitle: originalTitle, Year: parseYear(date)})
		}
		if page > 1 && before == len(candidates) {
			return nil, errors.New("TMDb 搜索分页重复且没有新增候选")
		}
		if page >= totalPages {
			if received != totalResults {
				return nil, errors.New("TMDb 搜索分页条数与声明总数不符")
			}
			return candidates, nil
		}
	}
}

func (r providerSearchResponse) validate(page int) error {
	if r.Results == nil || r.Page == nil || r.TotalPages == nil || r.TotalResults == nil {
		return errors.New("TMDb 搜索响应缺少候选列表或分页元数据")
	}
	if *r.Page != page || *r.TotalPages < 0 || *r.TotalResults < 0 {
		return errors.New("TMDb 搜索分页元数据无效")
	}
	if *r.TotalPages > metadata.MaxDecisionCandidates || *r.TotalResults > metadata.MaxDecisionCandidates {
		return fmt.Errorf("TMDb 搜索页数或条数超过 %d，无法完整判断", metadata.MaxDecisionCandidates)
	}
	if len(r.Results) == 0 {
		// TMDb 的零结果第一页可能仍计为一页。
		if page != 1 || *r.TotalPages > 1 || *r.TotalResults != 0 {
			return errors.New("TMDb 空搜索页与分页总数矛盾")
		}
		return nil
	}
	if *r.TotalPages < page || *r.TotalResults < *r.TotalPages || len(r.Results) > *r.TotalResults {
		return errors.New("TMDb 搜索候选与分页总数矛盾")
	}
	return nil
}
