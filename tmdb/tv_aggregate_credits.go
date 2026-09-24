package tmdb

type TvAggregateCredits struct {
	Id   int      `json:"id,omitempty"`
	Cast []TvCast `json:"cast"`
	Crew []TvCrew `json:"crew"`
}

type TvCast struct {
	Adult              bool    `json:"adult"`
	Gender             int     `json:"gender"`
	Id                 int     `json:"id"`
	KnownForDepartment string  `json:"known_for_department"`
	Name               string  `json:"name"`
	OriginalName       string  `json:"original_name"`
	Popularity         float32 `json:"popularity"`
	ProfilePath        string  `json:"profile_path"`
	Roles              []Role  `json:"roles"`
	TotalEpisodeCount  int     `json:"total_episode_count"`
	Order              int     `json:"order"`
}

type Role struct {
	CreditId     string `json:"credit_id"`
	Character    string `json:"character"`
	EpisodeCount int    `json:"episode_count"`
}

type Job struct {
	CreditId     string `json:"credit_id"`
	Job          string `json:"job"`
	EpisodeCount int    `json:"episode_count"`
}

type TvCrew struct {
	Adult              bool    `json:"adult"`
	Gender             int     `json:"gender"`
	Id                 int     `json:"id"`
	KnownForDepartment string  `json:"known_for_department"`
	Name               string  `json:"name"`
	OriginalName       string  `json:"original_name"`
	Popularity         float32 `json:"popularity"`
	ProfilePath        string  `json:"profile_path"`
	Jobs               []Job   `json:"jobs"`
	Department         string  `json:"department"`
	TotalEpisodeCount  int     `json:"total_episode_count"`
}
