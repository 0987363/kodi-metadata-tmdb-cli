package providers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/artwork"
	"fengqi/kodi-metadata-tmdb-cli/common/ai"
	"fengqi/kodi-metadata-tmdb-cli/common/httpx"
	"fengqi/kodi-metadata-tmdb-cli/common/jev"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
	"fengqi/kodi-metadata-tmdb-cli/thetvdb"
	"fengqi/kodi-metadata-tmdb-cli/tmdb"
)

func New() (*ai.Client, *metadata.Manager, *artwork.Downloader, error) {
	if config.Scraper == nil || len(config.Scraper.Providers) == 0 {
		return nil, nil, nil, errors.New("scraper.providers 至少指定一个来源")
	}
	if config.Scraper.CacheHours < 0 {
		return nil, nil, nil, errors.New("scraper.cache_hours 不能为负数")
	}
	extraction, err := config.FindLLM(config.Scraper.ExtractLLM)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("提取模型: %w", err)
	}
	if extraction.Type != "openai" {
		return nil, nil, nil, errors.New("scraper.extract_llm 必须引用 openai 类型的通用模型")
	}
	extractor, err := ai.New(extraction)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("提取模型 %q: %w", extraction.Name, err)
	}
	selection, err := config.FindLLM(config.Scraper.SelectLLM)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("判断模型: %w", err)
	}
	judge, err := newJudge(selection)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("判断模型 %q: %w", selection.Name, err)
	}
	var all []metadata.Provider
	clients := map[string]*http.Client{}
	for _, name := range config.Scraper.Providers {
		if _, exists := clients[name]; exists {
			return nil, nil, nil, fmt.Errorf("重复的数据源 %q", name)
		}
		switch name {
		case "tmdb":
			if config.Tmdb == nil || config.Tmdb.ApiKey == "" {
				return nil, nil, nil, errors.New("启用 TMDb 需要配置 tmdb.api_key")
			}
			all = append(all, tmdb.NewMetadataProvider(config.Tmdb))
			clients[name] = httpx.NewClient(config.Tmdb.Proxy, config.Tmdb.TimeoutSeconds)
		case "thetvdb":
			if config.TheTVDB == nil {
				return nil, nil, nil, errors.New("缺少 thetvdb 配置")
			}
			c := config.TheTVDB
			client := httpx.NewClient(c.Proxy, c.TimeoutSeconds)
			all = append(all, thetvdb.New(client, c.BaseURL, c.Language, time.Duration(c.RequestIntervalMS)*time.Millisecond))
			clients[name] = client
		default:
			return nil, nil, nil, fmt.Errorf("未知的数据源 %q", name)
		}
	}
	return extractor, metadata.NewManager(all, time.Duration(config.Scraper.CacheHours)*time.Hour, judge), &artwork.Downloader{Clients: clients}, nil
}

func newJudge(c config.LLMConfig) (metadata.Judge, error) {
	switch c.Type {
	case "openai":
		client, err := ai.New(c)
		if err != nil {
			return nil, err
		}
		return ai.NewJudge(client), nil
	case "jev":
		return jev.New(c, config.Scraper.JevMatchThreshold)
	default:
		return nil, fmt.Errorf("不支持的判断模型类型 %q", c.Type)
	}
}
