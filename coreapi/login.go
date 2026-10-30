package coreapi

import (
	"context"
)

// LoginClient 管理宿主持有的扫码登录会话，不向插件暴露 core 验证 token。
type LoginClient Client

// Start 开始扫码登录并返回宿主不透明会话。
func (c *LoginClient) Start(ctx context.Context) (*LoginSession, error) {
	data, err := (*Client)(c).call(ctx, LoginStart, nil)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[LoginSession](LoginStart, data)
	if err != nil {
		return nil, err
	}
	if result.Token == "" || result.QRCodeURL == "" || result.ExpiresAt.IsZero() {
		return nil, unexpected(operation(LoginStart))
	}
	return &result, nil
}

// Status 查询宿主登录会话状态；core 账号状态使用 AccountStatus。
func (c *LoginClient) Status(ctx context.Context, sessionToken string) (*LoginState, error) {
	if err := required(operation(LoginStatus), sessionToken); err != nil {
		return nil, err
	}
	data, err := (*Client)(c).call(ctx, LoginStatus, LoginCancelRequest{SessionToken: sessionToken})
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[LoginState](LoginStatus, data)
	if err != nil {
		return nil, err
	}
	if result.SessionToken == "" || result.Status == "" || result.UpdatedAt.IsZero() {
		return nil, unexpected(operation(LoginStatus))
	}
	return &result, nil
}

// AccountStatus 查询 core 当前账号状态，未登录时也可调用。
func (c *LoginClient) AccountStatus(ctx context.Context) (*LoginAccountState, error) {
	data, err := (*Client)(c).call(ctx, LoginAccountStatus, nil)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[LoginAccountState](LoginAccountStatus, data)
	if err != nil {
		return nil, err
	}
	if result.Status == "" {
		return nil, unexpected(operation(LoginAccountStatus))
	}
	return &result, nil
}

// Cancel 结束本地登录会话；不登出已经完成的账号。
func (c *LoginClient) Cancel(ctx context.Context, sessionToken string) error {
	if err := required(operation(LoginCancel), sessionToken); err != nil {
		return err
	}
	return (*Client)(c).callVoid(ctx, LoginCancel, LoginCancelRequest{SessionToken: sessionToken})
}

// Logout 退出当前已登录账号。
func (c *LoginClient) Logout(ctx context.Context) error {
	return (*Client)(c).callVoid(ctx, LoginLogout, nil)
}
