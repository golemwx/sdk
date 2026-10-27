package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// embeddedBasePlugin 验证外部插件依赖的 Base 方法提升契约
type embeddedBasePlugin struct {
	Base

	unloadHost    *Host // OnUnload 内观察到的引用
	unloadError   error // 模拟卸载业务失败
	panicOnUnload bool  // 模拟卸载业务 panic
}

// Descriptor 返回最小测试声明
func (*embeddedBasePlugin) Descriptor() Descriptor { return Descriptor{ID: "embedded-base"} }

// OnUnload 记录卸载回调期间仍可读取的 Host
func (p *embeddedBasePlugin) OnUnload(context.Context) error {
	p.unloadHost = p.Host
	if p.panicOnUnload {
		panic("controlled unload panic")
	}
	return p.unloadError
}

var _ hostBinder = (*embeddedBasePlugin)(nil)

// baseErrorHasCategory 检查测试错误的稳定分类
func baseErrorHasCategory(err error, category sdk.Category) bool {
	var value *sdk.Error
	return errors.As(err, &value) && value.Category == category
}

// TestBaseLocalHost 验证纯本地测试构造不会依赖传输层
func TestBaseLocalHost(t *testing.T) {
	host := &Host{DataDir: t.TempDir()}
	base := NewBase(host)
	if base.Host != host {
		t.Fatal("local Host was not injected")
	}
	if (&Base{}).Host != nil {
		t.Fatal("zero Base has a Host before initialization")
	}
	if err := base.bindHost(&Host{}, &hostBinding{}); !baseErrorHasCategory(err, sdk.CategoryFailedPrecondition) {
		t.Fatalf("formal initialization replaced local Host: %v", err)
	}
}

// TestBaseBindingGeneration 验证稳定字段不被重复绑定和其他连接代次的清理改写
func TestBaseBindingGeneration(t *testing.T) {
	plugin := &embeddedBasePlugin{}
	firstHost, secondHost := &Host{}, &Host{}
	firstBinding, secondBinding := &hostBinding{}, &hostBinding{}
	if err := plugin.bindHost(firstHost, firstBinding); err != nil {
		t.Fatal(err)
	}
	if err := plugin.bindHost(secondHost, secondBinding); !baseErrorHasCategory(err, sdk.CategoryFailedPrecondition) || plugin.Host != firstHost {
		t.Fatalf("repeated binding changed Host: %v", err)
	}
	plugin.unbindHost(secondBinding)
	if plugin.Host != firstHost {
		t.Fatal("foreign cleanup released current Host")
	}
	plugin.unbindHost(firstBinding)
	if plugin.Host != firstHost || plugin.state.Load().binding != nil {
		t.Fatal("cleanup changed stable Host or retained binding")
	}
	if err := plugin.bindHost(secondHost, secondBinding); !baseErrorHasCategory(err, sdk.CategoryFailedPrecondition) {
		t.Fatalf("released Base was reused: %v", err)
	}
	second := &embeddedBasePlugin{}
	if err := second.bindHost(secondHost, secondBinding); err != nil {
		t.Fatal(err)
	}
	second.unbindHost(firstBinding)
	if second.Host != secondHost || second.state.Load().binding != secondBinding {
		t.Fatal("old cleanup released another instance's Host")
	}
}

// TestUnloadRetainsThenReleasesHost 验证正常、失败及 panic 卸载都使稳定 Host 句柄失效
func TestUnloadRetainsThenReleasesHost(t *testing.T) {
	for _, scenario := range []string{"success", "error", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			implementation := &embeddedBasePlugin{panicOnUnload: scenario == "panic"}
			if scenario == "error" {
				implementation.unloadError = sdk.NewError(sdk.CategoryConflict, "fixture.unload", "controlled unload failure")
			}
			server, _ := newExecutionFixture(t, implementation)
			conn, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			server.conn = conn
			binding := &hostBinding{}
			if err := implementation.bindHost(server.host, binding); err != nil {
				t.Fatal(err)
			}
			server.binding = binding
			server.enabled, server.accepting = false, false
			response, _ := server.OnUnload(lifecycleIncomingContext(t.Context(), "unload", "token"), &wire.LifecycleRequest{InvocationId: "unload"})
			var wantCategory sdk.Category
			if scenario == "error" {
				wantCategory = sdk.CategoryConflict
			} else if scenario == "panic" {
				wantCategory = sdk.CategoryInternal
			}
			if sdk.Category(response.GetError().GetCategory()) != wantCategory {
				t.Fatalf("unexpected unload error: %v", response)
			}
			if implementation.unloadHost != server.host || implementation.Host != server.host || implementation.state.Load().binding != nil || conn.GetState() != connectivity.Shutdown {
				t.Fatalf("unexpected unload binding lifecycle: response=%v callback=%p current=%p", response, implementation.unloadHost, implementation.Host)
			}
			if !server.unloaded || server.controlContext.Err() == nil || server.host.TaskContext().Err() == nil || len(server.activeExecutions()) != 0 {
				t.Fatal("unload retained execution or background state")
			}
		})
	}
}

// TestConnectionInvalidationUsesBindingIdentity 验证断连撤销绑定且旧连接清理不影响其他实例
func TestConnectionInvalidationUsesBindingIdentity(t *testing.T) {
	implementation := &embeddedBasePlugin{}
	server, _ := newExecutionFixture(t, implementation)
	firstConn := server.conn
	firstBinding := &hostBinding{}
	if err := implementation.bindHost(server.host, firstBinding); err != nil {
		t.Fatal(err)
	}
	server.binding = firstBinding
	server.invalidateConnection(firstConn, server.controlCancel, firstBinding)
	if implementation.Host != server.host || implementation.state.Load().binding != nil || server.host.TaskContext().Err() == nil {
		t.Fatal("connection invalidation changed Host or retained binding/task context")
	}

	secondHost := &Host{}
	secondBinding := &hostBinding{}
	second := &embeddedBasePlugin{}
	if err := second.bindHost(secondHost, secondBinding); err != nil {
		t.Fatal(err)
	}
	server.executionMu.Lock()
	server.implementation = second
	server.conn = &grpc.ClientConn{}
	server.host = secondHost
	server.binding = secondBinding
	server.executionMu.Unlock()
	server.invalidateConnection(firstConn, server.controlCancel, firstBinding)
	if second.Host != secondHost || second.state.Load().binding != secondBinding {
		t.Fatal("old connection invalidation released new Host")
	}
}
