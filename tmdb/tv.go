package tmdb

import (
	"encoding/json"
)

type TvDetail struct {
	ExternalIDs         map[string]json.RawMessage `json:"external_ids"`
	Id                  int                        `json:"id"`
	Name                string                     `json:"name"`
	BackdropPath        string                     `json:"backdrop_path"`
	CreatedBy           []CreatedBy                `json:"created_by"`
	EpisodeRunTime      []int                      `json:"episode_run_time"`
	FirstAirDate        string                     `json:"first_air_date"`
	LastAirDate         string                     `json:"last_air_date"`
	Genres              []Genre                    `json:"genres"`
	Homepage            string                     `json:"homepage"`
	InProduction        bool                       `json:"in_production"`
	Languages           []string                   `json:"languages"`
	LastEpisodeToAir    LastEpisodeToAir           `json:"last_episode_to_air"`
	NextEpisodeToAir    NextEpisodeToAir           `json:"next_episode_to_air"`
	Networks            []Network                  `json:"networks"`
	NumberOfEpisodes    int                        `json:"number_of_episodes"`
	NumberOfSeasons     int                        `json:"number_of_seasons"`
	OriginCountry       []string                   `json:"origin_country"`
	OriginalLanguage    string                     `json:"original_language"`
	OriginalName        string                     `json:"original_name"`
	Overview            string                     `json:"overview"`
	Popularity          float32                    `json:"popularity"`
	PosterPath          string                     `json:"poster_path"`
	ProductionCompanies []ProductionCompany        `json:"production_companies"`
	ProductionCountries []ProductionCountry        `json:"production_countries"`
	Seasons             []Season                   `json:"seasons"`
	SpokenLanguages     []SpokenLanguage           `json:"spoken_languages"`
	Status              string                     `json:"status"`
	Tagline             string                     `json:"tagline"`
	Type                string                     `json:"type"`
	VoteAverage         float32                    `json:"vote_average"`
	VoteCount           int                        `json:"vote_count"`
	AggregateCredits    *TvAggregateCredits        `json:"aggregate_credits"`
	ContentRatings      *TvContentRatings          `json:"content_ratings"`
	Images              *TvImages                  `json:"images"`
}

type TvImage struct {
	AspectRatio float32 `json:"aspect_ratio"`
	Height      int     `json:"height"`
	Iso6391     string  `json:"iso_639_1"`
	FilePath    string  `json:"file_path"`
	VoteAverage float32 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
	Width       int     `json:"width"`
}

type TvImages struct {
	Id        int        `json:"id"`
	Logos     []*TvImage `json:"logos"`
	Posters   []*TvImage `json:"posters"`
	Backdrops []*TvImage `json:"backdrops"`
}

type Genre struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type Network struct {
	Id            int    `json:"id"`
	Name          string `json:"name"`
	LogoPath      string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
}

type CreatedBy struct {
	Id          int    `json:"id"`
	CreditId    string `json:"credit_id"`
	Name        string `json:"name"`
	Gender      int    `json:"gender"`
	ProfilePath string `json:"profile_path"`
}

type LastEpisodeToAir struct {
	Id             int     `json:"id"`
	AirDate        string  `json:"air_date"`
	EpisodeNumber  int     `json:"episode_number"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	ProductionCode string  `json:"production_code"`
	SeasonNumber   int     `json:"season_number"`
	StillPath      string  `json:"still_path"`
	VoteAverage    float32 `json:"vote_average"`
	VoteCount      int     `json:"vote_count"`
}

type NextEpisodeToAir struct {
	Id             int     `json:"id"`
	AirDate        string  `json:"air_date"`
	EpisodeNumber  int     `json:"episode_number"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	ProductionCode string  `json:"production_code"`
	SeasonNumber   int     `json:"season_number"`
	StillPath      string  `json:"still_path"`
	VoteAverage    float32 `json:"vote_average"`
	VoteCount      int     `json:"vote_count"`
}

type ProductionCompany struct {
	Id            int    `json:"id"`
	LogoPath      string `json:"logo_path"`
	Name          string `json:"name"`
	OriginCountry string `json:"origin_country"`
}

type ProductionCountry struct {
	Iso31661 string `json:"iso_3166_1"`
	Name     string `json:"name"`
}

type Season struct {
	Id           int    `json:"id"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	SeasonNumber int    `json:"season_number"`
}

type SpokenLanguage struct {
	EnglishName string `json:"english_name"`
	Iso6391     string `json:"iso_639_1"`
	Name        string `json:"name"`
}
