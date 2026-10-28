package plugin

import (
	"context"
	"slices"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
)

const (
	// businessLimit 普通事件、命令、观察和会话通知共用的实际执行上限
	businessLimit = 4
	// capabilityLimit 被调用能力的独立实际执行上限
	capabilityLimit = 2
	// completionTimeout 单次独立控制确认的最大期限
	completionTimeout = 5 * time.Second
)

// executionKind 将控制操作与两类业务额度分开
type executionKind uint8

const (
	// executionControl 控制操作不占业务额度
	executionControl executionKind = iota
	// executionBusiness 普通业务执行
	executionBusiness
	// executionCapability 被调用能力执行
	executionCapability
)

// beginExecution 在进入用户代码前登记执行，取消或 RPC 返回均不能释放它
func (s *controlServer) beginExecution(ctx context.Context, id string, kind executionKind) error {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.execute", "missing invocation ID")
	}
	if s.controlContext == nil || s.controlContext.Err() != nil || (kind != executionControl && !s.accepting) {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.execute", "instance is not accepting execution")
	}
	if _, exists := s.active[id]; exists {
		return sdk.NewError(sdk.CategoryAlreadyExists, "plugin.execute", "invocation is already running")
	}
	count := 0
	for _, running := range s.active {
		if running == kind {
			count++
		}
	}
	if (kind == executionBusiness && count >= businessLimit) || (kind == executionCapability && count >= capabilityLimit) {
		return sdk.NewError(sdk.CategoryResourceExhausted, "plugin.execute", "actual execution limit reached")
	}
	if s.active == nil {
		s.active = make(map[string]executionKind)
	}
	s.active[id] = kind
	return nil
}

// finishExecution 仅在用户代码退出后释放记录，并通过独立实例 context 确认
func (s *controlServer) finishExecution(id string) {
	// 用户代码已经退出，先释放本地额度，再让宿主确认，通知失败可用 Drain 核对
	s.executionMu.Lock()
	delete(s.active, id)
	s.executionMu.Unlock()
	if s.controlContext == nil || s.completion == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.controlContext, completionTimeout)
	defer cancel()
	_, _ = s.completion.ExecutionCompleted(ctx, &wire.ExecutionCompletedRequest{
		ControlToken: s.controlToken, InvocationId: id, InstanceGeneration: s.generation,
	})
}

// stopAdmission 与执行登记使用同一同步边界关闭准入并撤销后台周期
func (s *controlServer) stopAdmission() {
	s.executionMu.Lock()
	s.accepting = false
	s.admissionEpoch++
	if s.host != nil {
		s.host.stopTasks()
	}
	s.executionMu.Unlock()
}

// resumeAdmission 仅在已加载且仍启用的实例上建立新后台周期并重新开放业务准入
func (s *controlServer) resumeAdmission(backgroundToken string) error {
	if backgroundToken == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config_resume", "missing background token")
	}
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.controlContext == nil || s.controlContext.Err() != nil || !s.loaded || !s.enabled || s.host == nil || s.accepting {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config_resume", "instance cannot resume admission")
	}
	s.admissionEpoch++
	s.host.startTasks(s.controlContext, backgroundToken)
	s.accepting = true
	return nil
}

// activeExecutions 返回稳定排序的真实在途标识副本
func (s *controlServer) activeExecutions() []string {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	ids := make([]string, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// businessContext 只绑定宿主签发的凭据，继续继承父调用的取消和 deadline
func businessContext(ctx context.Context, id, token string) (context.Context, error) {
	if id == "" || token == "" {
		return nil, sdk.NewError(sdk.CategoryPermissionDenied, "plugin.execute", "missing invocation identity")
	}
	return transport.WithInvocation(ctx, id, token), nil
}
