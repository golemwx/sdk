package coreapi

import (
	"context"
	"encoding/json"
)

// OfficialClient 访问 core 公众号域。
type OfficialClient Client

// Follow 关注公众号。
func (c *OfficialClient) Follow(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(OfficialFollow), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialFollow, OfficialUsernameRequest{Username: username})
}

// Quit 取关公众号。
func (c *OfficialClient) Quit(ctx context.Context, username string) (json.RawMessage, error) {
	if err := required(operation(OfficialQuit), username); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialQuit, OfficialUsernameRequest{Username: username})
}

// GetA8Key 获取公众号网页授权信息；敏感字段由宿主剥离。
func (c *OfficialClient) GetA8Key(ctx context.Context, rawURL string) (json.RawMessage, error) {
	if err := required(operation(OfficialGetA8Key), rawURL); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialGetA8Key, OfficialURLRequest{URL: rawURL})
}

// JSAPIPreVerify 验证公众号 JSAPI 权限。
func (c *OfficialClient) JSAPIPreVerify(ctx context.Context, parameters json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(OfficialJSAPIPreVerify), parameters, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialJSAPIPreVerify, OfficialJSAPIRequest{Parameters: parameters})
}

// Oauth 发起公众号 OAuth 授权。
func (c *OfficialClient) Oauth(ctx context.Context, parameters json.RawMessage) (json.RawMessage, error) {
	if err := inlineJSON(operation(OfficialOauth), parameters, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialOauth, OfficialOauthRequest{Parameters: parameters})
}

// ReadArticle 阅读公众号文章。
func (c *OfficialClient) ReadArticle(ctx context.Context, rawURL string) (json.RawMessage, error) {
	if err := required(operation(OfficialReadArticle), rawURL); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialReadArticle, OfficialURLRequest{URL: rawURL})
}

// LikeArticle 阅读并点赞公众号文章。
func (c *OfficialClient) LikeArticle(ctx context.Context, rawURL string) (json.RawMessage, error) {
	if err := required(operation(OfficialLikeArticle), rawURL); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, OfficialLikeArticle, OfficialURLRequest{URL: rawURL})
}
