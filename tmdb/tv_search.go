package tmdb

type SearchTvResponse struct {
	Page         int              `json:"page"`
	TotalResults int              `json:"total_results"`
	TotalPages   int              `json:"total_pages"`
	Results      []*SearchResults `json:"results"`
}

type SearchResults struct {
	Id               int      `json:"id"`
	PosterPath       string   `json:"poster_path"`
	Popularity       float32  `json:"popularity"`
	BackdropPath     string   `json:"backdrop_path"`
	VoteAverage      float32  `json:"vote_average"`
	Overview         string   `json:"overview"`
	FirstAirDate     string   `json:"first_air_date"`
	OriginCountry    []string `json:"origin_country"`
	GenreIds         []int    `json:"genre_ids"`
	OriginalLanguage string   `json:"original_language"`
	VoteCount        int      `json:"vote_count"`
	Name             string   `json:"name"`
	OriginalName     string   `json:"original_name"`
}
