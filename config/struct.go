package config

type Config struct {
	LLMs      []LLMConfig      `json:"llms"`    // 可供刮削任务引用的具名模型实例
	Scraper   *ScraperConfig   `json:"scraper"` // 刮削任务与输出配置
	TheTVDB   *TheTVDBConfig   `json:"thetvdb"`
	Log       *LogConfig       `json:"log"`       // 日志配置
	Tmdb      *TmdbConfig      `json:"tmdb"`      // TMDB 配置
	Collector *CollectorConfig `json:"collector"` // 目录扫描与监听配置
}

type LogConfig struct {
	Mode  int    `json:"mode"`  // 日志模式：1 stdout，2 logfile，3 both
	Level int    `json:"level"` // 日志等级：0 debug，1 info，2 warning，3 error，4 fatal
	File  string `json:"file"`  // 日志文件路径
}

type TmdbConfig struct {
	ApiHost        string `json:"api_host"`        // TMDB 接口地址
	ApiKey         string `json:"api_key"`         // API 凭据
	ImageHost      string `json:"image_host"`      // 图片地址
	Language       string `json:"language"`        // 语言
	Rating         string `json:"rating"`          // 内容分级
	Proxy          string `json:"proxy"`           // 请求 TMDB 代理，支持 http、https、socks5、socks5h
	TimeoutSeconds int    `json:"timeout_seconds"` // 请求超时时间（秒），未配置或为0时默认30
	RetryCount     int    `json:"retry_count"`     // 请求失败重试次数，0表示不重试
}

type CollectorConfig struct {
	RunMode      int      `json:"run_mode"`       // 运行模式：1 daemon，2 once，3 spec
	Watcher      bool     `json:"watcher"`        // 是否开启文件监听
	CronSeconds  int      `json:"cron_seconds"`   // 定时扫描频率
	CronScan     bool     `json:"cron_scan"`      // 是否开启定时扫描
	CronScanBoot bool     `json:"cron_scan_boot"` // 守护进程模式启动后立即执行一次扫描
	TmpSuffix    []string `json:"tmp_suffix"`     // 临时文件后缀列表
	SkipFolders  []string `json:"skip_folders"`   // 跳过目录，可多个
	SkipKeywords []string `json:"skip_keywords"`  // 跳过文件名中的关键字，可多个
	MoviesDir    []string `json:"movies_dir"`     // 电影文件根目录，可多个
	ShowsDir     []string `json:"shows_dir"`      // 电视剧文件根目录，可多个
}

type NfoField struct {
	Tag   bool `json:"tag"`   // 开启标签
	Genre bool `json:"genre"` // 开启分类
}

type LLMConfig struct {
	Name           string   `json:"name"`                  // 供刮削任务引用的唯一实例名称
	Type           string   `json:"type"`                  // 协议类型：openai 或 jev
	BaseURL        string   `json:"base_url"`              // 该实例的接口地址
	ApiKey         string   `json:"api_key"`               // 仅供该实例使用的访问凭据
	Model          string   `json:"model"`                 // 服务端模型名称
	Proxy          string   `json:"proxy"`                 // 该实例的独立请求代理
	TimeoutSeconds int      `json:"timeout_seconds"`       // 请求超时秒数，默认 30
	Temperature    *float64 `json:"temperature,omitempty"` // 仅 OpenAI 兼容实例允许设置采样温度
}

type ScraperConfig struct {
	Providers         []string `json:"providers"`           // 启用来源及其执行顺序
	ExtractLLM        string   `json:"extract_llm"`         // 文件信息提取所用模型实例名称
	SelectLLM         string   `json:"select_llm"`          // 当前来源候选判断所用模型实例名称
	CacheHours        int      `json:"cache_hours"`         // 来源事实缓存有效小时数，0 禁用磁盘缓存
	JevMatchThreshold float64  `json:"jev_match_threshold"` // 所选 Jev 候选的最低 Noul 匹配概率
	NfoField          NfoField `json:"nfo_field"`           // NFO 字段输出开关
}

type TheTVDBConfig struct {
	BaseURL           string `json:"base_url"`            // TheTVDB 公开网站根地址
	Language          string `json:"language"`            // 网页翻译语言，使用 zho 或 eng 等站点语言代码
	Proxy             string `json:"proxy"`               // 网页及图片请求代理
	TimeoutSeconds    int    `json:"timeout_seconds"`     // 单次请求超时秒数
	RequestIntervalMS int    `json:"request_interval_ms"` // 两次网页请求的最小间隔毫秒数
}
