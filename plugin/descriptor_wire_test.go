package plugin

import (
	"math"
	"testing"

	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/protobuf/proto"
)

// TestDescriptorRoundTripPreservesPolicyFields 验证描述中的订阅、能力和默认配置。
func TestDescriptorRoundTripPreservesPolicyFields(t *testing.T) {
	want := Descriptor{
		Metadata: Metadata{ID: "fixture", Name: "示例", Version: "1.0.0"},
		Subscriptions: []event.Subscription{
			{Topic: event.Message, Mode: event.Consume, Priority: -10},
			{Topic: event.Member, Mode: event.Observe},
		},
		Commands: command.Schema{Roots: []command.Node{{Name: "fixture", Scope: command.ScopeOwner}}},
		Capabilities: []capability.Spec{{
			Name:          "fixture.echo",
			InputType:     "text/plain",
			OutputType:    "text/plain",
			MaxInlineSize: capability.MaxInlineSize,
		}},
		HasConfig:     true,
		DefaultConfig: []byte{},
	}

	wireValue, err := toWireDescriptor(want)
	if err != nil {
		t.Fatalf("toWireDescriptor() error = %v", err)
	}
	encoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	decoded := &wire.Descriptor{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	got := fromWireDescriptor(decoded)
	gotWire, err := toWireDescriptor(got)
	if err != nil {
		t.Fatalf("toWireDescriptor(round trip) error = %v", err)
	}
	if !proto.Equal(gotWire, wireValue) {
		t.Fatalf("descriptor changed: got %+v want %+v", got, want)
	}
}

// TestDescriptorRejectsOutOfRangeNumbers 验证整数转换失败而不是静默截断。
func TestDescriptorRejectsOutOfRangeNumbers(t *testing.T) {
	_, err := toWireDescriptor(Descriptor{
		Subscriptions: []event.Subscription{{Priority: int(math.MaxInt32) + 1}},
	})
	if err == nil {
		t.Fatal("out-of-range subscription priority was accepted")
	}

	_, err = wire.ToWireCapabilitySpec(capability.Spec{MaxInlineSize: capability.MaxInlineSize + 1})
	if err == nil {
		t.Fatal("out-of-range capability size was accepted")
	}
}
