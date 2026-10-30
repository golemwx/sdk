package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// CDNClient 上传、下载并签发 core CDN 私有元数据引用。
type CDNClient Client

func (c *CDNClient) upload(ctx context.Context, method Method, kind string, request any) (*ResourceRef, error) {
	data, err := (*Client)(c).call(ctx, method, request)
	if err != nil {
		return nil, err
	}
	ref, err := decodeResult[ResourceRef](method, data)
	if err != nil {
		return nil, err
	}
	if ref.Token == "" || ref.Kind != kind || ref.ExpiresAt.IsZero() {
		return nil, unexpected(operation(method))
	}
	return &ref, nil
}

// UploadImage 上传聊天图片并返回不透明 CDN 资源引用。
func (c *CDNClient) UploadImage(ctx context.Context, ref sdk.MediaRef, purpose string) (*ResourceRef, error) {
	if err := validateMediaRef(CDNUploadImage, ref, sdk.MediaKindImage); err != nil {
		return nil, err
	}
	if purpose != "" && purpose != "chat" && purpose != "record" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadImage), "purpose must be chat or record")
	}
	return c.upload(ctx, CDNUploadImage, "image", CDNUploadImageRequest{Ref: &ref, Purpose: purpose})
}

// UploadImageURL 从 core 可访问的 HTTP(S) 地址上传聊天图片。
func (c *CDNClient) UploadImageURL(ctx context.Context, rawURL, purpose string) (*ResourceRef, error) {
	if err := validHTTPURL(operation(CDNUploadImage), rawURL); err != nil {
		return nil, err
	}
	if purpose != "" && purpose != "chat" && purpose != "record" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadImage), "purpose must be chat or record")
	}
	return c.upload(ctx, CDNUploadImage, "image", CDNUploadImageRequest{URL: rawURL, Purpose: purpose})
}

// UploadVideo 上传聊天视频并返回不透明 CDN 资源引用。
func (c *CDNClient) UploadVideo(ctx context.Context, request CDNUploadVideoRequest) (*ResourceRef, error) {
	if request.Ref == nil && request.VideoURL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadVideo), "video source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(CDNUploadVideo, *request.Ref, sdk.MediaKindVideo); err != nil {
			return nil, err
		}
	}
	if request.ThumbRef != nil {
		if err := validateMediaRef(CDNUploadVideo, *request.ThumbRef, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.VideoURL != "" {
		if err := validHTTPURL(operation(CDNUploadVideo), request.VideoURL); err != nil {
			return nil, err
		}
	}
	if request.ThumbURL != "" {
		if err := validHTTPURL(operation(CDNUploadVideo), request.ThumbURL); err != nil {
			return nil, err
		}
	}
	if request.Purpose != "" && request.Purpose != "chat" && request.Purpose != "record" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadVideo), "purpose must be chat or record")
	}
	return c.upload(ctx, CDNUploadVideo, "video", request)
}

// UploadFile 上传文件并返回不透明 CDN 资源引用。
func (c *CDNClient) UploadFile(ctx context.Context, request CDNUploadFileRequest) (*ResourceRef, error) {
	if request.Ref == nil && request.URL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadFile), "file source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(CDNUploadFile, *request.Ref, sdk.MediaKindFile, sdk.MediaKindOther); err != nil {
			return nil, err
		}
	}
	if request.URL != "" {
		if err := validHTTPURL(operation(CDNUploadFile), request.URL); err != nil {
			return nil, err
		}
	}
	return c.upload(ctx, CDNUploadFile, "file", request)
}

// UploadMomentsImage 上传朋友圈图片并返回可直接交给 Moments.Post 的公开 URL。
func (c *CDNClient) UploadMomentsImage(ctx context.Context, ref sdk.MediaRef) (*MomentsImageUpload, error) {
	if err := validateMediaRef(CDNUploadMomentsImage, ref, sdk.MediaKindImage); err != nil {
		return nil, err
	}
	return c.uploadMomentsImage(ctx, CDNUploadImageRequest{Ref: &ref})
}

// UploadMomentsImageURL 从 core 可访问的 HTTP(S) 地址上传朋友圈图片。
func (c *CDNClient) UploadMomentsImageURL(ctx context.Context, rawURL string) (*MomentsImageUpload, error) {
	if err := validHTTPURL(operation(CDNUploadMomentsImage), rawURL); err != nil {
		return nil, err
	}
	return c.uploadMomentsImage(ctx, CDNUploadImageRequest{URL: rawURL})
}

func (c *CDNClient) uploadMomentsImage(ctx context.Context, request CDNUploadImageRequest) (*MomentsImageUpload, error) {
	data, err := (*Client)(c).call(ctx, CDNUploadMomentsImage, request)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[MomentsImageUpload](CDNUploadMomentsImage, data)
	if err != nil {
		return nil, err
	}
	if result.FileURL == "" {
		return nil, unexpected(operation(CDNUploadMomentsImage))
	}
	return &result, nil
}

// UploadMomentsVideo 上传朋友圈视频并返回可直接交给 Moments.Post 的公开 URL。
func (c *CDNClient) UploadMomentsVideo(ctx context.Context, request CDNUploadVideoRequest) (*MomentsVideoUpload, error) {
	if request.Ref == nil && request.VideoURL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadMomentsVideo), "video source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(CDNUploadMomentsVideo, *request.Ref, sdk.MediaKindVideo); err != nil {
			return nil, err
		}
	}
	if request.ThumbRef != nil {
		if err := validateMediaRef(CDNUploadMomentsVideo, *request.ThumbRef, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.VideoURL != "" {
		if err := validHTTPURL(operation(CDNUploadMomentsVideo), request.VideoURL); err != nil {
			return nil, err
		}
	}
	if request.ThumbURL != "" {
		if err := validHTTPURL(operation(CDNUploadMomentsVideo), request.ThumbURL); err != nil {
			return nil, err
		}
	}
	if request.Purpose != "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(CDNUploadMomentsVideo), "moments video does not accept purpose")
	}
	data, err := (*Client)(c).call(ctx, CDNUploadMomentsVideo, request)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[MomentsVideoUpload](CDNUploadMomentsVideo, data)
	if err != nil {
		return nil, err
	}
	if result.FileURL == "" {
		return nil, unexpected(operation(CDNUploadMomentsVideo))
	}
	return &result, nil
}

func (c *CDNClient) download(ctx context.Context, method Method, kind, mediaKind string, request CDNDownloadRequest) (*sdk.MediaRef, error) {
	if err := validateResourceRef(method, request.Ref, kind); err != nil {
		return nil, err
	}
	data, err := (*Client)(c).call(ctx, method, request)
	if err != nil {
		return nil, err
	}
	ref, err := decodeResult[sdk.MediaRef](method, data)
	if err != nil {
		return nil, err
	}
	expected := sdk.MediaKindImage
	if mediaKind == "video" {
		expected = sdk.MediaKindVideo
	} else if mediaKind == "file" {
		expected = sdk.MediaKindFile
	}
	if err := validateMediaRef(method, ref, expected); err != nil {
		return nil, err
	}
	return &ref, nil
}

// DownloadImage 下载 CDN 图片。
func (c *CDNClient) DownloadImage(ctx context.Context, request CDNDownloadRequest) (*sdk.MediaRef, error) {
	return c.download(ctx, CDNDownloadImage, "image", "image", request)
}

// DownloadVideo 下载 CDN 视频。
func (c *CDNClient) DownloadVideo(ctx context.Context, request CDNDownloadRequest) (*sdk.MediaRef, error) {
	return c.download(ctx, CDNDownloadVideo, "video", "video", request)
}

// DownloadVideoCover 下载 CDN 视频封面。
func (c *CDNClient) DownloadVideoCover(ctx context.Context, request CDNDownloadRequest) (*sdk.MediaRef, error) {
	return c.download(ctx, CDNDownloadVideoCover, "video", "image", request)
}

// DownloadFile 下载 CDN 文件。
func (c *CDNClient) DownloadFile(ctx context.Context, request CDNDownloadRequest) (*sdk.MediaRef, error) {
	return c.download(ctx, CDNDownloadFile, "file", "file", request)
}

// DecodeUploadResult 解出非敏感 CDN 公共元数据；AES key 已由宿主剥离。
func DecodeUploadResult(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return sdk.NewError(sdk.CategoryInternal, "cdn.upload", "empty CDN result")
	}
	return json.Unmarshal(data, target)
}
