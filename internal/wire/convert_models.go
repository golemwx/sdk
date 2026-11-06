package wire

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
)

// ToWireAccount 转换当前账号快照。
func ToWireAccount(value *sdk.Account) *Account {
	if value == nil {
		return nil
	}
	return &Account{
		Username: value.Username,
		Nickname: value.Nickname,
		Online:   value.Online,
	}
}

// FromWireAccount 转换当前账号快照。
func FromWireAccount(value *Account) *sdk.Account {
	if value == nil {
		return nil
	}
	return &sdk.Account{
		Username: value.GetUsername(),
		Nickname: value.GetNickname(),
		Online:   value.GetOnline(),
	}
}

// ToWireContact 转换联系人快照。
func ToWireContact(value contact.Contact) *Contact {
	return &Contact{
		Username: value.Username,
		Kind:     toWireContactKind(value.Kind),
		Nickname: value.Nickname,
		Remark:   value.Remark,
	}
}

// FromWireContact 转换联系人快照。
func FromWireContact(value *Contact) contact.Contact {
	if value == nil {
		return contact.Contact{}
	}
	return contact.Contact{
		Username: value.GetUsername(),
		Kind:     fromWireContactKind(value.GetKind()),
		Nickname: value.GetNickname(),
		Remark:   value.GetRemark(),
	}
}

func toWireContactKind(value contact.Kind) ContactKind {
	switch value {
	case contact.KindPersonal:
		return ContactKind_CONTACT_KIND_PERSONAL
	case contact.KindRoom:
		return ContactKind_CONTACT_KIND_ROOM
	case contact.KindOfficial:
		return ContactKind_CONTACT_KIND_OFFICIAL
	case contact.KindSelf:
		return ContactKind_CONTACT_KIND_SELF
	default:
		return ContactKind_CONTACT_KIND_UNKNOWN
	}
}

func fromWireContactKind(value ContactKind) contact.Kind {
	switch value {
	case ContactKind_CONTACT_KIND_PERSONAL:
		return contact.KindPersonal
	case ContactKind_CONTACT_KIND_ROOM:
		return contact.KindRoom
	case ContactKind_CONTACT_KIND_OFFICIAL:
		return contact.KindOfficial
	case ContactKind_CONTACT_KIND_SELF:
		return contact.KindSelf
	default:
		return contact.KindUnknown
	}
}

// ToWireChatroom 转换群专属资料。
func ToWireChatroom(value *chatroom.Chatroom) *Chatroom {
	if value == nil {
		return nil
	}
	return &Chatroom{
		Username:      value.Username,
		Name:          value.Name,
		Remark:        value.Remark,
		OwnerUsername: value.OwnerUsername,
	}
}

// FromWireChatroom 转换群专属资料。
func FromWireChatroom(value *Chatroom) *chatroom.Chatroom {
	if value == nil {
		return nil
	}
	return &chatroom.Chatroom{
		Username:      value.GetUsername(),
		Name:          value.GetName(),
		Remark:        value.GetRemark(),
		OwnerUsername: value.GetOwnerUsername(),
	}
}

// ToWireMember 转换群成员。
func ToWireMember(value *chatroom.Member) *Member {
	if value == nil {
		return nil
	}
	return &Member{
		Username:    value.Username,
		Chatroom:    value.Chatroom,
		DisplayName: value.DisplayName,
		Nickname:    value.Nickname,
	}
}

// FromWireMember 转换群成员。
func FromWireMember(value *Member) *chatroom.Member {
	if value == nil {
		return nil
	}
	return &chatroom.Member{
		Username:    value.GetUsername(),
		Chatroom:    value.GetChatroom(),
		DisplayName: value.GetDisplayName(),
		Nickname:    value.GetNickname(),
	}
}
