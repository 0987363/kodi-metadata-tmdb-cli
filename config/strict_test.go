package config

import (
	"encoding/json"
	"testing"
)

func TestConfigurationRejectsRemovedAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		`{"ai":{}}`, `{"jev":{}}`, `{"metadata":{}}`, `{"kodi":{}}`,
		`{"collector":{"nfo_field":{"tag":true}}}`,
		`{"collector":{"cron_scan_kodi":true}}`,
		`{"collector":{"movies_nfo_mode":1}}`,
		`{"collector":{"filter_tmp_suffix":true}}`,
		`{"collector":{"music_videos_dir":["/music"]}}`,
		`{"unknown":true}`, `{"tmdb":{"unknown":true}}`,
		`{"thetvdb":{"unknown":true}}`, `{"log":{"unknown":true}}`,
		`{"llms":[{"name":"extract","type":"openai","match_mode":1}]}`,
		`{"llms":[{"name":"extract","type":"openai","search_mode":1}]}`,
		`{"llms":[{"name":"extract","type":"openai","enable":true}]}`,
		`{"llms":[{"name":"extract","type":"openai","confidence_threshold":0.8}]}`,
		`{"llms":[{"name":"judge","type":"jev","match_threshold":0.8}]}`,
		`{"scraper":{"judge":"jev"}}`, `{"scraper":{"nfo_field":{"unknown":true}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var parsed Config
			if err := json.Unmarshal([]byte(body), &parsed); err == nil {
				t.Fatal("已移除或未知配置被静默接受")
			}
		})
	}
}
