package plugin

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	hclog "github.com/hashicorp/go-hclog"
)

const (
	slogLevelTrace = slog.LevelDebug - 4
)

type logIdentityValue struct {
	id   string
	name string
}

// logIdentity 是插件进程与宿主共用的可信身份来源。
//
// 日志记录在 Handle 阶段读取当时的值，因此先创建的 With/WithGroup logger
// 也能在元数据可用后自动携带插件 ID 和名称。
type logIdentity struct {
	value atomic.Pointer[logIdentityValue]
}

func newLogIdentity(id, name string) *logIdentity {
	identity := &logIdentity{}
	identity.Set(id, name)
	return identity
}

func (i *logIdentity) Set(id, name string) {
	if i == nil {
		return
	}
	i.value.Store(&logIdentityValue{
		id:   strings.TrimSpace(id),
		name: strings.TrimSpace(name),
	})
}

func (i *logIdentity) SetName(name string) {
	if i == nil {
		return
	}
	for {
		current := i.value.Load()
		next := &logIdentityValue{name: strings.TrimSpace(name)}
		if current != nil {
			next.id = current.id
		}
		if i.value.CompareAndSwap(current, next) {
			return
		}
	}
}

func (i *logIdentity) Current() (string, string) {
	if i == nil {
		return "", ""
	}
	current := i.value.Load()
	if current == nil {
		return "", ""
	}
	return current.id, current.name
}

// identityHandler 在最终输出前移除不可信字段并注入宿主确认的插件身份。
//
// 无论插件使用 With、WithGroup 还是记录级 Attr，同名保留字段都会被移除；
// 因而插件无法覆盖宿主最终看到的 plugin_id/plugin_name。
type identityHandler struct {
	next     slog.Handler
	identity *logIdentity
}

func (h *identityHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *identityHandler) Handle(ctx context.Context, record slog.Record) error {
	filtered := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if clean, ok := sanitizeLogAttr(attr); ok {
			filtered.AddAttrs(clean)
		}
		return true
	})
	if h.identity != nil {
		id, name := h.identity.Current()
		if id != "" {
			filtered.AddAttrs(slog.String(LogFieldPluginID, id))
		}
		if name != "" {
			filtered.AddAttrs(slog.String(LogFieldPluginName, name))
		}
	}
	return h.next.Handle(ctx, filtered)
}

func (h *identityHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if value, ok := sanitizeLogAttr(attr); ok {
			clean = append(clean, value)
		}
	}
	return &identityHandler{next: h.next.WithAttrs(clean), identity: h.identity}
}

func (h *identityHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &identityHandler{next: h.next.WithGroup(name), identity: h.identity}
}

func sanitizeLogAttr(attr slog.Attr) (slog.Attr, bool) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}, false
	}
	if isReservedLogKey(attr.Key) {
		return slog.Attr{}, false
	}
	if attr.Value.Kind() != slog.KindGroup {
		if attr.Key == "" {
			return slog.Attr{}, false
		}
		return attr, true
	}
	group := attr.Value.Group()
	if len(group) == 0 {
		return slog.Attr{}, false
	}
	clean := make([]slog.Attr, 0, len(group))
	for _, child := range group {
		if value, ok := sanitizeLogAttr(child); ok {
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return slog.Attr{}, false
	}
	return slog.Attr{Key: attr.Key, Value: slog.GroupValue(clean...)}, true
}

// hclogSlogHandler 将 slog 记录转换成 hclog 的扁平静态字段。
//
// go-plugin 只把 stderr 上的 hclog JSON 当作结构化日志通道。group 使用
// 点分前缀表示，既保留字段命名空间，也不需要私有日志 RPC。
type hclogSlogHandler struct {
	logger hclog.Logger
	prefix string
}

func (h *hclogSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return hclogLevelEnabled(h.logger, hclogLevelFromSlog(level))
}

func (h *hclogSlogHandler) Handle(_ context.Context, record slog.Record) error {
	args := make([]any, 0, record.NumAttrs()*2)
	record.Attrs(func(attr slog.Attr) bool {
		args = appendSlogAttr(args, h.prefix, attr)
		return true
	})
	h.logger.Log(hclogLevelFromSlog(record.Level), record.Message, args...)
	return nil
}

func (h *hclogSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	args := make([]any, 0, len(attrs)*2)
	for _, attr := range attrs {
		args = appendSlogAttr(args, h.prefix, attr)
	}
	if len(args) == 0 {
		return h
	}
	return &hclogSlogHandler{logger: h.logger.With(args...), prefix: h.prefix}
}

func (h *hclogSlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &hclogSlogHandler{logger: h.logger, prefix: h.prefix + name + "."}
}

func appendSlogAttr(args []any, prefix string, attr slog.Attr) []any {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return args
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		if len(group) == 0 {
			return args
		}
		nextPrefix := prefix
		if attr.Key != "" {
			nextPrefix += attr.Key + "."
		}
		for _, child := range group {
			args = appendSlogAttr(args, nextPrefix, child)
		}
		return args
	}
	if attr.Key == "" {
		return args
	}
	key := prefix + attr.Key
	if isReservedLogKey(attr.Key) {
		key = attr.Key
	}
	return append(args, key, attr.Value.Any())
}

func hclogLevelFromSlog(level slog.Level) hclog.Level {
	switch {
	case level < slog.LevelDebug:
		return hclog.Trace
	case level < slog.LevelInfo:
		return hclog.Debug
	case level < slog.LevelWarn:
		return hclog.Info
	case level < slog.LevelError:
		return hclog.Warn
	default:
		return hclog.Error
	}
}

func hclogLevelEnabled(logger hclog.Logger, level hclog.Level) bool {
	switch level {
	case hclog.Trace:
		return logger.IsTrace()
	case hclog.Debug:
		return logger.IsDebug()
	case hclog.Info:
		return logger.IsInfo()
	case hclog.Warn:
		return logger.IsWarn()
	case hclog.Error:
		return logger.IsError()
	default:
		return false
	}
}

func isReservedLogKey(key string) bool {
	switch key {
	case LogFieldPluginID, LogFieldPluginName, "timestamp", "@timestamp", "@module", "@message", "@level":
		return true
	default:
		return false
	}
}

type pluginLoggers struct {
	hclog    hclog.Logger
	identity *logIdentity
}

var _ slog.Handler = (*identityHandler)(nil)
var _ slog.Handler = (*hclogSlogHandler)(nil)

func setupPluginLogging() *pluginLoggers {
	identity := newLogIdentity("", "")
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		Output:     os.Stderr,
		JSONFormat: true,
	})
	slog.SetDefault(slog.New(&identityHandler{
		next:     &hclogSlogHandler{logger: logger},
		identity: identity,
	}))
	return &pluginLoggers{hclog: logger, identity: identity}
}
