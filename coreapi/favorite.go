package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// FavoriteClient 访问 core 收藏域。
type FavoriteClient Client

// Info 获取收藏容量信息。
func (c *FavoriteClient) Info(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, FavoriteInfo, nil)
}

// BatchGet 批量获取收藏详情。
func (c *FavoriteClient) BatchGet(ctx context.Context, ids []uint32) (json.RawMessage, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(FavoriteBatchGet), "favorite IDs must contain 1-1000 entries")
	}
	return (*Client)(c).call(ctx, FavoriteBatchGet, FavoriteIDsRequest{IDs: append([]uint32(nil), ids...)})
}

// BatchDelete 批量删除收藏。
func (c *FavoriteClient) BatchDelete(ctx context.Context, ids []uint32) (json.RawMessage, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(FavoriteBatchDelete), "favorite IDs must contain 1-1000 entries")
	}
	return (*Client)(c).call(ctx, FavoriteBatchDelete, FavoriteIDsRequest{IDs: append([]uint32(nil), ids...)})
}

// Sync 获取一页收藏变更。
func (c *FavoriteClient) Sync(ctx context.Context, key []byte) (json.RawMessage, error) {
	if len(key) > 64<<10 {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, operation(FavoriteSync), "sync cursor is too large")
	}
	return (*Client)(c).call(ctx, FavoriteSync, FavoriteSyncRequest{Key: append([]byte(nil), key...)})
}

// Share 分享收藏项。
func (c *FavoriteClient) Share(ctx context.Context, request FavoriteShareRequest) (json.RawMessage, error) {
	if request.Receiver == "" || len(request.IDs) == 0 || len(request.IDs) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(FavoriteShare), "receiver and 1-1000 favorite IDs are required")
	}
	return (*Client)(c).call(ctx, FavoriteShare, request)
}

// CheckCDN 检查收藏附件 CDN 状态。
func (c *FavoriteClient) CheckCDN(ctx context.Context, items json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(FavoriteCheckCDN), items, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, FavoriteCheckCDN, FavoriteCheckRequest{Items: items})
}
