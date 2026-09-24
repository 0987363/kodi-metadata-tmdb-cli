package tmdb

import (
	"encoding/json"
)

type TvEpisodeDetail struct {
	Runtime        int                        `json:"runtime"`
	ShowID         int                        `json:"show_id"`
	ExternalIDs    map[string]json.RawMessage `json:"external_ids"`
	Credits        *Credit                    `json:"credits"`
	Images         *EpisodeImages             `json:"images"`
	AirDate        string                     `json:"air_date"`
	Crew           []Crew                     `json:"crew"`
	GuestStars     []GuestStars               `json:"guest_stars"`
	Name           string                     `json:"name"`
	Overview       string                     `json:"overview"`
	Id             int                        `json:"id"`
	ProductionCode string                     `json:"production_code"`
	SeasonNumber   int                        `json:"season_number"`
	EpisodeNumber  int                        `json:"episode_number"`
	StillPath      string                     `json:"still_path"`
	VoteAverage    float32                    `json:"vote_average"`
	VoteCount      int                        `json:"vote_count"`
}

type EpisodeImages struct {
	Stills []*TvImage `json:"stills"`
}

type Crew struct {
	Id          int    `json:"id"`
	CreditId    string `json:"credit_id"`
	Name        string `json:"name"`
	Department  string `json:"department"`
	Job         string `json:"job"`
	ProfilePath string `json:"profile_path"`
}

type GuestStars struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	CreditId    string `json:"credit_id"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}
