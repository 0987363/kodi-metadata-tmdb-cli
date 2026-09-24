package main

import (
	"context"
	"fengqi/kodi-metadata-tmdb-cli/collector"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/providers"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"flag"
	"fmt"
	"os"
	"runtime"
)

var (
	configFile   string
	version      bool
	runMode      int
	buildVersion = "dev-master"
)

func init() {
	flag.StringVar(&configFile, "config", "config.json", "config file, read from working dir first, then binary dir")
	flag.BoolVar(&version, "version", false, "display version")
	flag.IntVar(&runMode, "mode", 0, "run mode: 1: daemon, 2: once, 3: spec")
	flag.Parse()
}

func main() {
	if version {
		fmt.Printf("version: %s, build with: %s\n", buildVersion, runtime.Version())
		return
	}

	config.LoadConfig(configFile, runMode)

	utils.InitLogger()
	extractor, manager, images, err := providers.New()
	if err != nil {
		utils.Logger.Error(err)
		os.Exit(1)
	}

	if err := collector.Run(context.Background(), extractor, manager, images); err != nil {
		os.Exit(1)
	}
}
