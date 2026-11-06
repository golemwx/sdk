package plugin_test

import (
	"context"
	"testing"

	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/plugin"
)

type examplePlugin struct {
	*plugin.Base
}

func newExamplePlugin() *examplePlugin {
	return &examplePlugin{Base: plugin.NewBase()}
}

func (*examplePlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{ID: "example", Name: "示例"}
}

func (*examplePlugin) Subscriptions() []event.Subscription {
	return []event.Subscription{{Topic: event.Message, Mode: event.Observe}}
}

func (*examplePlugin) OnEvent(context.Context, event.Event) (event.Result, error) {
	return event.Result{}, nil
}

var (
	_ plugin.Plugin       = (*examplePlugin)(nil)
	_ plugin.EventHandler = (*examplePlugin)(nil)
)

// TestExternalPluginExampleCompiles 验证公开包中的最小插件示例可以从外部包编译。
func TestExternalPluginExampleCompiles(t *testing.T) {
	instance := newExamplePlugin()
	if instance.Context() != nil {
		t.Fatal("unbound Base.Context() must be nil")
	}
	if instance.Metadata().ID != "example" {
		t.Fatalf("Metadata().ID = %q", instance.Metadata().ID)
	}
}
