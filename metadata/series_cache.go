package metadata

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

func (m *Manager) fetchSeries(ctx context.Context, provider Provider, req SeriesRequest, root string) (*SeriesResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateSeriesRequest(req); err != nil {
		return nil, err
	}
	if provider.Name() != req.Ref.Provider {
		return nil, errors.New("批量请求来源与 Provider 不一致")
	}
	batch, ok := provider.(SeriesProvider)
	if !ok {
		return nil, errors.New("来源未实现批量剧集获取")
	}
	req.Episodes = UniqueEpisodeKeys(req.Episodes)
	slices.SortFunc(req.Episodes, func(a, b EpisodeKey) int {
		if result := cmp.Compare(a.Group, b.Group); result != 0 {
			return result
		}
		if result := cmp.Compare(a.Season, b.Season); result != 0 {
			return result
		}
		return cmp.Compare(a.Episode, b.Episode)
	})
	key, err := json.Marshal(struct {
		Version int
		Scope   string
		Request SeriesRequest
	}{1, provider.Scope(), req})
	if err != nil {
		return nil, err
	}
	file := filepath.Join(root, "cache", provider.Name(), fmt.Sprintf("series-%x.json", sha256.Sum256(key)))
	result, hit, err := readCache[*SeriesResult](file, m.ttl)
	if err != nil {
		return nil, err
	}
	if !hit {
		result, err = batch.FetchSeries(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	if err := ValidateSeriesResult(req, result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !hit {
		if err := writeCache(file, m.ttl, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
