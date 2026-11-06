package wire

import (
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/session"
)

// ToWireSubscription 转换订阅声明。
func ToWireSubscription(value event.Subscription) (*Subscription, error) {
	priority, err := toWireInt32(value.Priority, "event.subscription.priority")
	if err != nil {
		return nil, err
	}
	return &Subscription{
		Topic:    string(value.Topic),
		Mode:     toWireSubscriptionMode(value.Mode),
		Priority: priority,
	}, nil
}

// FromWireSubscription 转换订阅声明。
func FromWireSubscription(value *Subscription) event.Subscription {
	if value == nil {
		return event.Subscription{}
	}
	return event.Subscription{
		Topic:    event.Topic(value.GetTopic()),
		Mode:     fromWireSubscriptionMode(value.GetMode()),
		Priority: int(value.GetPriority()),
	}
}

func toWireSubscriptionMode(value event.Mode) SubscriptionMode {
	switch value {
	case event.Observe:
		return SubscriptionMode_SUBSCRIPTION_MODE_OBSERVE
	case event.Consume:
		return SubscriptionMode_SUBSCRIPTION_MODE_CONSUME
	default:
		return SubscriptionMode_SUBSCRIPTION_MODE_UNKNOWN
	}
}

func fromWireSubscriptionMode(value SubscriptionMode) event.Mode {
	switch value {
	case SubscriptionMode_SUBSCRIPTION_MODE_OBSERVE:
		return event.Observe
	case SubscriptionMode_SUBSCRIPTION_MODE_CONSUME:
		return event.Consume
	default:
		return event.ModeUnknown
	}
}

// ToWireEvent 转换瞬时事件。
func ToWireEvent(value event.Event) (*Event, error) {
	timestamp, err := toWireTimestamp(value.OccurredAt)
	if err != nil {
		return nil, err
	}
	messageValue, err := ToWireMessage(value.Message)
	if err != nil {
		return nil, err
	}
	return &Event{
		Topic:      string(value.Topic),
		Message:    messageValue,
		Contact:    toWireContactPointer(value.Contact),
		Chatroom:   ToWireChatroom(value.Chatroom),
		Member:     ToWireMember(value.Member),
		OccurredAt: timestamp,
	}, nil
}

// FromWireEvent 转换瞬时事件。
func FromWireEvent(value *Event) (*event.Event, error) {
	if value == nil {
		return nil, nil
	}
	timestamp, err := fromWireTimestamp(value.GetOccurredAt(), "event.occurred_at")
	if err != nil {
		return nil, err
	}
	messageValue, err := FromWireMessage(value.GetMessage())
	if err != nil {
		return nil, err
	}
	return &event.Event{
		Topic:      event.Topic(value.GetTopic()),
		Message:    messageValue,
		Contact:    fromWireContactPointer(value.GetContact()),
		Chatroom:   FromWireChatroom(value.GetChatroom()),
		Member:     FromWireMember(value.GetMember()),
		OccurredAt: timestamp,
	}, nil
}

func toWireContactPointer(value *contact.Contact) *Contact {
	if value == nil {
		return nil
	}
	return ToWireContact(*value)
}

func fromWireContactPointer(value *Contact) *contact.Contact {
	if value == nil {
		return nil
	}
	result := FromWireContact(value)
	return &result
}

// ToWireEventResult 转换消费结果。
func ToWireEventResult(value event.Result) *EventResult {
	return &EventResult{Consumed: value.Consumed}
}

// FromWireEventResult 转换消费结果。
func FromWireEventResult(value *EventResult) event.Result {
	if value == nil {
		return event.Result{}
	}
	return event.Result{Consumed: value.GetConsumed()}
}

// ToWireLease 转换会话租约。
func ToWireLease(value *session.Lease) (*Lease, error) {
	if value == nil {
		return nil, nil
	}
	timestamp, err := toWireTimestamp(value.ExpiresAt)
	if err != nil {
		return nil, err
	}
	topics := make([]string, len(value.Topics))
	for index, topic := range value.Topics {
		topics[index] = string(topic)
	}
	return &Lease{
		Id:        value.ID,
		Target:    value.Target,
		Topics:    topics,
		ExpiresAt: timestamp,
	}, nil
}

// FromWireLease 转换会话租约。
func FromWireLease(value *Lease) (*session.Lease, error) {
	if value == nil {
		return nil, nil
	}
	timestamp, err := fromWireTimestamp(value.GetExpiresAt(), "session.lease.expires_at")
	if err != nil {
		return nil, err
	}
	topics := make([]event.Topic, len(value.GetTopics()))
	for index, topic := range value.GetTopics() {
		topics[index] = event.Topic(topic)
	}
	return &session.Lease{
		ID:        value.GetId(),
		Target:    value.GetTarget(),
		Topics:    topics,
		ExpiresAt: timestamp,
	}, nil
}
