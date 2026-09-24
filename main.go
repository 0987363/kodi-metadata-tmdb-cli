package main

import (
	"context"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/collector"
	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/providers"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
)

var buildVersion = "dev-master"

type cliOptions struct {
	configFile string
	path       string
	version    bool
}

type pathFlag struct {
	value string
	count int
}

func (p *pathFlag) String() string { return p.value }

func (p *pathFlag) Set(value string) error {
	p.count++
	if p.count > 1 {
		return errors.New("--path 只能指定一次")
	}
	if strings.TrimSpace(value) == "" {
		return errors.New("--path 不能为空")
	}
	p.value = value
	return nil
}

func parseCLI(args []string) (cliOptions, error) {
	var options cliOptions
	var path pathFlag
	flags := flag.NewFlagSet("kodi-metadata-tmdb-cli", flag.ContinueOnError)
	flags.StringVar(&options.configFile, "config", "config.json", "配置文件")
	flags.BoolVar(&options.version, "version", false, "显示版本")
	flags.Var(&path, "path", "要处理的媒体目录")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("不接受位置参数: %v", flags.Args())
	}
	if !options.version && path.count != 1 {
		return options, errors.New("必须指定一个 --path 媒体目录")
	}
	options.path = path.value
	return options, nil
}

func validateMediaPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("媒体目录不能为空")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("无法访问媒体目录 %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("媒体路径不是目录: %s", path)
	}
	return nil
}

func main() {
	options, err := parseCLI(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if options.version {
		fmt.Printf("version: %s, build with: %s\n", buildVersion, runtime.Version())
		return
	}
	if err := validateMediaPath(options.path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	config.LoadConfig(options.configFile)
	utils.InitLogger()
	extractor, manager, images, err := providers.New()
	if err != nil {
		utils.Logger.Error(err)
		os.Exit(1)
	}
	if err := collector.Run(context.Background(), options.path, extractor, manager, images); err != nil {
		utils.Logger.Error(err)
		os.Exit(1)
	}
}
