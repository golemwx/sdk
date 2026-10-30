package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// ContactClient 访问 core 的完整联系人 API。
type ContactClient Client

// List 列出联系人和群 username。
func (c *ContactClient) List(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, ContactList, ContactListRequest{})
}

// Detail 批量读取联系人资料。
func (c *ContactClient) Detail(ctx context.Context, usernames []string) (json.RawMessage, error) {
	if len(usernames) == 0 || len(usernames) > 1000 || containsBlank(usernames) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ContactDetail), "usernames must contain 1-1000 entries")
	}
	return (*Client)(c).call(ctx, ContactDetail, ContactDetailRequest{Usernames: append([]string(nil), usernames...)})
}

// Search 按微信号或手机号搜索。
func (c *ContactClient) Search(ctx context.Context, request ContactSearchRequest) (json.RawMessage, error) {
	if err := required(operation(ContactSearch), request.Keyword); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ContactSearch, request)
}

// SendRequest 发送好友申请。
func (c *ContactClient) SendRequest(ctx context.Context, request ContactFriendRequest) (json.RawMessage, error) {
	if request.V1 == "" || request.V2 == "" || request.Scene < 1 || request.Scene > 255 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ContactSendRequest), "friend request ticket and 1-255 scene are required")
	}
	return (*Client)(c).call(ctx, ContactSendRequest, request)
}

// PassVerify 通过好友申请。
func (c *ContactClient) PassVerify(ctx context.Context, request ContactVerifyRequest) (json.RawMessage, error) {
	if request.V1 == "" || request.V2 == "" || request.Scene < 1 || request.Scene > 255 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ContactPassVerify), "verify ticket and 1-255 scene are required")
	}
	return (*Client)(c).call(ctx, ContactPassVerify, request)
}

// SetRemark 设置或清除联系人备注。
func (c *ContactClient) SetRemark(ctx context.Context, request ContactRemarkRequest) (json.RawMessage, error) {
	if request.Username == "" || request.Remark == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ContactSetRemark), "username and explicit remark are required")
	}
	return (*Client)(c).call(ctx, ContactSetRemark, request)
}

// BlacklistAdd 将联系人加入黑名单。
func (c *ContactClient) BlacklistAdd(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(ContactBlacklistAdd), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ContactBlacklistAdd, UsernameRequest{Username: username})
}

// BlacklistRemove 将联系人移出黑名单。
func (c *ContactClient) BlacklistRemove(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(ContactBlacklistRemove), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ContactBlacklistRemove, UsernameRequest{Username: username})
}

// Delete 删除联系人。
func (c *ContactClient) Delete(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(ContactDelete), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ContactDelete, UsernameRequest{Username: username})
}
