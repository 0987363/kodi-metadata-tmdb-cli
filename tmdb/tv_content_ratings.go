package tmdb

// TV 内容分级

type TvContentRatings struct {
	Id      int                      `json:"id"`
	Results []TvContentRatingsResult `json:"results"`
}

type TvContentRatingsResult struct {
	ISO31661 string `json:"iso_3166_1"`
	Rating   string `json:"rating"`
}
