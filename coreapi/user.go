package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// UserClient 访问当前账号用户资料及安全设置域。
type UserClient Client

// Profile 获取当前账号资料。
func (c *UserClient) Profile(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, UserProfile, nil)
}

// UpdateProfile 修改当前账号资料。
func (c *UserClient) UpdateProfile(ctx context.Context, profile json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(UserUpdateProfile), profile, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, UserUpdateProfile, UserUpdateProfileRequest{Profile: profile})
}

// UploadAvatar 使用宿主媒体引用上传头像。
func (c *UserClient) UploadAvatar(ctx context.Context, ref sdk.MediaRef) (json.RawMessage, error) {
	if err := validateMediaRef(UserUploadAvatar, ref, sdk.MediaKindImage); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, UserUploadAvatar, UserUploadAvatarRequest{Ref: &ref})
}

// UploadAvatarURL 从 core 可访问的 HTTP(S) 地址上传头像。
func (c *UserClient) UploadAvatarURL(ctx context.Context, rawURL string) (json.RawMessage, error) {
	if err := validHTTPURL(operation(UserUploadAvatar), rawURL); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, UserUploadAvatar, UserUploadAvatarRequest{URL: rawURL})
}

// QRCode 获取个人二维码。
func (c *UserClient) QRCode(ctx context.Context, style uint32) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, UserQRCode, UserQRCodeRequest{Style: style})
}

// SetPrivacy 修改一个隐私选项。
func (c *UserClient) SetPrivacy(ctx context.Context, request UserPrivacyRequest) (json.RawMessage, error) {
	if request.Function <= 0 || request.Value < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(UserSetPrivacy), "privacy option is invalid")
	}
	return (*Client)(c).call(ctx, UserSetPrivacy, request)
}

// SetAlias 修改当前微信号。
func (c *UserClient) SetAlias(ctx context.Context, alias string) (json.RawMessage, error) {
	if err := required(operation(UserSetAlias), alias); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, UserSetAlias, UserAliasRequest{Alias: alias})
}

// SafetyDevices 获取安全设备与验证方式。
func (c *UserClient) SafetyDevices(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, UserSafetyDevices, nil)
}

// DeleteSafeDevice 删除安全设备。
func (c *UserClient) DeleteSafeDevice(ctx context.Context, uuid string) (json.RawMessage, error) {
	if err := required(operation(UserDeleteSafeDevice), uuid); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, UserDeleteSafeDevice, UserDeleteSafeDeviceRequest{UUID: uuid})
}

// ReportMotion 上报当天运动步数。
func (c *UserClient) ReportMotion(ctx context.Context, request UserReportMotionRequest) (json.RawMessage, error) {
	if request.DeviceID == "" || request.DeviceType == "" || request.StepCount < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(UserReportMotion), "motion fields are invalid")
	}
	return (*Client)(c).call(ctx, UserReportMotion, request)
}
