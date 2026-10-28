package rpc

import (
	"context"
	"errors"

	"github.com/golemwx/sdk"
)

// ConfigProvider 包装插件配置提供者的默认、校验、应用和恢复控制
type ConfigProvider struct {
	client *Client // 当前插件实例的唯一控制客户端
}

// NewConfigProvider 为已启动实例创建配置提供者包装
func NewConfigProvider(client *Client) *ConfigProvider {
	return &ConfigProvider{client: client}
}

// Defaults 读取插件业务默认配置，缺失实现返回 Unimplemented
func (p *ConfigProvider) Defaults(ctx context.Context) ([]byte, error) {
	if p == nil || p.client == nil {
		return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.config_defaults", "configuration provider is unavailable")
	}
	return p.client.ConfigDefaults(ctx)
}

// Validate 执行纯校验，缺失实现返回 Unimplemented 供宿主跳过
func (p *ConfigProvider) Validate(ctx context.Context, candidate []byte) error {
	if p == nil || p.client == nil {
		return sdk.NewError(sdk.CategoryUnavailable, "plugin.config_validate", "configuration provider is unavailable")
	}
	return p.client.ConfigValidate(ctx, candidate)
}

// Apply 应用候选配置，不改变业务准入状态
func (p *ConfigProvider) Apply(ctx context.Context, candidate []byte) error {
	if p == nil || p.client == nil {
		return sdk.NewError(sdk.CategoryUnavailable, "plugin.config_apply", "configuration provider is unavailable")
	}
	return p.client.ConfigApply(ctx, candidate)
}

// Resume 在排空后恢复业务准入并绑定新的后台周期令牌
func (p *ConfigProvider) Resume(ctx context.Context, backgroundToken string) error {
	if p == nil || p.client == nil {
		return sdk.NewError(sdk.CategoryUnavailable, "plugin.config_resume", "configuration provider is unavailable")
	}
	return p.client.ConfigResume(ctx, backgroundToken)
}

// ApplyAndResume 在成功或确定失败后恢复准入，未知结果保持关闭等待宿主重建
func (p *ConfigProvider) ApplyAndResume(ctx context.Context, candidate []byte, backgroundToken string) error {
	applyErr := p.Apply(ctx, candidate)
	if applyErr != nil && !IsDeterminateConfigFailure(applyErr) {
		return applyErr
	}
	if resumeErr := p.Resume(ctx, backgroundToken); resumeErr != nil {
		return markConfigOutcomeUnknown(resumeErr, "plugin.config_resume")
	}
	return applyErr
}

// IsDeterminateConfigFailure 判断配置失败是否保证旧资源与旧配置完整
func IsDeterminateConfigFailure(err error) bool {
	if err == nil {
		return false
	}
	var value *sdk.Error
	if !errors.As(err, &value) || value == nil {
		return false
	}
	return value.Outcome == sdk.OutcomeFailed || value.Category == sdk.CategoryUnimplemented
}

// markConfigOutcomeUnknown 防止恢复控制失败被误报为可回滚的确定结果
func markConfigOutcomeUnknown(err error, operation string) error {
	if err == nil {
		return nil
	}
	var value *sdk.Error
	if errors.As(err, &value) && value != nil {
		copy := *value
		copy.Outcome = sdk.OutcomeUnknown
		return &copy
	}
	return &sdk.Error{
		Category:  sdk.CategoryUnavailable,
		Operation: operation,
		Outcome:   sdk.OutcomeUnknown,
		Reason:    "configuration control result is unknown",
	}
}
