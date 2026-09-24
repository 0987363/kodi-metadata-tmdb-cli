package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

const maxAppendedPaths = 20

type seriesFacts struct {
	show  *TvDetail
	paths map[string]json.RawMessage
}

type seasonEpisodes struct {
	SeasonNumber int               `json:"season_number"`
	Episodes     []TvEpisodeDetail `json:"episodes"`
}

type episodeSource struct {
	season  int
	episode int
	id      int
}

var _ metadata.SeriesProvider = (*metadataProvider)(nil)

// FetchSeries 在同一节目内批量读取来源事实，并把结果映射回请求坐标。
func (p *metadataProvider) FetchSeries(ctx context.Context, req metadata.SeriesRequest) (*metadata.SeriesResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := metadata.ValidateSeriesRequest(req); err != nil {
		return nil, err
	}
	if req.Ref.Provider != p.Name() || req.Ref.Kind != metadata.Show {
		return nil, errors.New("TMDb 节目引用的数据源或对象类型不匹配")
	}
	showID, err := strconv.Atoi(req.Ref.ID)
	if err != nil || showID <= 0 || strconv.Itoa(showID) != req.Ref.ID {
		return nil, errors.New("TMDb 节目编号必须为正整数")
	}

	keys := metadata.UniqueEpisodeKeys(req.Episodes)

	select {
	case p.seriesGate <- struct{}{}:
		defer func() { <-p.seriesGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.seriesShows == nil {
		p.seriesShows = make(map[int]*seriesFacts)
	}
	if p.seriesGroups == nil {
		p.seriesGroups = make(map[string]*TvEpisodeGroupDetail)
	}
	facts := p.seriesShows[showID]
	if facts == nil {
		facts = &seriesFacts{paths: make(map[string]json.RawMessage)}
		p.seriesShows[showID] = facts
	}
	groups := make(map[string]*TvEpisodeGroupDetail)
	sources := make(map[metadata.EpisodeKey]episodeSource, len(keys))
	seasonPaths := make([]string, 0)
	neededSeasons := make(map[int]bool)
	for _, key := range keys {
		source := episodeSource{season: key.Season, episode: key.Episode}
		if key.Group != "" {
			group := groups[key.Group]
			if group == nil {
				groupCacheKey := fmt.Sprintf("%d:%s", showID, key.Group)
				group = p.seriesGroups[groupCacheKey]
				if group == nil {
					group, err = p.fetchGroup(ctx, key.Group, showID)
					if err != nil {
						return nil, err
					}
					p.seriesGroups[groupCacheKey] = group
				}
				groups[key.Group] = group
			}
			member, err := groupedEpisode(group, key.Season, key.Episode)
			if err != nil {
				return nil, err
			}
			source = episodeSource{season: member.SeasonNumber, episode: member.EpisodeNumber, id: member.Id}
		}
		sources[key] = source
		neededSeasons[source.season] = true
	}
	for _, key := range keys {
		season := sources[key].season
		path := fmt.Sprintf("season/%d", season)
		if !neededSeasons[season] {
			continue
		}
		neededSeasons[season] = false
		if _, cached := facts.paths[path]; !cached {
			seasonPaths = append(seasonPaths, path)
		}
	}
	paths := seasonPaths
	if facts.show == nil {
		paths = append([]string{"aggregate_credits", "content_ratings", "images", "external_ids"}, paths...)
	}
	if len(paths) == 0 && facts.show == nil {
		if err := p.requestSeriesPaths(ctx, showID, facts, nil); err != nil {
			return nil, err
		}
	} else if err := p.requestSeriesPaths(ctx, showID, facts, paths); err != nil {
		return nil, err
	}

	episodes := make(map[metadata.EpisodeKey]TvEpisodeDetail, len(keys))
	for _, key := range keys {
		source := sources[key]
		var season seasonEpisodes
		if err := json.Unmarshal(facts.paths[fmt.Sprintf("season/%d", source.season)], &season); err != nil {
			return nil, fmt.Errorf("解析 TMDb 季单集：%w", err)
		}
		if season.SeasonNumber != source.season {
			return nil, fmt.Errorf("TMDb 第 %d 季身份或单集列表无效", source.season)
		}
		found := false
		var matched TvEpisodeDetail
		for _, episode := range season.Episodes {
			if episode.EpisodeNumber != source.episode {
				continue
			}
			if episode.Id <= 0 || episode.Name == "" || episode.SeasonNumber != source.season || (episode.ShowID != 0 && episode.ShowID != showID) || (source.id != 0 && episode.Id != source.id) {
				return nil, fmt.Errorf("TMDb 第 %d 季第 %d 集身份与请求不符", source.season, source.episode)
			}
			if found {
				return nil, fmt.Errorf("TMDb 第 %d 季第 %d 集重复出现", source.season, source.episode)
			}
			matched = episode
			found = true
		}
		if !found {
			return nil, fmt.Errorf("TMDb 第 %d 季第 %d 集缺失：%w", source.season, source.episode, metadata.ErrNotFound)
		}
		episodes[key] = matched
	}
	if req.Details {
		var extraPaths []string
		planned := make(map[string]bool)
		for _, key := range keys {
			source := sources[key]
			prefix := fmt.Sprintf("season/%d/episode/%d/", source.season, source.episode)
			for _, extension := range []string{"credits", "images", "external_ids"} {
				path := prefix + extension
				if _, cached := facts.paths[path]; !cached && !planned[path] {
					extraPaths = append(extraPaths, path)
					planned[path] = true
				}
			}
		}
		if err := p.requestSeriesPaths(ctx, showID, facts, extraPaths); err != nil {
			return nil, err
		}
	}

	work := p.showRecord(facts.show)
	if len(groups) > 0 {
		oneGroup := keys[0].Group != ""
		for _, key := range keys {
			if key.Group != keys[0].Group {
				oneGroup = false
				break
			}
		}
		if oneGroup {
			applyEpisodeGroup(work, groups[keys[0].Group])
		} else {
			clearEpisodeGroupSeasons(work)
		}
	}
	result := &metadata.SeriesResult{Work: work, Episodes: make([]metadata.SeriesEpisode, 0, len(keys))}
	for _, key := range keys {
		episode := episodes[key]
		if req.Details {
			source := sources[key]
			prefix := fmt.Sprintf("season/%d/episode/%d/", source.season, source.episode)
			if err := json.Unmarshal(facts.paths[prefix+"credits"], &episode.Credits); err != nil {
				return nil, fmt.Errorf("解析 TMDb 单集演职员：%w", err)
			}
			if err := json.Unmarshal(facts.paths[prefix+"images"], &episode.Images); err != nil {
				return nil, fmt.Errorf("解析 TMDb 单集图片：%w", err)
			}
			if err := json.Unmarshal(facts.paths[prefix+"external_ids"], &episode.ExternalIDs); err != nil {
				return nil, fmt.Errorf("解析 TMDb 单集外部编号：%w", err)
			}
		}
		record := p.episodeRecord(&episode, showID)
		record.SeasonNumber, record.EpisodeNumber = key.Season, key.Episode
		result.Episodes = append(result.Episodes, metadata.SeriesEpisode{Key: key, Record: record})
	}
	if err := metadata.ValidateSeriesResult(req, result); err != nil {
		return nil, err
	}
	return result, nil
}

// requestSeriesPaths 只缓存已经逐项确认返回的追加对象。
func (p *metadataProvider) requestSeriesPaths(ctx context.Context, showID int, facts *seriesFacts, paths []string) error {
	for start := 0; start < len(paths) || (start == 0 && facts.show == nil); start += maxAppendedPaths {
		end := min(start+maxAppendedPaths, len(paths))
		batch := paths[start:end]
		args := url.Values{"include_image_language": {"zh,en,null"}}
		if len(batch) > 0 {
			args.Set("append_to_response", strings.Join(batch, ","))
		}
		var response map[string]json.RawMessage
		if err := p.requestJSON(ctx, fmt.Sprintf(ApiTvDetail, showID), args, &response); err != nil {
			return err
		}
		var identity struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		body, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &identity); err != nil {
			return err
		}
		if identity.ID != showID || identity.Name == "" {
			return errors.New("TMDb 节目详情缺少有效身份或与请求不符")
		}
		for _, path := range batch {
			value, ok := response[path]
			if !ok || !validAppendedObject(path, value) {
				return fmt.Errorf("TMDb 追加对象 %s 缺失或无效", path)
			}
		}
		if facts.show == nil {
			var show TvDetail
			if err := json.Unmarshal(body, &show); err != nil {
				return fmt.Errorf("解析 TMDb 节目详情：%w", err)
			}
			facts.show = &show
		}
		for _, path := range batch {
			facts.paths[path] = append(json.RawMessage(nil), response[path]...)
		}
	}
	return nil
}

func validAppendedObject(path string, value json.RawMessage) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil || len(object) == 0 {
		return false
	}
	if raw, ok := object["success"]; ok && string(raw) == "false" {
		return false
	}
	if strings.HasPrefix(path, "season/") {
		parts := strings.Split(path, "/")
		if len(parts) == 2 {
			var episodes []json.RawMessage
			if err := json.Unmarshal(object["episodes"], &episodes); err != nil || episodes == nil {
				return false
			}
			return len(object["season_number"]) > 0
		}
		if len(parts) != 5 {
			return false
		}
		switch parts[4] {
		case "credits":
			return hasArray(object, "cast") && hasArray(object, "crew")
		case "images":
			return hasArray(object, "stills")
		case "external_ids":
			return hasExternalIDField(object)
		default:
			return false
		}
	}
	switch path {
	case "aggregate_credits":
		return hasArray(object, "cast") && hasArray(object, "crew")
	case "content_ratings":
		return hasArray(object, "results")
	case "images":
		return hasArray(object, "posters") || hasArray(object, "backdrops") || hasArray(object, "logos")
	case "external_ids":
		return hasExternalIDField(object)
	default:
		return false
	}
}

func hasArray(object map[string]json.RawMessage, field string) bool {
	var values []json.RawMessage
	return json.Unmarshal(object[field], &values) == nil && values != nil
}

func hasExternalIDField(object map[string]json.RawMessage) bool {
	for key := range object {
		if strings.HasSuffix(key, "_id") {
			return true
		}
	}
	return false
}

func applyEpisodeGroup(record *metadata.Record, group *TvEpisodeGroupDetail) {
	record.Seasons = nil
	record.SeasonCount = len(group.Groups)
	record.EpisodeCount = 0
	for _, season := range group.Groups {
		record.Seasons = append(record.Seasons, metadata.Season{Number: season.Order, Title: season.Name})
		record.EpisodeCount += len(season.Episodes)
	}
	removeSeasonPosters(record)
}

func clearEpisodeGroupSeasons(record *metadata.Record) {
	record.Seasons = nil
	record.SeasonCount = 0
	record.EpisodeCount = 0
	removeSeasonPosters(record)
}

func removeSeasonPosters(record *metadata.Record) {
	artwork := record.Artwork[:0]
	for _, item := range record.Artwork {
		if item.Kind != "season_poster" {
			artwork = append(artwork, item)
		}
	}
	record.Artwork = artwork
}
