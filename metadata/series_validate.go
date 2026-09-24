package metadata

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateSeriesRequest(req SeriesRequest) error {
	if req.Ref.Provider == "" || req.Ref.Kind != Show || req.Ref.ID == "" && req.Ref.Slug == "" {
		return errors.New("批量剧集请求缺少来源和节目引用")
	}
	if len(req.Episodes) == 0 {
		return errors.New("批量剧集请求没有目标单集")
	}
	for _, key := range req.Episodes {
		if key.Season < 0 || key.Episode < 1 {
			return errors.New("批量剧集请求包含无效季集坐标")
		}
	}
	return nil
}

// UniqueEpisodeKeys 保留首次出现的请求顺序，同一单集的本地版本共用事实。
func UniqueEpisodeKeys(keys []EpisodeKey) []EpisodeKey {
	out := make([]EpisodeKey, 0, len(keys))
	seen := make(map[EpisodeKey]bool, len(keys))
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func ValidateSeriesResult(req SeriesRequest, result *SeriesResult) error {
	if err := ValidateSeriesRequest(req); err != nil {
		return err
	}
	if result == nil || result.Work == nil {
		return errors.New("批量剧集响应缺少节目事实")
	}
	work := result.Work
	if work.Ref.Provider != req.Ref.Provider || work.Ref.Kind != Show || work.Ref.ID == "" || strings.TrimSpace(work.Title) == "" || req.Ref.ID != "" && work.Ref.ID != req.Ref.ID {
		return errors.New("批量剧集响应的节目身份无效或不匹配")
	}
	wanted := make(map[EpisodeKey]bool, len(req.Episodes))
	for _, key := range req.Episodes {
		wanted[key] = true
	}
	seen := make(map[EpisodeKey]bool, len(result.Episodes))
	for _, item := range result.Episodes {
		if !wanted[item.Key] || seen[item.Key] {
			return errors.New("批量剧集响应含未请求或重复的单集")
		}
		r := item.Record
		if r == nil || r.Ref.Provider != work.Ref.Provider || r.Ref.Kind != Episode || r.Ref.ID == "" || strings.TrimSpace(r.Title) == "" || r.SeasonNumber != item.Key.Season || r.EpisodeNumber != item.Key.Episode {
			return fmt.Errorf("批量剧集响应 S%02dE%02d 身份或坐标不符", item.Key.Season, item.Key.Episode)
		}
		seen[item.Key] = true
	}
	if len(seen) != len(wanted) {
		return errors.New("批量剧集响应缺少目标单集")
	}
	return nil
}
