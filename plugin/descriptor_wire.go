package plugin

import (
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/wire"
)

func toWireMetadata(value Metadata) *wire.Metadata {
	return &wire.Metadata{
		Id:          value.ID,
		Name:        value.Name,
		Description: value.Description,
		Author:      value.Author,
		Version:     value.Version,
	}
}

// toWireDescriptor 独立转换一次描述快照，避免把内部缓存直接交给调用方修改。
func toWireDescriptor(value Descriptor) (*wire.Descriptor, error) {
	subscriptions := make([]*wire.Subscription, len(value.Subscriptions))
	for index, subscription := range value.Subscriptions {
		converted, err := wire.ToWireSubscription(subscription)
		if err != nil {
			return nil, err
		}
		subscriptions[index] = converted
	}
	capabilities := make([]*wire.CapabilitySpec, len(value.Capabilities))
	for index, specification := range value.Capabilities {
		converted, err := wire.ToWireCapabilitySpec(specification)
		if err != nil {
			return nil, err
		}
		capabilities[index] = converted
	}
	return &wire.Descriptor{
		Metadata:      toWireMetadata(value.Metadata),
		Subscriptions: subscriptions,
		Commands:      wire.ToWireCommandSchema(value.Commands),
		Capabilities:  capabilities,
		HasConfig:     value.HasConfig,
		DefaultConfig: wire.ToWireBytes(value.DefaultConfig),
	}, nil
}

// fromWireDescriptor 为宿主返回独立副本，避免调用方修改缓存描述。
func fromWireDescriptor(value *wire.Descriptor) Descriptor {
	if value == nil {
		return Descriptor{}
	}
	subscriptions := make([]event.Subscription, len(value.GetSubscriptions()))
	for index, subscription := range value.GetSubscriptions() {
		subscriptions[index] = wire.FromWireSubscription(subscription)
	}
	capabilities := make([]capability.Spec, len(value.GetCapabilities()))
	for index, specification := range value.GetCapabilities() {
		capabilities[index] = wire.FromWireCapabilitySpec(specification)
	}
	return Descriptor{
		Metadata: Metadata{
			ID:          value.GetMetadata().GetId(),
			Name:        value.GetMetadata().GetName(),
			Description: value.GetMetadata().GetDescription(),
			Author:      value.GetMetadata().GetAuthor(),
			Version:     value.GetMetadata().GetVersion(),
		},
		Subscriptions: subscriptions,
		Commands:      wire.FromWireCommandSchema(value.GetCommands()),
		Capabilities:  capabilities,
		HasConfig:     value.GetHasConfig(),
		DefaultConfig: wire.FromWireBytes(value.GetDefaultConfig()),
	}
}
