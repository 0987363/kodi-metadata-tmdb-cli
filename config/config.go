package config

import (
	"log"
	"os"
	"path/filepath"
	"slices"
)

var (
	LLMs      []LLMConfig
	Scraper   *ScraperConfig
	TheTVDB   *TheTVDBConfig
	Log       *LogConfig
	Tmdb      *TmdbConfig
	Collector *CollectorConfig
)

func LoadConfig(file string) {
	// 优先从当前工作目录读取 不存在时回退到可执行文件所在目录
	if !filepath.IsAbs(file) {
		if _, err := os.Stat(file); err != nil {
			if exe, err := os.Executable(); err == nil {
				binFile := filepath.Join(filepath.Dir(exe), filepath.Base(file))
				if _, err := os.Stat(binFile); err == nil {
					file = binFile
				}
			}
		}
	}

	bytes, err := os.ReadFile(file)
	if err != nil {
		log.Fatalf("load config err: %v", err)
	}

	c, err := parseConfig(bytes)
	if err != nil {
		log.Fatalf("parse config err: %v", err)
	}

	LLMs = c.LLMs
	Scraper = c.Scraper
	TheTVDB = c.TheTVDB
	Log = c.Log
	Tmdb = c.Tmdb
	Collector = c.Collector

	validateConfigEnums()
}

// validateConfigEnums 校验并规范化日志配置枚举。
func validateConfigEnums() {
	if Log != nil {
		if !inIntSet(Log.Mode, LogModeStdout, LogModeLogfile, LogModeBoth) {
			log.Printf("invalid log.mode=%d, fallback to %d", Log.Mode, LogModeStdout)
			Log.Mode = LogModeStdout
		}

		if !inIntSet(Log.Level, LogLevelDebug, LogLevelInfo, LogLevelWarning, LogLevelError, LogLevelFatal) {
			log.Printf("invalid log.level=%d, fallback to %d", Log.Level, LogLevelInfo)
			Log.Level = LogLevelInfo
		}
	}

	if Tmdb != nil {
		if Tmdb.TimeoutSeconds <= 0 {
			log.Printf("invalid tmdb.timeout_seconds=%d, fallback to 30", Tmdb.TimeoutSeconds)
			Tmdb.TimeoutSeconds = 30
		}
	}

}

func inIntSet(val int, set ...int) bool {
	return slices.Contains(set, val)
}
