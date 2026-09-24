package tmdb

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func parseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year := 0
	for i := range 4 {
		c := date[i]
		if c < '0' || c > '9' {
			return 0
		}
		year = year*10 + int(c-'0')
	}
	return year
}

func recordIdentity(kind metadata.Kind, id int) (metadata.Ref, []metadata.Identifier) {
	value := strconv.Itoa(id)
	return metadata.Ref{Provider: "tmdb", Kind: kind, ID: value}, []metadata.Identifier{{Type: "tmdb", Value: value}}
}

func externalIdentifiers(identifiers []metadata.Identifier, values map[string]json.RawMessage) []metadata.Identifier {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "id" {
			continue
		}
		var value string
		if err := json.Unmarshal(values[key], &value); err != nil {
			var number json.Number
			if err := json.Unmarshal(values[key], &number); err != nil {
				continue
			}
			value = number.String()
		}
		if value == "" || value == "0" {
			continue
		}
		identifiers = append(identifiers, metadata.Identifier{Type: strings.TrimSuffix(key, "_id"), Value: value})
	}
	return identifiers
}

func uniqueStrings(values ...string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, value := range values {
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}

func (p *metadataProvider) appendArtwork(record *metadata.Record, kind, path string, season int) {
	if path == "" {
		return
	}
	artwork := metadata.Artwork{Kind: kind, URL: p.imageURL(path), Season: season}
	for _, existing := range record.Artwork {
		if existing == artwork {
			return
		}
	}
	record.Artwork = append(record.Artwork, artwork)
}

func (p *metadataProvider) movieRecord(movie *MovieDetail) *metadata.Record {
	ref, ids := recordIdentity(metadata.Movie, movie.Id)
	if movie.ImdbId != "" {
		ids = append(ids, metadata.Identifier{Type: "imdb", Value: movie.ImdbId})
	}
	record := &metadata.Record{
		Ref: ref, ExternalIDs: ids, SourceURL: fmt.Sprintf("https://www.themoviedb.org/movie/%d", movie.Id),
		Title: movie.Title, OriginalTitle: movie.OriginalTitle, Plot: movie.Overview, Premiered: movie.ReleaseDate, Status: movie.Status,
		RuntimeMinutes: movie.Runtime, Ratings: []metadata.Rating{{Source: "tmdb", Value: movie.VoteAverage, Votes: movie.VoteCount, Max: 10}},
	}
	for _, genre := range movie.Genres {
		record.Genres = append(record.Genres, genre.Name)
	}
	for _, company := range movie.ProductionCompanies {
		record.Studios = append(record.Studios, company.Name)
	}
	for _, country := range movie.ProductionCountries {
		record.Countries = append(record.Countries, country.Name)
	}
	record.Languages = append(record.Languages, movie.OriginalLanguage)
	for _, language := range movie.SpokenLanguages {
		record.Languages = append(record.Languages, language.Iso6391)
	}
	record.Languages = uniqueStrings(record.Languages...)
	for _, country := range movie.ReleaseDates.Results {
		if country.ISO31661 != p.config.Rating {
			continue
		}
		for _, release := range country.ReleaseDates {
			if release.Certification != "" {
				record.Certification = release.Certification
				break
			}
		}
		if record.Certification != "" {
			break
		}
	}
	p.appendCredits(record, movie.Credits)
	p.appendArtwork(record, "poster", movie.PosterPath, 0)
	p.appendArtwork(record, "fanart", movie.BackdropPath, 0)
	if movie.Images != nil {
		for _, image := range movie.Images.Posters {
			if image != nil {
				p.appendArtwork(record, "poster", image.FilePath, 0)
			}
		}
		for _, image := range movie.Images.Backdrops {
			if image != nil {
				p.appendArtwork(record, "fanart", image.FilePath, 0)
			}
		}
		for _, image := range movie.Images.Logos {
			if image != nil {
				p.appendArtwork(record, "clearlogo", image.FilePath, 0)
			}
		}
	}
	return record
}

func (p *metadataProvider) showRecord(show *TvDetail) *metadata.Record {
	ref, ids := recordIdentity(metadata.Show, show.Id)
	record := &metadata.Record{
		Ref: ref, ExternalIDs: externalIdentifiers(ids, show.ExternalIDs), SourceURL: fmt.Sprintf("https://www.themoviedb.org/tv/%d", show.Id),
		Title: show.Name, OriginalTitle: show.OriginalName, Plot: show.Overview, Premiered: show.FirstAirDate, LastAired: show.LastAirDate, Status: show.Status,
		SeasonCount: show.NumberOfSeasons, EpisodeCount: show.NumberOfEpisodes,
		Ratings: []metadata.Rating{{Source: "tmdb", Value: show.VoteAverage, Votes: show.VoteCount, Max: 10}},
	}
	for _, genre := range show.Genres {
		record.Genres = append(record.Genres, genre.Name)
	}
	for _, network := range show.Networks {
		record.Studios = append(record.Studios, network.Name)
	}
	for _, company := range show.ProductionCompanies {
		record.Studios = append(record.Studios, company.Name)
	}
	record.Studios = uniqueStrings(record.Studios...)
	for _, country := range show.ProductionCountries {
		record.Countries = append(record.Countries, country.Name)
	}
	if len(record.Countries) == 0 {
		record.Countries = append(record.Countries, show.OriginCountry...)
	}
	record.Languages = append(record.Languages, show.OriginalLanguage)
	record.Languages = append(record.Languages, show.Languages...)
	for _, language := range show.SpokenLanguages {
		record.Languages = append(record.Languages, language.Iso6391)
	}
	record.Languages = uniqueStrings(record.Languages...)
	if len(show.EpisodeRunTime) > 0 {
		record.RuntimeMinutes = show.EpisodeRunTime[0]
	}
	if show.ContentRatings != nil {
		for _, rating := range show.ContentRatings.Results {
			if rating.ISO31661 == p.config.Rating && rating.Rating != "" {
				record.Certification = rating.Rating
				break
			}
		}
	}
	if show.AggregateCredits != nil {
		for _, actor := range show.AggregateCredits.Cast {
			var roles []string
			for _, role := range actor.Roles {
				roles = append(roles, role.Character)
			}
			record.Actors = append(record.Actors, metadata.Actor{Name: actor.Name, Role: strings.Join(uniqueStrings(roles...), " / "), Order: actor.Order, Thumb: p.imageURL(actor.ProfilePath)})
		}
		for _, crew := range show.AggregateCredits.Crew {
			for _, job := range crew.Jobs {
				appendCrew(record, crew.Name, crew.Department, job.Job)
			}
		}
	}
	p.appendArtwork(record, "poster", show.PosterPath, 0)
	p.appendArtwork(record, "fanart", show.BackdropPath, 0)
	if show.Images != nil {
		for _, image := range show.Images.Posters {
			if image != nil {
				p.appendArtwork(record, "poster", image.FilePath, 0)
			}
		}
		for _, image := range show.Images.Backdrops {
			if image != nil {
				p.appendArtwork(record, "fanart", image.FilePath, 0)
			}
		}
		for _, image := range show.Images.Logos {
			if image != nil {
				p.appendArtwork(record, "clearlogo", image.FilePath, 0)
			}
		}
	}
	for _, season := range show.Seasons {
		record.Seasons = append(record.Seasons, metadata.Season{Number: season.SeasonNumber, Title: season.Name})
		p.appendArtwork(record, "season_poster", season.PosterPath, season.SeasonNumber)
	}
	return record
}

func (p *metadataProvider) episodeRecord(episode *TvEpisodeDetail, showID int) *metadata.Record {
	ref, ids := recordIdentity(metadata.Episode, episode.Id)
	record := &metadata.Record{
		Ref: ref, ExternalIDs: externalIdentifiers(ids, episode.ExternalIDs), SourceURL: fmt.Sprintf("https://www.themoviedb.org/tv/%d/season/%d/episode/%d", showID, episode.SeasonNumber, episode.EpisodeNumber),
		Title: episode.Name, Plot: episode.Overview, Premiered: episode.AirDate, SeasonNumber: episode.SeasonNumber, EpisodeNumber: episode.EpisodeNumber, RuntimeMinutes: episode.Runtime,
		Ratings: []metadata.Rating{{Source: "tmdb", Value: episode.VoteAverage, Votes: episode.VoteCount, Max: 10}},
	}
	p.appendCredits(record, episode.Credits)
	for _, actor := range episode.GuestStars {
		value := metadata.Actor{Name: actor.Name, Role: actor.Character, Order: actor.Order, Thumb: p.imageURL(actor.ProfilePath)}
		duplicate := false
		for _, existing := range record.Actors {
			if existing.Name == value.Name && existing.Role == value.Role {
				duplicate = true
				break
			}
		}
		if !duplicate {
			record.Actors = append(record.Actors, value)
		}
	}
	for _, crew := range episode.Crew {
		appendCrew(record, crew.Name, crew.Department, crew.Job)
	}
	p.appendArtwork(record, "thumb", episode.StillPath, 0)
	if episode.Images != nil {
		for _, image := range episode.Images.Stills {
			if image != nil {
				p.appendArtwork(record, "thumb", image.FilePath, 0)
			}
		}
	}
	return record
}

func (p *metadataProvider) appendCredits(record *metadata.Record, credits *Credit) {
	if credits == nil {
		return
	}
	for _, actor := range credits.Cast {
		record.Actors = append(record.Actors, metadata.Actor{Name: actor.Name, Role: actor.Character, Order: actor.Order, Thumb: p.imageURL(actor.ProfilePath)})
	}
	for _, crew := range credits.Crew {
		appendCrew(record, crew.Name, crew.Department, crew.Job)
	}
}

func appendCrew(record *metadata.Record, name, department, job string) {
	if job == "Director" {
		record.Directors = uniqueStrings(append(record.Directors, name)...)
	}
	if department == "Writing" || job == "Writer" || job == "Screenplay" || job == "Story" {
		record.Credits = uniqueStrings(append(record.Credits, name)...)
	}
}
