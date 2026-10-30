package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// LabelClient 访问 core 联系人标签域。
type LabelClient Client

// List 获取全部标签。
func (c *LabelClient) List(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, LabelList, nil)
}

// Add 创建标签。
func (c *LabelClient) Add(ctx context.Context, name string) (json.RawMessage, error) {
	if name == "" || len(name) > 4096 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(LabelAdd), "label name is invalid")
	}
	return (*Client)(c).call(ctx, LabelAdd, LabelAddRequest{Name: name})
}

// Delete 删除标签。
func (c *LabelClient) Delete(ctx context.Context, ids []uint32) (json.RawMessage, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(LabelDelete), "label IDs must contain 1-1000 entries")
	}
	return (*Client)(c).call(ctx, LabelDelete, LabelDeleteRequest{IDs: append([]uint32(nil), ids...)})
}

// Update 修改标签名称。
func (c *LabelClient) Update(ctx context.Context, request LabelUpdateRequest) (json.RawMessage, error) {
	if request.ID == 0 || request.Name == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(LabelUpdate), "label ID and name are required")
	}
	return (*Client)(c).call(ctx, LabelUpdate, request)
}

// ModifyContacts 覆盖联系人的完整标签集合。
func (c *LabelClient) ModifyContacts(ctx context.Context, request LabelModifyContactsRequest) (json.RawMessage, error) {
	if len(request.Usernames) == 0 || len(request.Usernames) > 1000 || len(request.IDs) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(LabelModifyContacts), "usernames and label IDs are out of bounds")
	}
	return (*Client)(c).call(ctx, LabelModifyContacts, request)
}
