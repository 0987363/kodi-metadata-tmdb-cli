package config

func applyDefaults(c *Config) {
	if c.Log == nil {
		c.Log = &LogConfig{Mode: LogModeStdout, Level: LogLevelInfo}
	}
	if c.Collector == nil {
		c.Collector = &CollectorConfig{}
	}
	if c.Tmdb == nil {
		c.Tmdb = &TmdbConfig{}
	}
	if c.Tmdb.ApiHost == "" {
		c.Tmdb.ApiHost = "https://api.themoviedb.org"
	}
	if c.Tmdb.ImageHost == "" {
		c.Tmdb.ImageHost = "https://image.tmdb.org"
	}
	if c.Tmdb.Language == "" {
		c.Tmdb.Language = "zh-CN"
	}
	if c.Scraper != nil && c.Scraper.JevMatchThreshold == 0 {
		c.Scraper.JevMatchThreshold = 0.8
	}
	if c.TheTVDB == nil {
		c.TheTVDB = &TheTVDBConfig{}
	}
	if c.TheTVDB.BaseURL == "" {
		c.TheTVDB.BaseURL = "https://thetvdb.com"
	}
	if c.TheTVDB.Language == "" {
		c.TheTVDB.Language = "zho"
	}
	if c.TheTVDB.TimeoutSeconds <= 0 {
		c.TheTVDB.TimeoutSeconds = 30
	}
	if c.TheTVDB.RequestIntervalMS <= 0 {
		c.TheTVDB.RequestIntervalMS = 1000
	}
}
