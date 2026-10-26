package wire

import (
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/session"
)

// toChangeDomain 编码资料变更域，未知值归一为零值
func toChangeDomain(value event.ChangeDomain) ChangeDomain {
	if value < event.ChangeDomainUnknown || value > event.ChangeDomainAccount {
		return ChangeDomain_CHANGE_DOMAIN_UNKNOWN
	}

	return ChangeDomain(value)
}

// fromChangeDomain 解码资料变更域，未知值归一为零值
func fromChangeDomain(value ChangeDomain) event.ChangeDomain {
	if value < ChangeDomain_CHANGE_DOMAIN_UNKNOWN || value > ChangeDomain_CHANGE_DOMAIN_ACCOUNT {
		return event.ChangeDomainUnknown
	}

	return event.ChangeDomain(value)
}

// toChangeAction 编码资料变更动作，未知值归一为零值
func toChangeAction(value event.ChangeAction) ChangeAction {
	if value < event.ChangeActionUnknown || value > event.ChangeActionDeleted {
		return ChangeAction_CHANGE_ACTION_UNKNOWN
	}

	return ChangeAction(value)
}

// fromChangeAction 解码资料变更动作，未知值归一为零值
func fromChangeAction(value ChangeAction) event.ChangeAction {
	if value < ChangeAction_CHANGE_ACTION_UNKNOWN || value > ChangeAction_CHANGE_ACTION_DELETED {
		return event.ChangeActionUnknown
	}

	return event.ChangeAction(value)
}

// ToWireLease 转换会话租约
func ToWireLease(value *session.Lease) (*Lease, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := toRequiredTimestamp(value.ExpiresAt, "lease.expires_at")
	if err != nil {
		return nil, err
	}

	return &Lease{
		Id:           value.ID,
		Conversation: ToWireConversation(value.Conversation),
		Topics:       append([]string(nil), value.Topics...),
		ExpiresAt:    expiresAt,
	}, nil
}

// FromWireLease 转换 wire 会话租约
func FromWireLease(value *Lease) (*session.Lease, error) {
	if value == nil {
		return nil, nil
	}

	expiresAt, err := fromRequiredTimestamp(value.ExpiresAt, "lease.expires_at")
	if err != nil {
		return nil, err
	}

	return &session.Lease{
		ID:           value.Id,
		Conversation: FromWireConversation(value.Conversation),
		Topics:       append([]string{}, value.Topics...),
		ExpiresAt:    expiresAt,
	}, nil
}

// ToWireChange 转换资料变更
func ToWireChange(value *event.Change) (*Change, error) {
	if value == nil {
		return nil, nil
	}

	contactValue, err := ToWireContact(value.Contact)
	if err != nil {
		return nil, err
	}
	room, err := ToWireRoom(value.Room)
	if err != nil {
		return nil, err
	}
	member, err := ToWireMember(value.Member)
	if err != nil {
		return nil, err
	}
	account, err := ToWireAccount(value.Account)
	if err != nil {
		return nil, err
	}

	return &Change{
		Domain:        toChangeDomain(value.Domain),
		Action:        toChangeAction(value.Action),
		SubjectId:     value.SubjectID,
		ParentId:      cloneString(value.ParentID),
		ChangedFields: toStringList(value.ChangedFields),
		Contact:       contactValue,
		Room:          room,
		Member:        member,
		Account:       account,
	}, nil
}

// FromWireChange 转换 wire 资料变更
func FromWireChange(value *Change) (*event.Change, error) {
	if value == nil {
		return nil, nil
	}

	contactValue, err := FromWireContact(value.Contact)
	if err != nil {
		return nil, err
	}
	room, err := FromWireRoom(value.Room)
	if err != nil {
		return nil, err
	}
	member, err := FromWireMember(value.Member)
	if err != nil {
		return nil, err
	}
	account, err := FromWireAccount(value.Account)
	if err != nil {
		return nil, err
	}

	return &event.Change{
		Domain:        fromChangeDomain(value.Domain),
		Action:        fromChangeAction(value.Action),
		SubjectID:     value.SubjectId,
		ParentID:      cloneString(value.ParentId),
		ChangedFields: fromStringList(value.ChangedFields),
		Contact:       contactValue,
		Room:          room,
		Member:        member,
		Account:       account,
	}, nil
}

// ToWireEvent 转换事件
func ToWireEvent(value *event.Event) (*Event, error) {
	if value == nil {
		return nil, nil
	}

	messageValue, err := ToWireMessage(value.Message)
	if err != nil {
		return nil, err
	}
	change, err := ToWireChange(value.Change)
	if err != nil {
		return nil, err
	}
	lease, err := ToWireLease(value.Lease)
	if err != nil {
		return nil, err
	}
	occurredAt, err := toRequiredTimestamp(value.OccurredAt, "event.occurred_at")
	if err != nil {
		return nil, err
	}

	return &Event{
		Topic:           value.Topic,
		AccountUsername: value.AccountUsername,
		Message:         messageValue,
		Change:          change,
		Lease:           lease,
		OccurredAt:      occurredAt,
	}, nil
}

// FromWireEvent 转换 wire 事件
func FromWireEvent(value *Event) (*event.Event, error) {
	if value == nil {
		return nil, nil
	}

	messageValue, err := FromWireMessage(value.Message)
	if err != nil {
		return nil, err
	}
	change, err := FromWireChange(value.Change)
	if err != nil {
		return nil, err
	}
	lease, err := FromWireLease(value.Lease)
	if err != nil {
		return nil, err
	}
	occurredAt, err := fromRequiredTimestamp(value.OccurredAt, "event.occurred_at")
	if err != nil {
		return nil, err
	}

	return &event.Event{
		Topic:           value.Topic,
		AccountUsername: value.AccountUsername,
		Message:         messageValue,
		Change:          change,
		Lease:           lease,
		OccurredAt:      occurredAt,
	}, nil
}
