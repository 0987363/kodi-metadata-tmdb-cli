package tmdb

type TvEpisodeGroupDetail struct {
	Id           string           `json:"id"`
	Name         string           `json:"name"`
	Type         int              `json:"type"`
	Network      Network          `json:"network"`
	GroupCount   int              `json:"group_count"`
	EpisodeCount int              `json:"episode_count"`
	Description  string           `json:"description"`
	Groups       []TvEpisodeGroup `json:"groups"`
}

type TvEpisodeGroup struct {
	Id       string                  `json:"id"`
	Name     string                  `json:"name"`
	Order    int                     `json:"order"`
	Episodes []TvEpisodeGroupEpisode `json:"episodes"`
	Locked   bool                    `json:"locked"`
}

type TvEpisodeGroupEpisode struct {
	AirDate        string  `json:"air_date"`
	EpisodeNumber  int     `json:"episode_number"`
	Id             int     `json:"id"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	ProductionCode string  `json:"production_code"`
	SeasonNumber   int     `json:"season_number"`
	ShowId         int     `json:"show_id"`
	StillPath      string  `json:"still_path"`
	VoteAverage    float32 `json:"vote_average"`
	VoteCount      int     `json:"vote_count"`
	Order          int     `json:"order"`
}
