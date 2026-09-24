package thetvdb

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

type seriesPage struct {
	ready chan struct{}
	facts seriesFacts
	err   error
}

type seriesFacts struct {
	show    *metadata.Record
	entries []episodeEntry
	season  map[string]seasonEpisode
	detail  *metadata.Record
}

type seasonEpisode struct {
	season  int
	number  int
	runtime int
}

var _ metadata.SeriesProvider = (*Provider)(nil)

func (p *Provider) FetchSeries(ctx context.Context, req metadata.SeriesRequest) (*metadata.SeriesResult, error) {
	if req.Ref.Provider != "" && req.Ref.Provider != p.Name() || req.Ref.Kind != "" && req.Ref.Kind != metadata.Show {
		return nil, metadata.ErrUnsupported
	}
	for _, key := range req.Episodes {
		if key.Group != "" {
			return nil, metadata.ErrUnsupported
		}
	}
	if err := metadata.ValidateSeriesRequest(req); err != nil {
		return nil, err
	}
	req.Episodes = metadata.UniqueEpisodeKeys(req.Episodes)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := p.showURL(req.Ref)
	if err != nil {
		return nil, err
	}
	showPage, err := p.loadSeriesPage(ctx, "show|"+target, func() (seriesFacts, error) {
		doc, finalURL, err := p.getHTML(ctx, target)
		if err != nil {
			return seriesFacts{}, err
		}
		record, err := parseShow(doc, finalURL, p.language, req.Ref.ID)
		return seriesFacts{show: record}, err
	})
	if err != nil {
		return nil, err
	}
	show := showPage.show
	p.cacheShowAliases(target, show)
	result := &metadata.SeriesResult{Work: copyRecord(show)}
	allURL := show.SourceURL + "/allseasons/official"
	allPage, err := p.loadSeriesPage(ctx, "all|"+allURL, func() (seriesFacts, error) {
		doc, finalURL, err := p.getHTML(ctx, allURL)
		if err != nil {
			return seriesFacts{}, err
		}
		entries, err := parseAllSeasons(doc, finalURL, show.Ref.Slug)
		return seriesFacts{entries: entries}, err
	})
	if err != nil {
		return nil, err
	}
	selected := make(map[metadata.EpisodeKey]episodeEntry)
	neededSeasons := make(map[int]bool)
	for _, key := range req.Episodes {
		if _, exists := selected[key]; exists {
			continue
		}
		for _, entry := range allPage.entries {
			if entry.season != key.Season || entry.number != key.Episode {
				continue
			}
			if _, exists := selected[key]; exists {
				return nil, fmt.Errorf("TheTVDB S%02dE%02d 对应多个单集编号: %w", key.Season, key.Episode, metadata.ErrAmbiguous)
			}
			selected[key] = entry
		}
		if _, exists := selected[key]; !exists {
			return nil, metadata.ErrNotFound
		}
		neededSeasons[key.Season] = true
	}
	runtimes := make(map[string]seasonEpisode)
	for season := range neededSeasons {
		seasonURL := fmt.Sprintf("%s/seasons/official/%d", show.SourceURL, season)
		page, err := p.loadSeriesPage(ctx, "season|"+seasonURL, func() (seriesFacts, error) {
			doc, finalURL, err := p.getHTML(ctx, seasonURL)
			if err != nil {
				return seriesFacts{}, err
			}
			rows, err := parseSeasonRuntime(doc, finalURL, show.Ref.Slug, season)
			return seriesFacts{season: rows}, err
		})
		if err != nil {
			return nil, err
		}
		for id, row := range page.season {
			runtimes[id] = row
		}
	}
	for _, key := range req.Episodes {
		entry := selected[key]
		row, ok := runtimes[entry.id]
		if !ok || row.season != key.Season || row.number != key.Episode {
			return nil, fmt.Errorf("TheTVDB 单集 %s 的季表关联不匹配", entry.id)
		}
		record := &metadata.Record{Ref: metadata.Ref{Provider: p.Name(), Kind: metadata.Episode, ID: entry.id}, ExternalIDs: []metadata.Identifier{{Type: "tvdb", Value: entry.id}}, SourceURL: entry.url, Title: entry.title, OriginalTitle: entry.title, Plot: entry.plot, Premiered: entry.aired, SeasonNumber: entry.season, EpisodeNumber: entry.number, RuntimeMinutes: row.runtime}
		if entry.network != "" {
			record.Studios = []string{entry.network}
		}
		if entry.thumb != "" {
			record.Artwork = []metadata.Artwork{{Kind: "thumb", URL: entry.thumb}}
		}
		if req.Details {
			page, err := p.loadSeriesPage(ctx, "detail|"+entry.url, func() (seriesFacts, error) {
				doc, finalURL, err := p.getHTML(ctx, entry.url)
				if err != nil {
					return seriesFacts{}, err
				}
				detail, err := parseEpisode(doc, finalURL, p.language, entry)
				return seriesFacts{detail: detail}, err
			})
			if err != nil {
				return nil, err
			}
			mergeEpisodeDetail(record, page.detail)
		}
		result.Episodes = append(result.Episodes, metadata.SeriesEpisode{Key: key, Record: record})
	}
	if err := metadata.ValidateSeriesResult(req, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *Provider) loadSeriesPage(ctx context.Context, key string, fetch func() (seriesFacts, error)) (seriesFacts, error) {
	if err := ctx.Err(); err != nil {
		return seriesFacts{}, err
	}
	key += "|" + p.language
	p.seriesMu.Lock()
	if page := p.seriesPages[key]; page != nil {
		p.seriesMu.Unlock()
		select {
		case <-ctx.Done():
			return seriesFacts{}, ctx.Err()
		case <-page.ready:
			return page.facts, page.err
		}
	}
	page := &seriesPage{ready: make(chan struct{})}
	p.seriesPages[key] = page
	p.seriesMu.Unlock()
	page.facts, page.err = fetch()
	p.seriesMu.Lock()
	if page.err != nil {
		delete(p.seriesPages, key)
	}
	close(page.ready)
	p.seriesMu.Unlock()
	return page.facts, page.err
}

func (p *Provider) cacheShowAliases(requested string, show *metadata.Record) {
	p.seriesMu.Lock()
	defer p.seriesMu.Unlock()
	page := p.seriesPages["show|"+requested+"|"+p.language]
	if page == nil {
		return
	}
	for _, target := range []string{
		p.baseURL + "/dereferrer/series/" + show.Ref.ID,
		p.baseURL + "/series/" + url.PathEscape(show.Ref.Slug),
		show.SourceURL,
	} {
		key := "show|" + target + "|" + p.language
		if p.seriesPages[key] == nil {
			p.seriesPages[key] = page
		}
	}
}

func parseSeasonRuntime(doc *html.Node, sourceURL, slug string, season int) (map[string]seasonEpisode, error) {
	source, err := url.Parse(sourceURL)
	if err != nil || source.Path != fmt.Sprintf("/series/%s/seasons/official/%d", slug, season) {
		return nil, fmt.Errorf("TheTVDB 季页面地址不匹配")
	}
	area := byID(doc, "episodes")
	tables := nodes(area, func(n *html.Node) bool { return n.Data == "table" })
	if len(tables) != 1 {
		return nil, fmt.Errorf("TheTVDB 季页面缺少单集表格")
	}
	bodies := nodes(tables[0], func(n *html.Node) bool { return n.Data == "tbody" })
	if len(bodies) != 1 {
		return nil, fmt.Errorf("TheTVDB 季页面缺少单集表体")
	}
	result := make(map[string]seasonEpisode)
	coords := make(map[[2]int]bool)
	for _, tr := range nodes(bodies[0], func(n *html.Node) bool { return n.Data == "tr" }) {
		cells := nodes(tr, func(n *html.Node) bool { return n.Data == "td" })
		if len(cells) != 5 {
			return nil, fmt.Errorf("TheTVDB 季表列数无效")
		}
		parts := regexp.MustCompile(`^S([0-9]+)E([0-9]+)$`).FindStringSubmatch(textContent(cells[0]))
		links := nodes(cells[1], func(n *html.Node) bool { return n.Data == "a" })
		if len(parts) != 3 || len(links) != 1 {
			return nil, fmt.Errorf("TheTVDB 季表单集坐标或链接缺失")
		}
		s, es := strconv.Atoi(parts[1])
		e, ee := strconv.Atoi(parts[2])
		link, el := source.Parse(attr(links[0], "href"))
		prefix := "/series/" + slug + "/episodes/"
		if es != nil || ee != nil || el != nil || s != season || e <= 0 || link.Scheme != source.Scheme || link.Host != source.Host || !strings.HasPrefix(link.Path, prefix) {
			return nil, fmt.Errorf("TheTVDB 季表单集身份无效")
		}
		id := strings.TrimPrefix(link.Path, prefix)
		if !positiveID(id) || coords[[2]int{s, e}] {
			return nil, fmt.Errorf("TheTVDB 季表单集身份重复或无效")
		}
		coords[[2]int{s, e}] = true
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("TheTVDB 季表单集编号重复")
		}
		runtimeText := textContent(cells[3])
		runtime := 0
		if runtimeText != "" {
			runtime, err = strconv.Atoi(runtimeText)
			if err != nil || runtime < 0 {
				return nil, fmt.Errorf("TheTVDB 季表时长无效")
			}
		}
		result[id] = seasonEpisode{season: s, number: e, runtime: runtime}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("TheTVDB 季表没有可验证的单集")
	}
	return result, nil
}

func mergeEpisodeDetail(batch, detail *metadata.Record) {
	if detail.Title != "" && detail.Title != detail.OriginalTitle {
		batch.Title = detail.Title
	}
	if detail.Plot != "" && detail.Plot != batch.Plot {
		batch.Plot = detail.Plot
	}
	if len(detail.ExternalIDs) > 1 {
		batch.ExternalIDs = append([]metadata.Identifier(nil), detail.ExternalIDs...)
	}
}

func copyRecord(record *metadata.Record) *metadata.Record {
	copy := *record
	copy.ExternalIDs = append([]metadata.Identifier(nil), record.ExternalIDs...)
	copy.Genres = append([]string(nil), record.Genres...)
	copy.Studios = append([]string(nil), record.Studios...)
	copy.Countries = append([]string(nil), record.Countries...)
	copy.Languages = append([]string(nil), record.Languages...)
	copy.Actors = append([]metadata.Actor(nil), record.Actors...)
	copy.Directors = append([]string(nil), record.Directors...)
	copy.Credits = append([]string(nil), record.Credits...)
	copy.Ratings = append([]metadata.Rating(nil), record.Ratings...)
	copy.Artwork = append([]metadata.Artwork(nil), record.Artwork...)
	copy.Seasons = append([]metadata.Season(nil), record.Seasons...)
	return &copy
}
