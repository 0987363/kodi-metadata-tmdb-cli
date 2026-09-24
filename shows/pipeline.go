package shows

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/media_file"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/nfo"
)

type Entry struct {
	File     *media_file.MediaFile
	Identity ai.Identity
}
type Task struct {
	Request   metadata.Request
	CacheRoot string
	Input     []ai.Identity
	root      string
	entries   []Entry
	keys      []metadata.EpisodeKey
}

func (t *Task) OutputPaths() []string {
	if t == nil {
		return nil
	}
	paths := []string{
		filepath.Join(t.root, "tvshow.nfo"),
		filepath.Join(t.root, "poster.jpg"),
		filepath.Join(t.root, "fanart.jpg"),
		filepath.Join(t.root, "clearlogo.png"),
	}
	seasons := map[int]bool{}
	for _, key := range t.Request.Episodes {
		if !seasons[key.Season] {
			seasons[key.Season] = true
			paths = append(paths, filepath.Join(t.root, fmt.Sprintf("season%02d-poster.jpg", key.Season)))
		}
	}
	for _, entry := range t.entries {
		prefix := strings.TrimSuffix(entry.File.Path, entry.File.Suffix)
		paths = append(paths, prefix+".nfo", prefix+"-thumb.jpg")
	}
	return paths
}

func (t *Task) validateOutputPaths() error {
	seen := map[string]bool{}
	for _, path := range t.OutputPaths() {
		clean := filepath.Clean(path)
		if seen[clean] {
			return fmt.Errorf("节目输出目标路径重复：%s", clean)
		}
		seen[clean] = true
	}
	return nil
}

func Prepare(root string, entries []Entry) (*Task, error) {
	if len(entries) == 0 {
		return nil, errors.New("节目没有输入文件")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	task := &Task{root: absoluteRoot, CacheRoot: filepath.Join(absoluteRoot, ".metadata"), entries: append([]Entry(nil), entries...)}
	ref, err := metadata.LoadReference(filepath.Join(task.CacheRoot, "source.json"), metadata.Show)
	if err != nil {
		return nil, err
	}
	var first *Show
	var tmdbID, theTVDBID string
	var tmdbCount, theTVDBCount int
	var commonNames map[string]bool
	seenPaths := map[string]bool{}
	for _, entry := range entries {
		mf, id := entry.File, entry.Identity
		if mf == nil || mf.Path == "" || mf.Filename == "" || id.MediaType != "tv" || id.RelativePath == "" || strings.TrimSpace(id.Title) == "" {
			return nil, errors.New("剧集文件或识别身份无效")
		}
		path, err := filepath.Abs(mf.Path)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(absoluteRoot, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || seenPaths[path] {
			return nil, errors.New("剧集文件不在节目目录内或重复")
		}
		if filepath.ToSlash(filepath.Clean(filepath.Join(id.SeriesRoot, relative))) != id.RelativePath {
			return nil, errors.New("剧集文件路径与识别身份不一致")
		}
		seenPaths[path] = true
		s := &Show{MediaFile: mf, TvRoot: absoluteRoot, SeasonRoot: filepath.Dir(path), Title: id.Title, ChsTitle: id.ChsTitle, EngTitle: id.EngTitle, Year: id.Year, TMDBID: id.TMDBID, TheTVDBID: id.TheTVDBID, EpisodeTitle: id.EpisodeTitle}
		if id.Season != nil {
			s.Season = *id.Season
			s.SeasonKnown = true
		}
		if id.Episode != nil {
			s.Episode = *id.Episode
		}
		if err := s.readMetadataOverrides(); err != nil {
			return nil, err
		}
		if !s.SeasonKnown || s.Season < 0 || s.Episode < 1 {
			return nil, errors.New("未识别出有效季集号，且人工设置未补足")
		}
		if id.TMDBID != "" {
			if tmdbID != "" && tmdbID != id.TMDBID {
				return nil, errors.New("同一节目输入的 TMDb 编号冲突")
			}
			tmdbID = id.TMDBID
			tmdbCount++
		}
		if id.TheTVDBID != "" {
			if theTVDBID != "" && theTVDBID != id.TheTVDBID {
				return nil, errors.New("同一节目输入的 TheTVDB 编号冲突")
			}
			theTVDBID = id.TheTVDBID
			theTVDBCount++
		}
		names := identityNames(id)
		if commonNames == nil {
			commonNames = names
		} else {
			for name := range commonNames {
				if !names[name] {
					delete(commonNames, name)
				}
			}
		}
		if first == nil {
			first = s
		} else if first.TvId != s.TvId {
			return nil, errors.New("同一节目输入的身份线索冲突")
		}
		task.Input = append(task.Input, id)
		task.keys = append(task.keys, metadata.EpisodeKey{Season: s.Season, Episode: s.Episode, Group: s.GroupId})
		task.Request.Files = append(task.Request.Files, mf.Path)
	}
	if len(commonNames) == 0 && tmdbCount != len(entries) && theTVDBCount != len(entries) {
		return nil, errors.New("同一节目输入缺少共同标题或明确来源编号")
	}
	if first.TvId > 0 {
		ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: strconv.Itoa(first.TvId)})
		if err != nil {
			return nil, err
		}
	}
	for _, key := range task.keys {
		if key.Group != "" {
			ref, err = metadata.CombineReferences(ref, metadata.Ref{Provider: "tmdb", Kind: metadata.Show})
			if err != nil {
				return nil, err
			}
		}
	}
	task.Request.Kind = metadata.Show
	task.Request.Ref = ref
	task.Request.Path = entries[0].File.Path
	task.Request.Filename = entries[0].File.Filename
	task.Request.Query = metadata.Query{Kind: metadata.Show, Title: first.Title, ChineseTitle: first.ChsTitle, OriginalTitle: first.EngTitle, Year: first.Year}
	task.Request.Episodes = metadata.UniqueEpisodeKeys(task.keys)
	task.Request.Season = task.keys[0].Season
	task.Request.Episode = task.keys[0].Episode
	task.Request.Group = task.keys[0].Group
	task.Request.EpisodeTitle = first.EpisodeTitle
	if tmdbID != "" {
		if ref.Provider == "tmdb" && ref.ID != "" && ref.ID != tmdbID {
			return nil, errors.New("TMDb 识别编号与人工指定冲突")
		}
		task.Request.Hints = append(task.Request.Hints, metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: tmdbID})
	}
	if theTVDBID != "" {
		if ref.Provider == "thetvdb" && ref.ID != "" && ref.ID != theTVDBID {
			return nil, errors.New("TheTVDB 识别编号与人工指定冲突")
		}
		task.Request.Hints = append(task.Request.Hints, metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: theTVDBID})
	}
	return task, nil
}

func identityNames(id ai.Identity) map[string]bool {
	names := map[string]bool{}
	for _, raw := range []string{id.Title, id.AliasTitle, id.ChsTitle, id.EngTitle} {
		name := strings.ToLower(strings.Join(strings.Fields(raw), ""))
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func (t *Task) Local(descriptions []ai.LocalDescription) (*metadata.Option, error) {
	if t == nil || len(descriptions) != len(t.Input) {
		return nil, errors.New("剧集本地描述数量不符")
	}
	byPath := make(map[string]ai.LocalDescription, len(descriptions))
	for _, d := range descriptions {
		if d.RelativePath == "" || strings.TrimSpace(d.Title) == "" || byPath[d.RelativePath].RelativePath != "" {
			return nil, errors.New("剧集本地描述路径或标题无效")
		}
		byPath[d.RelativePath] = d
	}
	first, ok := byPath[t.Input[0].RelativePath]
	if !ok {
		return nil, errors.New("剧集本地描述缺少输入")
	}
	work := &metadata.Record{Ref: metadata.LocalRef(metadata.Show, t.root), Title: filepath.Base(t.root), Plot: "依据目录和文件名整理：" + strings.TrimSpace(first.Plot), Genres: append([]string(nil), first.Genres...)}
	option := &metadata.Option{Work: work}
	seen := map[metadata.EpisodeKey]bool{}
	for i, id := range t.Input {
		d, ok := byPath[id.RelativePath]
		if !ok {
			return nil, errors.New("剧集本地描述缺少输入")
		}
		key := t.keys[i]
		if seen[key] {
			continue
		}
		seen[key] = true
		identity := fmt.Sprintf("%s/S%02dE%02d", t.root, key.Season, key.Episode)
		record := &metadata.Record{Ref: metadata.LocalRef(metadata.Episode, identity), Title: d.Title, Plot: "依据目录和文件名整理：" + strings.TrimSpace(d.Plot), Genres: append([]string(nil), d.Genres...), SeasonNumber: key.Season, EpisodeNumber: key.Episode}
		option.Episodes = append(option.Episodes, metadata.SeriesEpisode{Key: key, Record: record})
	}
	return option, nil
}

func (t *Task) Write(ctx context.Context, selected *metadata.Option, images *artwork.Downloader) error {
	if t == nil || selected == nil || selected.Work == nil {
		return errors.New("节目输出缺少作品事实")
	}
	if err := t.validateOutputPaths(); err != nil {
		return err
	}
	work := selected.Work
	if work.Ref.Kind != metadata.Show || work.Ref.Provider == "" || work.Ref.ID == "" || strings.TrimSpace(work.Title) == "" {
		return errors.New("节目事实身份无效")
	}
	if t.Request.Ref.Provider != "" && (work.Ref.Provider != t.Request.Ref.Provider || t.Request.Ref.ID != "" && work.Ref.ID != t.Request.Ref.ID) {
		return errors.New("节目事实与人工来源指定不一致")
	}
	if work.Ref.Provider == "local" && work.Ref != metadata.LocalRef(metadata.Show, t.root) {
		return errors.New("本地节目身份与目录不一致")
	}
	wanted := make(map[metadata.EpisodeKey]bool, len(t.Request.Episodes))
	for _, key := range t.Request.Episodes {
		wanted[key] = true
	}
	found := make(map[metadata.EpisodeKey]*metadata.Record, len(selected.Episodes))
	for _, item := range selected.Episodes {
		r := item.Record
		if !wanted[item.Key] || found[item.Key] != nil || r == nil || r.Ref.Provider != work.Ref.Provider || r.Ref.Kind != metadata.Episode || r.Ref.ID == "" || strings.TrimSpace(r.Title) == "" || r.SeasonNumber != item.Key.Season || r.EpisodeNumber != item.Key.Episode {
			return errors.New("单集事实身份、坐标或覆盖范围无效")
		}
		if r.Ref.Provider == "local" && r.Ref != metadata.LocalRef(metadata.Episode, fmt.Sprintf("%s/S%02dE%02d", t.root, item.Key.Season, item.Key.Episode)) {
			return errors.New("本地单集身份与季集坐标不一致")
		}
		found[item.Key] = r
	}
	if len(found) != len(wanted) {
		return errors.New("单集事实未覆盖全部输入")
	}
	if config.Scraper == nil {
		return errors.New("刮削配置 scraper 未初始化")
	}
	if err := nfo.Write(filepath.Join(t.root, "tvshow.nfo"), work, "", config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
		return err
	}
	seasonSeen := map[int]bool{}
	var common []artwork.Item
	for _, a := range work.Artwork {
		var file string
		switch a.Kind {
		case "poster":
			file = "poster.jpg"
		case "fanart":
			file = "fanart.jpg"
		case "clearlogo":
			file = "clearlogo.png"
		case "season_poster":
			if wantedSeason(t.Request.Episodes, a.Season) && !seasonSeen[a.Season] {
				file = fmt.Sprintf("season%02d-poster.jpg", a.Season)
				seasonSeen[a.Season] = true
			}
		}
		if file != "" {
			common = append(common, artwork.Item{URL: a.URL, Path: filepath.Join(t.root, file)})
		}
	}
	if err := images.Save(ctx, work.Ref.Provider, common); err != nil {
		return err
	}
	for i, entry := range t.entries {
		key := t.keys[i]
		r := found[key]
		prefix := strings.TrimSuffix(entry.File.Path, entry.File.Suffix)
		if err := nfo.Write(prefix+".nfo", r, work.Title, config.Scraper.NfoField.Tag, config.Scraper.NfoField.Genre); err != nil {
			return err
		}
		var thumbs []artwork.Item
		for _, a := range r.Artwork {
			if a.Kind == "thumb" {
				thumbs = append(thumbs, artwork.Item{URL: a.URL, Path: prefix + "-thumb.jpg"})
			}
		}
		if err := images.Save(ctx, r.Ref.Provider, thumbs); err != nil {
			return err
		}
	}
	return nil
}

func wantedSeason(keys []metadata.EpisodeKey, season int) bool {
	for _, key := range keys {
		if key.Season == season {
			return true
		}
	}
	return false
}
