package tmdb

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fmt"
	"net/url"
	"sort"
)

func (p *metadataProvider) fetchGroup(ctx context.Context, groupID string, showID int) (*TvEpisodeGroupDetail, error) {
	var group TvEpisodeGroupDetail
	if err := p.requestJSON(ctx, fmt.Sprintf(ApiTvEpisodeGroup, url.PathEscape(groupID)), nil, &group); err != nil {
		return nil, err
	}
	if group.Id != groupID || len(group.Groups) == 0 {
		return nil, errors.New("TMDb 分组缺少有效身份或季信息")
	}
	seasonOrders := make(map[int]bool)
	for _, season := range group.Groups {
		if season.Order < 0 || seasonOrders[season.Order] {
			return nil, errors.New("TMDb 分组季序号无效或重复")
		}
		seasonOrders[season.Order] = true
		episodeOrders := make(map[int]bool)
		for _, episode := range season.Episodes {
			if episode.ShowId != 0 && episode.ShowId != showID {
				return nil, fmt.Errorf("%w: TMDb 分组成员属于其他节目", metadata.ErrConstraintMismatch)
			}
			if episode.Id <= 0 || episode.SeasonNumber < 0 || episode.EpisodeNumber <= 0 || episode.Order < 0 || episodeOrders[episode.Order] {
				return nil, errors.New("TMDb 分组单集身份或顺序无效")
			}
			episodeOrders[episode.Order] = true
		}
	}
	// 每次请求独立解码，只对本次响应排序，不改动其他请求的对象。
	sort.Slice(group.Groups, func(i, j int) bool { return group.Groups[i].Order < group.Groups[j].Order })
	return &group, nil
}

func groupedEpisode(group *TvEpisodeGroupDetail, seasonNumber, episodeNumber int) (*TvEpisodeGroupEpisode, error) {
	for _, season := range group.Groups {
		if season.Order != seasonNumber {
			continue
		}
		episodes := append([]TvEpisodeGroupEpisode(nil), season.Episodes...)
		sort.SliceStable(episodes, func(i, j int) bool { return episodes[i].Order < episodes[j].Order })
		if episodeNumber < 1 || episodeNumber > len(episodes) {
			return nil, errors.New("TMDb 分组集号越界")
		}
		return &episodes[episodeNumber-1], nil
	}
	return nil, errors.New("TMDb 分组季号越界")
}
