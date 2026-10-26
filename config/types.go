// Package config 定义插件自身业务配置契约
package config

import "context"

// ApplyStatus 配置应用状态
type ApplyStatus int

const (
	// ApplyStatusUnknown 配置应用状态未知
	ApplyStatusUnknown ApplyStatus = iota
	// ApplyStatusPending 配置已保存，尚待应用
	ApplyStatusPending
	// ApplyStatusApplied 已保存配置已成功应用
	ApplyStatusApplied
	// ApplyStatusFailed 配置应用失败，保留此前生效配置
	ApplyStatusFailed
)

// ConfigSnapshot 插件业务配置快照
type ConfigSnapshot struct {
	// Revision 已保存配置的修订号，不能据此认定配置已生效
	Revision uint64
	// ActiveRevision 当前已生效配置的修订号
	ActiveRevision uint64
	// Data 插件自身业务配置字节，缺失与显式空字节可区分
	Data []byte
	// ApplyStatus 已保存配置的应用状态
	ApplyStatus ApplyStatus
}

// ConfigSaveResult 配置保存接受结果
type ConfigSaveResult struct {
	// Revision 已保存配置的修订号，不能据此认定配置已生效
	Revision uint64
}

// ConfigClient 宿主配置能力
type ConfigClient interface {
	Snapshot(ctx context.Context) (*ConfigSnapshot, error)
	Save(ctx context.Context, expectedRevision uint64, data []byte) (*ConfigSaveResult, error)
}

// ConfigDefaulter 返回业务默认配置
type ConfigDefaulter interface {
	ConfigDefaults() []byte
}

// ConfigValidator 纯校验候选配置
type ConfigValidator interface {
	ConfigValidate(ctx context.Context, candidate []byte) error
}

// ConfigApplier 原子应用候选配置
type ConfigApplier interface {
	ConfigApply(ctx context.Context, candidate []byte) error
}
