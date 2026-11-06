package main

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/plugin"
)

const (
	fixtureMarkerEnv = "GOLEM_FIXTURE_MARKER"
	fixtureModeEnv   = "GOLEM_FIXTURE_MODE"
)

var (
	markerPath = os.Getenv(fixtureMarkerEnv)
	markerMu   sync.Mutex
)

func mark(value string) {
	if markerPath == "" {
		return
	}
	markerMu.Lock()
	defer markerMu.Unlock()
	file, err := os.OpenFile(markerPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(value + "\n")
	_ = file.Close()
}

type fixture struct {
	*plugin.Base

	mode string
}

func New() *fixture {
	return &fixture{
		Base: plugin.NewBase(),
		mode: os.Getenv(fixtureModeEnv),
	}
}

func (*fixture) Metadata() plugin.Metadata {
	return plugin.Metadata{ID: "subprocess-fixture", Name: "子进程夹具"}
}

func (*fixture) Subscriptions() []event.Subscription {
	return []event.Subscription{{Topic: event.Message, Mode: event.Consume}}
}

func (p *fixture) OnLoad(context.Context) error {
	mark("load")
	return nil
}

func (p *fixture) OnStart(ctx context.Context) error {
	mark("start")
	if p.Context() == nil || p.Context().Err() != nil {
		mark("cycle-invalid")
	} else {
		mark("cycle-active")
	}
	if p.Host == nil {
		mark("host-missing")
	} else if _, err := p.Host.Account.Current(ctx); err != nil {
		mark("host-error")
	} else {
		mark("host-current")
	}
	go p.watchCycle(ctx)
	if p.mode == "start-fail" {
		mark("start-fail")
		return sdk.NewError(sdk.CategoryInternal, "fixture.start", "requested start failure")
	}
	return nil
}

func (p *fixture) watchCycle(ctx context.Context) {
	<-ctx.Done()
	mark("cycle-canceled")
	if p.Host == nil {
		return
	}
	if _, err := p.Host.Account.Current(context.Background()); err != nil {
		mark("host-disabled")
	} else {
		mark("host-still-active")
	}
}

func (*fixture) OnStop(context.Context) error {
	mark("stop")
	return nil
}

func (*fixture) OnUnload(context.Context) error {
	mark("unload")
	return nil
}

func (p *fixture) OnEvent(ctx context.Context, _ event.Event) (event.Result, error) {
	mark("event-start")
	if p.mode == "ignore-cancel" {
		time.Sleep(800 * time.Millisecond)
	}
	if p.Host != nil {
		if _, err := p.Host.Account.Current(ctx); err != nil {
			mark("event-host-error")
		} else {
			mark("event-host-current")
		}
	}
	mark("event-end")
	return event.Result{Consumed: true}, nil
}

func main() {
	plugin.Serve(New())
}
