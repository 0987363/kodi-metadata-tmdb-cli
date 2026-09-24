package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestProviderRejectsRedirectWithoutLeakingAPIRequest(t *testing.T) {
	var forwarded atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.Write([]byte(`{"id":42,"title":"Movie"}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", other.URL+"/foreign")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	provider := NewMetadataProvider(&config.TmdbConfig{ApiHost: origin.URL, ApiKey: "test-private-key"})
	_, err := provider.Fetch(context.Background(), metadata.Request{Kind: metadata.Movie, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "42"}})
	if err == nil || forwarded.Load() != 0 {
		t.Fatalf("API请求离开配置端点: forwarded=%d err=%v", forwarded.Load(), err)
	}
}
