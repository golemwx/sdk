package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// PaymentClient 访问 core 支付与红包域。
type PaymentClient Client

// Cards 获取绑定银行卡及余额信息。
func (c *PaymentClient) Cards(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, PaymentCards, nil)
}

// QRCode 生成面对面收款二维码。
func (c *PaymentClient) QRCode(ctx context.Context) (json.RawMessage, error) {
	return (*Client)(c).call(ctx, PaymentQRCode, nil)
}

// Collect 确认接收转账。
func (c *PaymentClient) Collect(ctx context.Context, request PaymentCollectRequest) (json.RawMessage, error) {
	if request.InvalidTime == "" || request.TransferID == "" || request.TransactionID == "" || request.ToUsername == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(PaymentCollect), "transfer fields are required")
	}
	return (*Client)(c).call(ctx, PaymentCollect, request)
}

// HongBaoDetail 查询红包领取详情。
func (c *PaymentClient) HongBaoDetail(ctx context.Context, request PaymentHongBaoDetailRequest) (json.RawMessage, error) {
	if request.NativeURL == "" || request.SendUsername == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(PaymentHongBaoDetail), "native_url and sender are required")
	}
	return (*Client)(c).call(ctx, PaymentHongBaoDetail, request)
}

// HongBaoList 查询红包领取列表。
func (c *PaymentClient) HongBaoList(ctx context.Context, request PaymentHongBaoListRequest) (json.RawMessage, error) {
	if request.NativeURL == "" || request.Offset < 0 || request.Limit < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(PaymentHongBaoList), "红包查询参数无效")
	}
	return (*Client)(c).call(ctx, PaymentHongBaoList, request)
}

// GrabHongBao 自动接收并拆开红包。
func (c *PaymentClient) GrabHongBao(ctx context.Context, request PaymentGrabHongBaoRequest) (json.RawMessage, error) {
	if request.NativeURL == "" || (request.InWay != 0 && request.InWay != 1) ||
		(request.InWay == 0 && request.SessionUsername == "") {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(PaymentGrabHongBao), "红包地址或会话无效")
	}
	return (*Client)(c).call(ctx, PaymentGrabHongBao, request)
}
