package nfo

import (
	"encoding/xml"
	"errors"
	"fmt"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/utils"
)

type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}
type actor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role,omitempty"`
	Order int    `xml:"order"`
	Thumb string `xml:"thumb,omitempty"`
}
type rating struct {
	Name  string  `xml:"name,attr"`
	Max   int     `xml:"max,attr"`
	Value float32 `xml:"value"`
	Votes int     `xml:"votes"`
}
type season struct {
	Number int    `xml:"number,attr"`
	Title  string `xml:",chardata"`
}
type thumb struct {
	Aspect string `xml:"aspect,attr,omitempty"`
	URL    string `xml:",chardata"`
}
type Document struct {
	XMLName       xml.Name
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	ShowTitle     string     `xml:"showtitle,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	UniqueIDs     []UniqueID `xml:"uniqueid"`
	Premiered     string     `xml:"premiered,omitempty"`
	Aired         string     `xml:"aired,omitempty"`
	Status        string     `xml:"status,omitempty"`
	MPAA          string     `xml:"mpaa,omitempty"`
	Genres        []string   `xml:"genre,omitempty"`
	Tags          []string   `xml:"tag,omitempty"`
	Studios       []string   `xml:"studio,omitempty"`
	Countries     []string   `xml:"country,omitempty"`
	Actors        []actor    `xml:"actor,omitempty"`
	Directors     []string   `xml:"director,omitempty"`
	Credits       []string   `xml:"credits,omitempty"`
	Ratings       []rating   `xml:"ratings>rating,omitempty"`
	Seasons       []season   `xml:"namedseason,omitempty"`
	Season        *int       `xml:"season,omitempty"`
	Episode       *int       `xml:"episode,omitempty"`
	Runtime       int        `xml:"runtime,omitempty"`
	Thumbs        []thumb    `xml:"thumb,omitempty"`
	Fanart        []thumb    `xml:"fanart>thumb,omitempty"`
}

func Write(file string, r *metadata.Record, showTitle string, tagEnabled, genreEnabled bool) error {
	if r == nil || r.Title == "" || r.Ref.ID == "" {
		return errors.New("NFO 缺少作品标题或来源编号")
	}
	root := ""
	switch r.Ref.Kind {
	case metadata.Movie:
		root = "movie"
	case metadata.Show:
		root = "tvshow"
	case metadata.Episode:
		root = "episodedetails"
	default:
		return errors.New("NFO 对象类型无效")
	}
	sourceType := r.Ref.Provider
	if sourceType == "thetvdb" {
		sourceType = "tvdb"
	}
	if sourceType == "" {
		return errors.New("NFO 缺少来源")
	}
	ids := []UniqueID{{Type: sourceType, Default: true, Value: r.Ref.ID}}
	known := map[string]string{sourceType: r.Ref.ID}
	for _, id := range r.ExternalIDs {
		if id.Type == "" || id.Value == "" {
			continue
		}
		if value, ok := known[id.Type]; ok {
			if value != id.Value {
				return fmt.Errorf("%s 编号冲突", id.Type)
			}
			continue
		}
		ids = append(ids, UniqueID{Type: id.Type, Value: id.Value})
		known[id.Type] = id.Value
	}
	out := Document{XMLName: xml.Name{Local: root}, Title: r.Title, OriginalTitle: r.OriginalTitle, ShowTitle: showTitle, Plot: r.Plot, UniqueIDs: ids, Premiered: r.Premiered, Status: r.Status, MPAA: r.Certification, Studios: r.Studios, Countries: r.Countries, Directors: r.Directors, Credits: r.Credits}
	if tagEnabled {
		out.Tags = r.Genres
	}
	if genreEnabled {
		out.Genres = r.Genres
	}
	for _, a := range r.Actors {
		out.Actors = append(out.Actors, actor{Name: a.Name, Role: a.Role, Order: a.Order, Thumb: a.Thumb})
	}
	for _, v := range r.Ratings {
		out.Ratings = append(out.Ratings, rating{Name: v.Source, Max: v.Max, Value: v.Value, Votes: v.Votes})
	}
	for _, v := range r.Seasons {
		out.Seasons = append(out.Seasons, season{Number: v.Number, Title: v.Title})
	}
	if r.Ref.Kind == metadata.Episode {
		out.Season = &r.SeasonNumber
		out.Episode = &r.EpisodeNumber
		out.Aired = r.Premiered
	}
	if (r.Ref.Kind == metadata.Movie || r.Ref.Kind == metadata.Episode) && r.RuntimeMinutes > 0 {
		out.Runtime = r.RuntimeMinutes
	}
	for _, a := range r.Artwork {
		switch a.Kind {
		case "fanart":
			out.Fanart = append(out.Fanart, thumb{URL: a.URL})
		case "poster", "thumb", "clearlogo":
			out.Thumbs = append(out.Thumbs, thumb{Aspect: a.Kind, URL: a.URL})
		}
	}
	return utils.SaveNfo(file, out)
}
