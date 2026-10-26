package wire

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

// ToWireMediaRef 转换媒体引用
func ToWireMediaRef(value *sdk.MediaRef) (*MediaRef, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := toRequiredTimestamp(value.ExpiresAt, "media_ref.expires_at")
	if err != nil {
		return nil, err
	}

	return &MediaRef{
		Token:     value.Token,
		Kind:      toMediaKind(value.Kind),
		FileName:  value.FileName,
		SizeBytes: value.SizeBytes,
		ExpiresAt: expiresAt,
	}, nil
}

// FromWireMediaRef 转换 wire 媒体引用
func FromWireMediaRef(value *MediaRef) (*sdk.MediaRef, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := fromRequiredTimestamp(value.ExpiresAt, "media_ref.expires_at")
	if err != nil {
		return nil, err
	}

	return &sdk.MediaRef{
		Token:     value.Token,
		Kind:      fromMediaKind(value.Kind),
		FileName:  value.FileName,
		SizeBytes: value.SizeBytes,
		ExpiresAt: expiresAt,
	}, nil
}

// ToWireRevokeRef 转换撤回引用
func ToWireRevokeRef(value *sdk.RevokeRef) (*RevokeRef, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := toRequiredTimestamp(value.ExpiresAt, "revoke_ref.expires_at")
	if err != nil {
		return nil, err
	}

	return &RevokeRef{Token: value.Token, ExpiresAt: expiresAt}, nil
}

// FromWireRevokeRef 转换 wire 撤回引用
func FromWireRevokeRef(value *RevokeRef) (*sdk.RevokeRef, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := fromRequiredTimestamp(value.ExpiresAt, "revoke_ref.expires_at")
	if err != nil {
		return nil, err
	}

	return &sdk.RevokeRef{Token: value.Token, ExpiresAt: expiresAt}, nil
}

// toWireQuote 复制引用消息摘要到传输模型
func toWireQuote(value *message.Quote) *Quote {
	if value == nil {
		return nil
	}

	return &Quote{
		MessageId:     value.MessageID,
		ActorUsername: value.ActorUsername,
		Digest:        value.Digest,
	}
}

// fromWireQuote 复制传输引用摘要到业务模型
func fromWireQuote(value *Quote) *message.Quote {
	if value == nil {
		return nil
	}

	return &message.Quote{
		MessageID:     value.MessageId,
		ActorUsername: value.ActorUsername,
		Digest:        value.Digest,
	}
}

// ToWireMessage 转换消息模型
func ToWireMessage(value *message.Message) (*Message, error) {
	if value == nil {
		return nil, nil
	}

	createdAt, err := toRequiredTimestamp(value.CreatedAt, "message.created_at")
	if err != nil {
		return nil, err
	}
	media, err := ToWireMediaRef(value.Media)
	if err != nil {
		return nil, err
	}

	return &Message{
		Id:              value.ID,
		Conversation:    ToWireConversation(value.Conversation),
		Actor:           ToWireActor(value.Actor),
		ActorResolution: toActorResolution(value.ActorResolution),
		Direction:       toDirection(value.Direction),
		Kind:            toMessageKind(value.Kind),
		CreatedAt:       createdAt,
		Text:            cloneString(value.Text),
		Mentions:        toStringList(value.Mentions),
		Quote:           toWireQuote(value.Quote),
		Media:           media,
	}, nil
}

// FromWireMessage 转换 wire 消息模型
func FromWireMessage(value *Message) (*message.Message, error) {
	if value == nil {
		return nil, nil
	}

	createdAt, err := fromRequiredTimestamp(value.CreatedAt, "message.created_at")
	if err != nil {
		return nil, err
	}
	media, err := FromWireMediaRef(value.Media)
	if err != nil {
		return nil, err
	}

	return &message.Message{
		ID:              value.Id,
		Conversation:    FromWireConversation(value.Conversation),
		Actor:           FromWireActor(value.Actor),
		ActorResolution: fromActorResolution(value.ActorResolution),
		Direction:       fromDirection(value.Direction),
		Kind:            fromMessageKind(value.Kind),
		CreatedAt:       createdAt,
		Text:            cloneString(value.Text),
		Mentions:        fromStringList(value.Mentions),
		Quote:           fromWireQuote(value.Quote),
		Media:           media,
	}, nil
}

// ToWireSendReceipt 转换发送回执
func ToWireSendReceipt(value *message.SendReceipt) (*SendReceipt, error) {
	if value == nil {
		return nil, nil
	}

	sentAt, err := toRequiredTimestamp(value.SentAt, "send_receipt.sent_at")
	if err != nil {
		return nil, err
	}
	revokeRef, err := ToWireRevokeRef(value.RevokeRef)
	if err != nil {
		return nil, err
	}

	return &SendReceipt{
		MessageId:    value.MessageID,
		Conversation: ToWireConversation(value.Conversation),
		SentAt:       sentAt,
		Outcome:      toWriteOutcome(value.Outcome),
		RevokeRef:    revokeRef,
	}, nil
}

// FromWireSendReceipt 转换 wire 发送回执
func FromWireSendReceipt(value *SendReceipt) (*message.SendReceipt, error) {
	if value == nil {
		return nil, nil
	}

	sentAt, err := fromRequiredTimestamp(value.SentAt, "send_receipt.sent_at")
	if err != nil {
		return nil, err
	}
	revokeRef, err := FromWireRevokeRef(value.RevokeRef)
	if err != nil {
		return nil, err
	}

	return &message.SendReceipt{
		MessageID:    value.MessageId,
		Conversation: FromWireConversation(value.Conversation),
		SentAt:       sentAt,
		Outcome:      fromWriteOutcome(value.Outcome),
		RevokeRef:    revokeRef,
	}, nil
}
