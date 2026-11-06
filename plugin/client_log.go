package plugin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync"

	hclog "github.com/hashicorp/go-hclog"
)

// hclogSlogLogger 把 go-plugin 使用的 hclog 接口转回宿主 slog。
//
// go-plugin 解析插件 stderr 上的结构化日志后，以扁平 key/value 调用该
// logger。所有字段先交给 identityHandler 过滤并注入可信插件身份，再写入
// 宿主配置的 slog handler。
type hclogSlogLogger struct {
	logger *slog.Logger
	name   string
}

var _ hclog.Logger = (*hclogSlogLogger)(nil)

func newHclogSlogLogger(logger *slog.Logger) hclog.Logger {
	return &hclogSlogLogger{logger: logger}
}

func (l *hclogSlogLogger) Trace(msg string, args ...any) {
	l.emit(hclog.Trace, msg, args)
}

func (l *hclogSlogLogger) Debug(msg string, args ...any) {
	l.emit(hclog.Debug, msg, args)
}

func (l *hclogSlogLogger) Info(msg string, args ...any) {
	l.emit(hclog.Info, msg, args)
}

func (l *hclogSlogLogger) Warn(msg string, args ...any) {
	l.emit(hclog.Warn, msg, args)
}

func (l *hclogSlogLogger) Error(msg string, args ...any) {
	l.emit(hclog.Error, msg, args)
}

func (l *hclogSlogLogger) Log(level hclog.Level, msg string, args ...any) {
	l.emit(level, msg, args)
}

func (l *hclogSlogLogger) emit(level hclog.Level, msg string, args []any) {
	slogLevel, ok := slogLevelFromHclog(level)
	if !ok {
		return
	}
	l.logger.LogAttrs(context.Background(), slogLevel, msg, hclogArgsToAttrs(args)...)
}

func (l *hclogSlogLogger) IsTrace() bool {
	level, ok := slogLevelFromHclog(hclog.Trace)
	return ok && l.logger.Enabled(context.Background(), level)
}

func (l *hclogSlogLogger) IsDebug() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}

func (l *hclogSlogLogger) IsInfo() bool {
	return l.logger.Enabled(context.Background(), slog.LevelInfo)
}

func (l *hclogSlogLogger) IsWarn() bool {
	return l.logger.Enabled(context.Background(), slog.LevelWarn)
}

func (l *hclogSlogLogger) IsError() bool {
	return l.logger.Enabled(context.Background(), slog.LevelError)
}

func (l *hclogSlogLogger) GetLevel() hclog.Level {
	if l.IsTrace() {
		return hclog.Trace
	}
	if l.IsDebug() {
		return hclog.Debug
	}
	if l.IsInfo() {
		return hclog.Info
	}
	if l.IsWarn() {
		return hclog.Warn
	}
	if l.IsError() {
		return hclog.Error
	}
	return hclog.Off
}

// SetLevel 不单独保存级别；宿主 slog handler 或其 LevelVar 决定实际过滤。
func (l *hclogSlogLogger) SetLevel(hclog.Level) {}

func (l *hclogSlogLogger) Name() string {
	return l.name
}

func (l *hclogSlogLogger) Named(name string) hclog.Logger {
	next := *l
	next.name = name
	return &next
}

func (l *hclogSlogLogger) ResetNamed(name string) hclog.Logger {
	return l.Named(name)
}

func (l *hclogSlogLogger) With(args ...any) hclog.Logger {
	next := *l
	next.logger = slog.New(l.logger.Handler().WithAttrs(hclogArgsToAttrs(args)))
	return &next
}

func (l *hclogSlogLogger) ImpliedArgs() []any {
	return nil
}

func (l *hclogSlogLogger) StandardLogger(options *hclog.StandardLoggerOptions) *log.Logger {
	return log.New(l.StandardWriter(options), "", 0)
}

func (l *hclogSlogLogger) StandardWriter(options *hclog.StandardLoggerOptions) io.Writer {
	return &hclogStdWriter{logger: l, options: options}
}

func hclogArgsToAttrs(args []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, (len(args)+1)/2)
	for index := 0; index < len(args); index += 2 {
		key, ok := args[index].(string)
		if !ok {
			key = fmt.Sprint(args[index])
		}
		if index+1 >= len(args) {
			attrs = append(attrs, slog.Any(hclog.MissingKey, args[index]))
			break
		}
		attrs = append(attrs, slog.Any(key, args[index+1]))
	}
	return attrs
}

func slogLevelFromHclog(level hclog.Level) (slog.Level, bool) {
	switch level {
	case hclog.Trace:
		return slogLevelTrace, true
	case hclog.Debug:
		return slog.LevelDebug, true
	case hclog.Info, hclog.NoLevel:
		return slog.LevelInfo, true
	case hclog.Warn:
		return slog.LevelWarn, true
	case hclog.Error:
		return slog.LevelError, true
	default:
		return 0, false
	}
}

type hclogStdWriter struct {
	logger  *hclogSlogLogger
	options *hclog.StandardLoggerOptions
}

func (w *hclogStdWriter) Write(data []byte) (int, error) {
	message := strings.TrimRight(string(data), " \t\r\n")
	level := hclog.Info
	if w.options != nil {
		if w.options.ForceLevel != hclog.NoLevel {
			level = w.options.ForceLevel
		} else if w.options.InferLevels {
			level, message = inferStdlogLevel(message)
		}
	}
	w.logger.Log(level, message)
	return len(data), nil
}

func inferStdlogLevel(message string) (hclog.Level, string) {
	switch {
	case strings.HasPrefix(message, "[TRACE]"):
		return hclog.Trace, strings.TrimSpace(message[len("[TRACE]"):])
	case strings.HasPrefix(message, "[DEBUG]"):
		return hclog.Debug, strings.TrimSpace(message[len("[DEBUG]"):])
	case strings.HasPrefix(message, "[INFO]"):
		return hclog.Info, strings.TrimSpace(message[len("[INFO]"):])
	case strings.HasPrefix(message, "[WARN]"):
		return hclog.Warn, strings.TrimSpace(message[len("[WARN]"):])
	case strings.HasPrefix(message, "[ERROR]"):
		return hclog.Error, strings.TrimSpace(message[len("[ERROR]"):])
	case strings.HasPrefix(message, "[ERR]"):
		return hclog.Error, strings.TrimSpace(message[len("[ERR]"):])
	default:
		return hclog.Info, message
	}
}

// streamLogWriter 把 go-plugin stdio 服务收到的未结构化 stdout/stderr
// 按行写入宿主 slog。结构化的插件日志走原始 stderr，不会经过这里。
type streamLogWriter struct {
	logger hclog.Logger
	level  hclog.Level

	mu     sync.Mutex
	buffer []byte
}

const maxStreamLineBytes = 64 << 10

func newStreamLogWriter(logger hclog.Logger, level hclog.Level) io.Writer {
	return &streamLogWriter{logger: logger, level: level}
}

func (w *streamLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, data...)
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSuffix(string(w.buffer[:index]), "\r")
		w.buffer = w.buffer[index+1:]
		if line != "" {
			w.logger.Log(w.level, line)
		}
	}
	if len(w.buffer) > maxStreamLineBytes {
		line := string(w.buffer)
		w.buffer = w.buffer[:0]
		w.logger.Log(w.level, line)
	}
	return len(data), nil
}
