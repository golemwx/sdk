package coreapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/golemwx/sdk"
)

// MomentsClient 访问 core 朋友圈域。
type MomentsClient Client

// Timeline 获取一页朋友圈时间线。
func (c *MomentsClient) Timeline(ctx context.Context, request MomentsTimelineRequest) (json.RawMessage, error) {
	if len(request.Session) > 64<<10 {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, operation(MomentsTimeline), "timeline session is too large")
	}
	return (*Client)(c).call(ctx, MomentsTimeline, request)
}

// UserPage 获取指定用户的朋友圈页面。
func (c *MomentsClient) UserPage(ctx context.Context, request MomentsUserPageRequest) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, MomentsUserPage, request)
}

// Detail 获取一条朋友圈详情。
func (c *MomentsClient) Detail(ctx context.Context, id uint64) (json.RawMessage, error) {
	if id == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsDetail), "moments ID must be positive")
	}
	return (*Client)(c).call(ctx, MomentsDetail, MomentsIDRequest{ID: id})
}

// Post 发布文字、图片或视频朋友圈。
func (c *MomentsClient) Post(ctx context.Context, request MomentsPostRequest) (json.RawMessage, error) {
	if request.Privacy > 3 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsPost), "privacy must be 0-3")
	}
	hasImages := len(request.Images) > 0 || request.ImagesURL != ""
	hasVideo := request.Video != nil || request.VideoURL != ""
	if !hasImages && !hasVideo && request.Content == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsPost), "content, images or video is required")
	}
	if hasImages && hasVideo {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsPost), "images and video are mutually exclusive")
	}
	imageCount := len(request.Images)
	if request.ImagesURL != "" {
		for _, rawURL := range strings.Split(request.ImagesURL, ",") {
			if err := validHTTPURL(operation(MomentsPost), strings.TrimSpace(rawURL)); err != nil {
				return nil, err
			}
			imageCount++
		}
	}
	if imageCount > 9 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsPost), "at most nine images are allowed")
	}
	for _, ref := range request.Images {
		if err := validateMediaRef(MomentsPost, ref, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.VideoURL != "" {
		if err := validHTTPURL(operation(MomentsPost), request.VideoURL); err != nil {
			return nil, err
		}
	}
	if request.Video != nil {
		if err := validateMediaRef(MomentsPost, *request.Video, sdk.MediaKindVideo); err != nil {
			return nil, err
		}
	}
	if request.Cover != nil || request.CoverURL != "" {
		if !hasVideo {
			return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsPost), "cover requires a video")
		}
	}
	if request.Cover != nil {
		if err := validateMediaRef(MomentsPost, *request.Cover, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.CoverURL != "" {
		if err := validHTTPURL(operation(MomentsPost), request.CoverURL); err != nil {
			return nil, err
		}
	}
	return (*Client)(c).call(ctx, MomentsPost, request)
}

// Comment 点赞或评论朋友圈。
func (c *MomentsClient) Comment(ctx context.Context, request MomentsCommentRequest) (json.RawMessage, error) {
	if request.ID == 0 || request.Username == "" || (request.Type != 1 && request.Type != 2) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsComment), "moments ID, author and interaction type are required")
	}
	if request.Type == 2 && request.Content == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsComment), "comment content is required")
	}
	return (*Client)(c).call(ctx, MomentsComment, request)
}

// Operate 批量删除、修改可见性或取消互动。
func (c *MomentsClient) Operate(ctx context.Context, operations []MomentsOperation) (json.RawMessage, error) {
	if len(operations) == 0 || len(operations) > 1000 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsOperate), "moments operations are out of bounds")
	}
	for _, item := range operations {
		if item.ID == 0 || item.Type < 1 || item.Type > 5 || (item.Type == 4 && len(item.Extend) == 0) {
			return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsOperate), "moments operation is invalid")
		}
	}
	return (*Client)(c).call(ctx, MomentsOperate, MomentsOperateRequest{Operations: append([]MomentsOperation(nil), operations...)})
}

// Sync 获取一页朋友圈变更。
func (c *MomentsClient) Sync(ctx context.Context, key []byte) (json.RawMessage, error) {
	if len(key) > 64<<10 {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, operation(MomentsSync), "sync cursor is too large")
	}
	return (*Client)(c).call(ctx, MomentsSync, MomentsSyncRequest{Key: append([]byte(nil), key...)})
}

// Delete 删除一条朋友圈。
func (c *MomentsClient) Delete(ctx context.Context, id uint64) (json.RawMessage, error) {
	if id == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsDelete), "moments ID must be positive")
	}
	return (*Client)(c).call(ctx, MomentsDelete, MomentsIDRequest{ID: id})
}

// SetPrivacy 修改朋友圈可见性。
func (c *MomentsClient) SetPrivacy(ctx context.Context, request MomentsPrivacyRequest) (json.RawMessage, error) {
	if request.ID == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MomentsSetPrivacy), "moments ID must be positive")
	}
	return (*Client)(c).call(ctx, MomentsSetPrivacy, request)
}
