package shows

import (
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Show) readMetadataOverrides() error {
	id, err := metadata.ReadNumericOverride(filepath.Join(s.TvRoot, "tmdb", "id.txt"), false)
	if err != nil {
		return err
	}
	if id != "" {
		s.TvId, _ = strconv.Atoi(id)
	}
	season, err := metadata.ReadNumericOverride(filepath.Join(s.GetCacheDir(), "season.txt"), true)
	if err != nil {
		return err
	}
	if season != "" {
		s.Season, _ = strconv.Atoi(season)
		s.SeasonKnown = true
	}
	if err := s.ReadGroupId(); err != nil {
		return err
	}
	file := filepath.Join(s.GetCacheDir(), "join.txt")
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(string(data)), "，", ","), ",")
	if len(parts) != 3 {
		return fmt.Errorf("%s 必须包含原季、新季、起始集号", file)
	}
	var values [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || v < 0 || i == 2 && v < 1 {
			return fmt.Errorf("%s 包含无效季集号", file)
		}
		values[i] = v
	}
	if !s.SeasonKnown || s.Episode < 1 {
		return errors.New("无法对未知季集号应用 join 映射")
	}
	if s.Season == values[0] {
		s.Season = values[1]
		s.Episode += values[2] - 1
	}
	return nil
}
