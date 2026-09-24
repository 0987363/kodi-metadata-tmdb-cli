package config

// 日志输出模式
const (
	LogModeStdout  = 1 // 仅标准输出
	LogModeLogfile = 2 // 仅日志文件
	LogModeBoth    = 3 // 标准输出和日志文件
)

// 日志等级
const (
	LogLevelDebug   = 0 // debug
	LogLevelInfo    = 1 // info
	LogLevelWarning = 2 // warning
	LogLevelError   = 3 // error
	LogLevelFatal   = 4 // fatal
)
