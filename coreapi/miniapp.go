package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// MiniappClient 访问 core 小程序域。
type MiniappClient Client

func (c *MiniappClient) app(ctx context.Context, method Method, appID string) (json.RawMessage, error) {
	if err := required(operation(method), appID); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, method, MiniappAppRequest{AppID: appID})
}

// JSLogin 获取小程序 JS 登录授权码。
func (c *MiniappClient) JSLogin(ctx context.Context, appID string) (json.RawMessage, error) {
	return c.app(ctx, MiniappJSLogin, appID)
}

// QrcodeAuthLogin 确认小程序二维码登录授权。
func (c *MiniappClient) QrcodeAuthLogin(ctx context.Context, uuid string) (json.RawMessage, error) {
	if err := required(operation(MiniappQrcodeAuthLogin), uuid); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappQrcodeAuthLogin, MiniappQrcodeRequest{UUID: uuid})
}

// RuntimeSession 获取小程序运行时会话。
func (c *MiniappClient) RuntimeSession(ctx context.Context, appID string) (json.RawMessage, error) {
	return c.app(ctx, MiniappRuntimeSession, appID)
}

// SessionQRCode 获取运行时支付二维码。
func (c *MiniappClient) SessionQRCode(ctx context.Context, parameters json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(MiniappSessionQRCode), parameters, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappSessionQRCode, MiniappRawRequest{Data: parameters})
}

// Operate 提交小程序协议数据操作。
func (c *MiniappClient) Operate(ctx context.Context, appID string, data json.RawMessage, operate int32) (json.RawMessage, error) {
	if appID == "" || operate < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappOperate), "app_id or operate is invalid")
	}
	if err := inlineJSON(operation(MiniappOperate), data, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappOperate, MiniappJSONRequest{AppID: appID, Data: data, Operate: operate})
}

// CloudFunction 调用小程序云函数。
func (c *MiniappClient) CloudFunction(ctx context.Context, appID string, data json.RawMessage) (json.RawMessage, error) {
	if err := required(operation(MiniappCloudFunction), appID); err != nil {
		return nil, err
	}
	if err := inlineJSON(operation(MiniappCloudFunction), data, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappCloudFunction, MiniappJSONRequest{AppID: appID, Data: data})
}

// SendVerifyCode 发送小程序手机号验证码。
func (c *MiniappClient) SendVerifyCode(ctx context.Context, request MiniappVerifyCodeRequest) (json.RawMessage, error) {
	if request.AppID == "" || request.Mobile == "" || (request.Operate != 0 && request.Operate != 1) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappSendVerifyCode), "app_id, mobile or operate is invalid")
	}
	return (*Client)(c).call(ctx, MiniappSendVerifyCode, request)
}

// CheckVerifyCode 校验小程序手机号验证码。
func (c *MiniappClient) CheckVerifyCode(ctx context.Context, request MiniappCheckCodeRequest) (json.RawMessage, error) {
	if request.AppID == "" || request.Mobile == "" || request.Code == "" || (request.Operate != 0 && request.Operate != 1) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappCheckVerifyCode), "verification fields are invalid")
	}
	return (*Client)(c).call(ctx, MiniappCheckVerifyCode, request)
}

// Mobiles 获取小程序绑定手机号列表。
func (c *MiniappClient) Mobiles(ctx context.Context, appID string) (json.RawMessage, error) {
	return c.app(ctx, MiniappMobiles, appID)
}

// AddMobile 绑定小程序手机号。
func (c *MiniappClient) AddMobile(ctx context.Context, request MiniappMobileRequest) (json.RawMessage, error) {
	if request.AppID == "" || request.Mobile == "" || request.Code == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappAddMobile), "app_id, mobile and code are required")
	}
	return (*Client)(c).call(ctx, MiniappAddMobile, request)
}

// DeleteMobile 解绑小程序手机号。
func (c *MiniappClient) DeleteMobile(ctx context.Context, appID, mobile string) (json.RawMessage, error) {
	if appID == "" || mobile == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappDeleteMobile), "app_id and mobile are required")
	}
	return (*Client)(c).call(ctx, MiniappDeleteMobile, MiniappMobileRequest{AppID: appID, Mobile: mobile})
}

// RandomAvatar 获取随机授权头像，并保留设置头像所需的文件标识。
func (c *MiniappClient) RandomAvatar(ctx context.Context, appID string) (*MiniappRandomAvatarResult, error) {
	if err := required(operation(MiniappRandomAvatar), appID); err != nil {
		return nil, err
	}
	data, err := (*Client)(c).call(ctx, MiniappRandomAvatar, MiniappAppRequest{AppID: appID})
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[MiniappRandomAvatarResult](MiniappRandomAvatar, data)
	if err != nil {
		return nil, err
	}
	if result.FileKey == "" {
		return nil, unexpected(operation(MiniappRandomAvatar))
	}
	return &result, nil
}

// AddAvatar 设置小程序头像和昵称。
func (c *MiniappClient) AddAvatar(ctx context.Context, request MiniappAvatarRequest) (json.RawMessage, error) {
	if request.AppID == "" || request.Nickname == "" || request.FileKey == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappAddAvatar), "avatar fields are required")
	}
	return (*Client)(c).call(ctx, MiniappAddAvatar, request)
}

// UploadAvatar 上传小程序头像地址并返回设置头像所需的文件标识。
func (c *MiniappClient) UploadAvatar(ctx context.Context, request MiniappUploadAvatarRequest) (*MiniappAvatarUploadResult, error) {
	if request.AppID == "" || request.ImageURL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappUploadAvatar), "app_id and image_url are required")
	}
	data, err := (*Client)(c).call(ctx, MiniappUploadAvatar, request)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[MiniappAvatarUploadResult](MiniappUploadAvatar, data)
	if err != nil {
		return nil, err
	}
	if result.FileKey == "" {
		return nil, unexpected(operation(MiniappUploadAvatar))
	}
	return &result, nil
}

// Record 获取小程序使用记录。
func (c *MiniappClient) Record(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, MiniappRecord, nil)
}

// AddRecord 添加小程序使用记录。
func (c *MiniappClient) AddRecord(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(MiniappAddRecord), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappAddRecord, MiniappRecordRequest{Username: username})
}

// OpenID 获取业务用户 OpenID。
func (c *MiniappClient) OpenID(ctx context.Context, request MiniappOpenIDRequest) (json.RawMessage, error) {
	if request.AppID == "" || request.Username == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MiniappOpenID), "app_id and username are required")
	}
	return (*Client)(c).call(ctx, MiniappOpenID, request)
}

// OauthSDK 确认第三方 SDK 授权。
func (c *MiniappClient) OauthSDK(ctx context.Context, parameters json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(MiniappOauthSDK), parameters, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappOauthSDK, MiniappRawRequest{Data: parameters})
}

// ThirdAppGrant 获取第三方应用授权码。
func (c *MiniappClient) ThirdAppGrant(ctx context.Context, parameters json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(MiniappThirdAppGrant), parameters, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MiniappThirdAppGrant, MiniappRawRequest{Data: parameters})
}
