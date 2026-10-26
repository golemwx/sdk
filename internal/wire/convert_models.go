package wire

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
)

// toAccountStatus 编码账号状态，未知值归一为零值
func toAccountStatus(value sdk.AccountStatus) AccountStatus {
	if value < sdk.AccountStatusUnknown || value > sdk.AccountStatusOffline {
		return AccountStatus_ACCOUNT_STATUS_UNKNOWN
	}

	return AccountStatus(value)
}

// fromAccountStatus 解码账号状态，未知值归一为零值
func fromAccountStatus(value AccountStatus) sdk.AccountStatus {
	if value < AccountStatus_ACCOUNT_STATUS_UNKNOWN || value > AccountStatus_ACCOUNT_STATUS_OFFLINE {
		return sdk.AccountStatusUnknown
	}

	return sdk.AccountStatus(value)
}

// toConversationKind 编码会话种类，未知值归一为零值
func toConversationKind(value sdk.ConversationKind) ConversationKind {
	if value < sdk.ConversationKindUnknown || value > sdk.ConversationKindRoom {
		return ConversationKind_CONVERSATION_KIND_UNKNOWN
	}

	return ConversationKind(value)
}

// fromConversationKind 解码会话种类，未知值归一为零值
func fromConversationKind(value ConversationKind) sdk.ConversationKind {
	if value < ConversationKind_CONVERSATION_KIND_UNKNOWN || value > ConversationKind_CONVERSATION_KIND_ROOM {
		return sdk.ConversationKindUnknown
	}

	return sdk.ConversationKind(value)
}

// toActorResolution 编码发言者解析结果，未知值归一为零值
func toActorResolution(value sdk.ActorResolution) ActorResolution {
	if value < sdk.ActorResolutionUnknown || value > sdk.ActorResolutionNoSender {
		return ActorResolution_ACTOR_RESOLUTION_UNKNOWN
	}

	return ActorResolution(value)
}

// fromActorResolution 解码发言者解析结果，未知值归一为零值
func fromActorResolution(value ActorResolution) sdk.ActorResolution {
	if value < ActorResolution_ACTOR_RESOLUTION_UNKNOWN || value > ActorResolution_ACTOR_RESOLUTION_NO_SENDER {
		return sdk.ActorResolutionUnknown
	}

	return sdk.ActorResolution(value)
}

// toContactKind 编码联系人类型，未知值归一为零值
func toContactKind(value contact.ContactKind) ContactKind {
	if value < contact.ContactKindUnknown || value > contact.ContactKindSelf {
		return ContactKind_CONTACT_KIND_UNKNOWN
	}

	return ContactKind(value)
}

// fromContactKind 解码联系人类型，未知值归一为零值
func fromContactKind(value ContactKind) contact.ContactKind {
	if value < ContactKind_CONTACT_KIND_UNKNOWN || value > ContactKind_CONTACT_KIND_SELF {
		return contact.ContactKindUnknown
	}

	return contact.ContactKind(value)
}

// toMemberRole 编码群成员角色，未知值归一为零值
func toMemberRole(value chatroom.MemberRole) MemberRole {
	if value < chatroom.MemberRoleUnknown || value > chatroom.MemberRoleAdmin {
		return MemberRole_MEMBER_ROLE_UNKNOWN
	}

	return MemberRole(value)
}

// fromMemberRole 解码群成员角色，未知值归一为零值
func fromMemberRole(value MemberRole) chatroom.MemberRole {
	if value < MemberRole_MEMBER_ROLE_UNKNOWN || value > MemberRole_MEMBER_ROLE_ADMIN {
		return chatroom.MemberRoleUnknown
	}

	return chatroom.MemberRole(value)
}

// toDirection 编码消息方向，未知值归一为零值
func toDirection(value message.Direction) Direction {
	if value < message.DirectionUnknown || value > message.DirectionOutbound {
		return Direction_DIRECTION_UNKNOWN
	}

	return Direction(value)
}

// fromDirection 解码消息方向，未知值归一为零值
func fromDirection(value Direction) message.Direction {
	if value < Direction_DIRECTION_UNKNOWN || value > Direction_DIRECTION_OUTBOUND {
		return message.DirectionUnknown
	}

	return message.Direction(value)
}

// toMessageKind 编码消息种类，未知值归一为零值
func toMessageKind(value message.MessageKind) MessageKind {
	if value < message.MessageKindUnknown || value > message.MessageKindSystem {
		return MessageKind_MESSAGE_KIND_UNKNOWN
	}

	return MessageKind(value)
}

// fromMessageKind 解码消息种类，未知值归一为零值
func fromMessageKind(value MessageKind) message.MessageKind {
	if value < MessageKind_MESSAGE_KIND_UNKNOWN || value > MessageKind_MESSAGE_KIND_SYSTEM {
		return message.MessageKindUnknown
	}

	return message.MessageKind(value)
}

// toMediaKind 编码媒体种类，未知值归一为零值
func toMediaKind(value sdk.MediaKind) MediaKind {
	if value < sdk.MediaKindUnknown || value > sdk.MediaKindOther {
		return MediaKind_MEDIA_KIND_UNKNOWN
	}

	return MediaKind(value)
}

// fromMediaKind 解码媒体种类，未知值归一为零值
func fromMediaKind(value MediaKind) sdk.MediaKind {
	if value < MediaKind_MEDIA_KIND_UNKNOWN || value > MediaKind_MEDIA_KIND_OTHER {
		return sdk.MediaKindUnknown
	}

	return sdk.MediaKind(value)
}

// ToWireAccount 转换账号模型
func ToWireAccount(value *sdk.Account) (*Account, error) {
	if value == nil {
		return nil, nil
	}
	if !validAccountState(value.Username, int(value.Status), value.Online) {
		return nil, encodeError("account identity or online status is inconsistent")
	}

	updatedAt, err := toRequiredTimestamp(value.UpdatedAt, "account.updated_at")
	if err != nil {
		return nil, err
	}

	return &Account{
		Username:  value.Username,
		Nickname:  cloneString(value.Nickname),
		Online:    value.Online,
		Status:    toAccountStatus(value.Status),
		UpdatedAt: updatedAt,
	}, nil
}

// FromWireAccount 转换 wire 账号模型
func FromWireAccount(value *Account) (*sdk.Account, error) {
	if value == nil {
		return nil, nil
	}
	if !validAccountState(value.Username, int(value.Status), value.Online) {
		return nil, decodeError("account identity or online status is inconsistent")
	}

	updatedAt, err := fromRequiredTimestamp(value.UpdatedAt, "account.updated_at")
	if err != nil {
		return nil, err
	}

	return &sdk.Account{
		Username:  value.Username,
		Nickname:  cloneString(value.Nickname),
		Online:    value.Online,
		Status:    fromAccountStatus(value.Status),
		UpdatedAt: updatedAt,
	}, nil
}

// validAccountState 校验已知账号状态的身份及在线标记，未来枚举仍按 Unknown 规则转换
func validAccountState(username string, status int, online bool) bool {
	if status < int(sdk.AccountStatusUnknown) || status > int(sdk.AccountStatusOffline) {
		return true
	}
	return online == (status == int(sdk.AccountStatusOnline)) && (username != "" || status <= int(sdk.AccountStatusUnlogged))
}

// ToWireConversation 转换会话模型
func ToWireConversation(value sdk.Conversation) *Conversation {
	return &Conversation{
		AccountUsername: value.AccountUsername,
		Kind:            toConversationKind(value.Kind),
		Id:              value.ID,
	}
}

// FromWireConversation 转换 wire 会话模型
func FromWireConversation(value *Conversation) sdk.Conversation {
	if value == nil {
		return sdk.Conversation{}
	}

	return sdk.Conversation{
		AccountUsername: value.AccountUsername,
		Kind:            fromConversationKind(value.Kind),
		ID:              value.Id,
	}
}

// ToWireActor 转换实际发言者
func ToWireActor(value *sdk.Actor) *Actor {
	if value == nil {
		return nil
	}

	return &Actor{
		Username:    value.Username,
		DisplayName: cloneString(value.DisplayName),
		Resolution:  toActorResolution(value.Resolution),
	}
}

// FromWireActor 转换 wire 实际发言者
func FromWireActor(value *Actor) *sdk.Actor {
	if value == nil {
		return nil
	}

	return &sdk.Actor{
		Username:    value.Username,
		DisplayName: cloneString(value.DisplayName),
		Resolution:  fromActorResolution(value.Resolution),
	}
}

// ToWireContact 转换联系人模型
func ToWireContact(value *contact.Contact) (*Contact, error) {
	if value == nil {
		return nil, nil
	}

	updatedAt, err := toRequiredTimestamp(value.UpdatedAt, "contact.updated_at")
	if err != nil {
		return nil, err
	}

	return &Contact{
		Username:     value.Username,
		Kind:         toContactKind(value.Kind),
		Nickname:     cloneString(value.Nickname),
		Remark:       cloneString(value.Remark),
		AvatarUrl:    cloneString(value.AvatarURL),
		Alias:        cloneString(value.Alias),
		UpdatedAt:    updatedAt,
		Completeness: toCompleteness(value.Completeness),
	}, nil
}

// FromWireContact 转换 wire 联系人模型
func FromWireContact(value *Contact) (*contact.Contact, error) {
	if value == nil {
		return nil, nil
	}

	updatedAt, err := fromRequiredTimestamp(value.UpdatedAt, "contact.updated_at")
	if err != nil {
		return nil, err
	}

	return &contact.Contact{
		Username:     value.Username,
		Kind:         fromContactKind(value.Kind),
		Nickname:     cloneString(value.Nickname),
		Remark:       cloneString(value.Remark),
		AvatarURL:    cloneString(value.AvatarUrl),
		Alias:        cloneString(value.Alias),
		UpdatedAt:    updatedAt,
		Completeness: fromCompleteness(value.Completeness),
	}, nil
}

// ToWireRoom 转换群模型
func ToWireRoom(value *chatroom.Room) (*Room, error) {
	if value == nil {
		return nil, nil
	}

	updatedAt, err := toRequiredTimestamp(value.UpdatedAt, "room.updated_at")
	if err != nil {
		return nil, err
	}

	return &Room{
		Username:            value.Username,
		Name:                cloneString(value.Name),
		Remark:              cloneString(value.Remark),
		OwnerUsername:       cloneString(value.OwnerUsername),
		Announcement:        cloneString(value.Announcement),
		AnnouncementVersion: cloneInt32(value.AnnouncementVersion),
		MemberCount:         cloneInt32(value.MemberCount),
		UpdatedAt:           updatedAt,
		Completeness:        toCompleteness(value.Completeness),
	}, nil
}

// FromWireRoom 转换 wire 群模型
func FromWireRoom(value *Room) (*chatroom.Room, error) {
	if value == nil {
		return nil, nil
	}

	updatedAt, err := fromRequiredTimestamp(value.UpdatedAt, "room.updated_at")
	if err != nil {
		return nil, err
	}

	return &chatroom.Room{
		Username:            value.Username,
		Name:                cloneString(value.Name),
		Remark:              cloneString(value.Remark),
		OwnerUsername:       cloneString(value.OwnerUsername),
		Announcement:        cloneString(value.Announcement),
		AnnouncementVersion: cloneInt32(value.AnnouncementVersion),
		MemberCount:         cloneInt32(value.MemberCount),
		UpdatedAt:           updatedAt,
		Completeness:        fromCompleteness(value.Completeness),
	}, nil
}

// ToWireMember 转换群成员模型
func ToWireMember(value *chatroom.Member) (*Member, error) {
	if value == nil {
		return nil, nil
	}

	joinedAt, err := toOptionalTimestamp(value.JoinedAt, "member.joined_at")
	if err != nil {
		return nil, err
	}

	return &Member{
		RoomUsername: value.RoomUsername,
		UserUsername: value.UserUsername,
		DisplayName:  cloneString(value.DisplayName),
		Nickname:     cloneString(value.Nickname),
		Role:         toMemberRole(value.Role),
		JoinedAt:     joinedAt,
		Completeness: toCompleteness(value.Completeness),
	}, nil
}

// FromWireMember 转换 wire 群成员模型
func FromWireMember(value *Member) (*chatroom.Member, error) {
	if value == nil {
		return nil, nil
	}

	joinedAt, err := fromOptionalTimestamp(value.JoinedAt, "member.joined_at")
	if err != nil {
		return nil, err
	}

	return &chatroom.Member{
		RoomUsername: value.RoomUsername,
		UserUsername: value.UserUsername,
		DisplayName:  cloneString(value.DisplayName),
		Nickname:     cloneString(value.Nickname),
		Role:         fromMemberRole(value.Role),
		JoinedAt:     joinedAt,
		Completeness: fromCompleteness(value.Completeness),
	}, nil
}
