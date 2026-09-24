package thetvdb

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func parseShow(doc *html.Node, sourceURL, language, expectedID string) (*metadata.Record, error) {
	source, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}
	path := strings.Split(strings.Trim(source.Path, "/"), "/")
	info := byID(doc, "series_basic_info")
	identity := field(info, "TheTVDB.com Series ID")
	heading := byID(doc, "series_title")
	if len(path) != 2 || path[0] != "series" || heading == nil || !positiveID(identity) || (expectedID != "" && expectedID != identity) {
		return nil, fmt.Errorf("TheTVDB 节目页面结构或编号不匹配")
	}
	title, plot := translated(doc, language)
	original := textContent(heading)
	if title == "" {
		title = original
	}
	if title == "" {
		return nil, fmt.Errorf("TheTVDB 节目页面缺少标题")
	}
	record := &metadata.Record{
		Ref:         metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: identity, Slug: path[1]},
		ExternalIDs: []metadata.Identifier{{Type: "tvdb", Value: identity}},
		SourceURL:   strings.TrimRight(sourceURL, "/"), Title: title, OriginalTitle: original, Plot: plot,
		Premiered: date(field(info, "First Aired")), LastAired: date(field(info, "Recent")), Status: field(info, "Status"),
		Genres: fieldValues(info, "Genres"), Studios: fieldValues(info, "Network"), Countries: fieldValues(info, "Original Country"), Languages: fieldValues(info, "Original Language"),
		RuntimeMinutes: leadingInt(field(info, "Average Runtime")), Certification: field(info, "Content Rating"),
	}
	record.ExternalIDs = append(record.ExternalIDs, externalIDs(info, metadata.Show)...)
	for _, kind := range []struct{ id, name string }{{"artwork-backgrounds", "fanart"}, {"artwork-posters", "poster"}, {"artwork-clearlogo", "clearlogo"}} {
		for _, a := range nodes(byID(doc, kind.id), func(n *html.Node) bool { return n.Data == "a" && hasClass(n, "lightbox") }) {
			if imageURL := artworkURL(attr(a, "href")); imageURL != "" {
				record.Artwork = append(record.Artwork, metadata.Artwork{Kind: kind.name, URL: imageURL})
			}
		}
	}
	for _, row := range nodes(byID(doc, "seasons-official"), func(n *html.Node) bool { return n.Data == "tr" }) {
		cells := nodes(row, func(n *html.Node) bool { return n.Data == "td" })
		if len(cells) < 4 {
			continue
		}
		links := nodes(cells[0], func(n *html.Node) bool { return n.Data == "a" })
		if len(links) == 0 {
			continue
		}
		link, err := source.Parse(attr(links[0], "href"))
		if err != nil || (link.Host != source.Host && !tvdbHost(link.Hostname())) {
			continue
		}
		prefix := "/series/" + path[1] + "/seasons/official/"
		if !strings.HasPrefix(link.Path, prefix) {
			continue
		}
		number, err := strconv.Atoi(strings.TrimPrefix(link.Path, prefix))
		if err != nil || number < 0 {
			continue
		}
		record.Seasons = append(record.Seasons, metadata.Season{Number: number, Title: textContent(links[0])})
		if number > 0 {
			record.SeasonCount++
		}
		record.EpisodeCount += leadingInt(textContent(cells[3]))
	}
	sort.Slice(record.Seasons, func(i, j int) bool { return record.Seasons[i].Number < record.Seasons[j].Number })
	return record, nil
}

func parseAllSeasons(doc *html.Node, sourceURL, slug string) ([]episodeEntry, error) {
	source, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}
	headings := nodes(doc, func(n *html.Node) bool { return n.Data == "h1" && textContent(n) == "All Seasons" })
	if source.Path != "/series/"+slug+"/allseasons/official" || len(headings) != 1 {
		return nil, fmt.Errorf("TheTVDB 全集页面结构或地址不匹配")
	}
	var entries []episodeEntry
	seen := make(map[string]bool)
	for _, li := range nodes(doc, func(n *html.Node) bool { return n.Data == "li" && hasClass(n, "list-group-item") }) {
		labels := nodes(li, func(n *html.Node) bool { return hasClass(n, "episode-label") })
		if len(labels) == 0 {
			continue
		}
		parts := regexp.MustCompile(`^S([0-9]+)E([0-9]+)$`).FindStringSubmatch(textContent(labels[0]))
		links := nodes(li, func(n *html.Node) bool { return n.Data == "a" && n.Parent != nil && n.Parent.Data == "h4" })
		if len(parts) != 3 || len(links) != 1 {
			return nil, fmt.Errorf("TheTVDB 全集条目缺少正式季集编号或链接")
		}
		link, err := source.Parse(attr(links[0], "href"))
		if err != nil || link.Scheme != source.Scheme || link.Host != source.Host {
			return nil, fmt.Errorf("TheTVDB 单集链接离开节目站点")
		}
		prefix := "/series/" + slug + "/episodes/"
		identity := strings.TrimPrefix(link.Path, prefix)
		if !strings.HasPrefix(link.Path, prefix) || !positiveID(identity) {
			return nil, fmt.Errorf("TheTVDB 单集链接不属于当前节目")
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		season, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("TheTVDB 季编号: %w", err)
		}
		number, err := strconv.Atoi(parts[2])
		if err != nil || number <= 0 {
			return nil, fmt.Errorf("TheTVDB 集编号无效")
		}
		entry := episodeEntry{id: identity, season: season, number: number, title: textContent(links[0]), url: link.String()}
		for _, list := range nodes(li, func(n *html.Node) bool { return hasClass(n, "list-inline") }) {
			for _, item := range nodes(list, func(n *html.Node) bool { return n.Data == "li" }) {
				if parsed := date(textContent(item)); parsed != "" {
					entry.aired = parsed
					break
				}
			}
		}
		for _, overview := range nodes(li, func(n *html.Node) bool { return hasClass(n, "list-group-item-text") }) {
			paragraphs := nodes(overview, func(n *html.Node) bool { return n.Data == "p" })
			if len(paragraphs) > 0 {
				entry.plot = textContent(paragraphs[0])
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("TheTVDB 全集页面缺少可验证的单集条目")
	}
	return entries, nil
}

func parseEpisode(doc *html.Node, sourceURL, language string, entry episodeEntry) (*metadata.Record, error) {
	source, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}
	expected, err := url.Parse(entry.url)
	if err != nil {
		return nil, err
	}
	identities := nodes(doc, func(n *html.Node) bool { return attr(n, "data-type") == "episode" && attr(n, "data-id") == entry.id })
	headings := nodes(doc, func(n *html.Node) bool { return n.Data == "h1" && hasClass(n, "translated_title") })
	if source.Path != expected.Path || len(identities) == 0 || len(headings) != 1 {
		return nil, fmt.Errorf("TheTVDB 单集页面结构或编号不匹配")
	}
	title, plot := translated(doc, language)
	if title == "" {
		title = textContent(headings[0])
	}
	if title == "" {
		return nil, fmt.Errorf("TheTVDB 单集页面缺少标题")
	}
	info := byID(doc, "general")
	aired := date(field(info, "First Aired"))
	if aired == "" {
		aired = entry.aired
	}
	record := &metadata.Record{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Episode, ID: entry.id}, ExternalIDs: []metadata.Identifier{{Type: "tvdb", Value: entry.id}}, SourceURL: sourceURL, Title: title, OriginalTitle: textContent(headings[0]), Plot: plot, Premiered: aired, SeasonNumber: entry.season, EpisodeNumber: entry.number, RuntimeMinutes: leadingInt(field(info, "Runtime")), Studios: fieldValues(info, "Network")}
	record.ExternalIDs = append(record.ExternalIDs, externalIDs(info, metadata.Episode)...)
	for _, row := range nodes(doc, func(n *html.Node) bool { return hasClass(n, "row") && hasClass(n, "mt-2") }) {
		for _, img := range nodes(row, func(n *html.Node) bool { return n.Data == "img" }) {
			if imageURL := artworkURL(attr(img, "src")); imageURL != "" {
				record.Artwork = append(record.Artwork, metadata.Artwork{Kind: "thumb", URL: imageURL})
				return record, nil
			}
		}
	}
	return record, nil
}

func translated(doc *html.Node, language string) (string, string) {
	for _, translation := range nodes(byID(doc, "translations"), func(n *html.Node) bool { return hasClass(n, "change_translation_text") }) {
		if attr(translation, "data-language") == languageCode(language) {
			return attr(translation, "data-title"), textContent(translation)
		}
	}
	return "", ""
}

func languageCode(language string) string {
	switch strings.ToLower(strings.ReplaceAll(language, "_", "-")) {
	case "zh", "zh-cn", "zh-hans", "zho":
		return "zho"
	case "zh-tw", "zh-hk", "zh-hant":
		return "zhtw"
	case "en", "en-us", "en-gb", "eng":
		return "eng"
	case "ja", "ja-jp", "jpn":
		return "jpn"
	case "ko", "ko-kr", "kor":
		return "kor"
	case "fr", "fr-fr", "fra":
		return "fra"
	case "de", "de-de", "deu":
		return "deu"
	case "es", "es-es", "spa":
		return "spa"
	default:
		return strings.ToLower(language)
	}
}

func externalIDs(info *html.Node, kind metadata.Kind) []metadata.Identifier {
	var result []metadata.Identifier
	for _, item := range nodes(info, func(n *html.Node) bool { return n.Data == "li" }) {
		labels := nodes(item, func(n *html.Node) bool { return n.Data == "strong" })
		if len(labels) != 1 || textContent(labels[0]) != "On Other Sites" {
			continue
		}
		for _, link := range nodes(item, func(n *html.Node) bool { return n.Data == "a" }) {
			parsed, err := url.Parse(attr(link, "href"))
			if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				continue
			}
			host := strings.ToLower(parsed.Hostname())
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if (host == "themoviedb.org" || host == "www.themoviedb.org") && kind == metadata.Show && len(parts) == 2 && parts[0] == "tv" && positiveID(parts[1]) {
				result = append(result, metadata.Identifier{Type: "tmdb", Value: parts[1]})
			}
			if (host == "imdb.com" || host == "www.imdb.com") && len(parts) == 2 && parts[0] == "title" && regexp.MustCompile(`^tt[0-9]+$`).MatchString(parts[1]) {
				result = append(result, metadata.Identifier{Type: "imdb", Value: parts[1]})
			}
		}
	}
	return result
}

func artworkURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Scheme != "https" || !tvdbHost(parsed.Hostname()) || !strings.HasPrefix(parsed.Path, "/banners/") {
		return ""
	}
	return parsed.String()
}

func field(info *html.Node, label string) string { return strings.Join(fieldValues(info, label), " ") }

func fieldValues(info *html.Node, label string) []string {
	for _, li := range nodes(info, func(n *html.Node) bool { return n.Data == "li" }) {
		labels := nodes(li, func(n *html.Node) bool { return n.Data == "strong" })
		if len(labels) != 1 || textContent(labels[0]) != label {
			continue
		}
		var values []string
		for child := li.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "span" {
				if value := textContent(child); value != "" {
					values = append(values, value)
				}
			}
		}
		return values
	}
	return nil
}

func date(value string) string {
	for _, layout := range []string{"January 2, 2006", "Jan 2, 2006", "2006-01-02"} {
		parsed, err := time.Parse(layout, strings.TrimSpace(value))
		if err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func leadingInt(value string) int {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	number, err := strconv.Atoi(fields[0])
	if err != nil || number < 0 {
		return 0
	}
	return number
}

func attr(node *html.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func byID(root *html.Node, identity string) *html.Node {
	matches := nodes(root, func(n *html.Node) bool { return attr(n, "id") == identity })
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func nodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var result []*html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode && match(node) {
			result = append(result, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return result
}

func textContent(root *html.Node) string {
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
			builder.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return strings.Join(strings.Fields(builder.String()), " ")
}
