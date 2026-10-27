package rpc

import (
	"context"
	"crypto/subtle"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Invocation 宿主反向服务边界收到的调用凭据，不从请求决定插件身份
type Invocation struct {
	ID    string // 宿主签发的执行标识，空值仅可按独立后台授权处理
	Token string // 宿主签发的本次执行凭据
}

// HostServices 当前实例绑定的宿主实现与授权入口
type HostServices struct {
	Host *plugin.Host // 已绑定实例和账号的七类宿主业务实现
	// 检查实例、调用凭据及操作授权，未提供时默认拒绝
	Authorize    func(context.Context, Invocation, string) (context.Context, error)
	ReverseReady func(context.Context) error // 可选的反向连接就绪校验，失败会使整个启动失败
	// 仅在 handler 实际退出后接收当前实例的独立完成确认
	ExecutionCompleted func(ctx context.Context, invocationID string, generation uint64) error
}

// hostControl 将就绪确认绑定到当前实例的随机控制凭据
type hostControl struct {
	wire.UnimplementedHostControlServiceServer // 为未来新增控制方法提供明确失败

	token        string                                      // 当前实例唯一的随机控制凭据
	ready        chan struct{}                               // 反向连接就绪信号
	once         sync.Once                                   // 保证就绪信号只关闭一次
	validate     func(context.Context) error                 // 可选的宿主就绪校验
	generation   uint64                                      // 反向连接绑定的实例代次
	completed    func(context.Context, string, uint64) error // 宿主实际执行记录的完成入口，不能根据业务 RPC 返回自行触发
	invocationMu sync.Mutex                                  // 保护用户调用权限，与真实执行完成记录独立
	invocations  map[string]hostInvocation                   // 仅保存宿主仍在等待且尚未取消的用户调用
}

// hostInvocation 将随机凭据绑定到宿主原调用上下文
type hostInvocation struct {
	token string          // 宿主为本次调用签发的随机凭据
	ctx   context.Context // 原用户调用期限与取消信号
}

// bindInvocation 在发起业务 RPC 前登记权限，返回后撤销，不等同于实际执行结束
func (s *hostControl) bindInvocation(ctx context.Context, invocation Invocation) (func(), error) {
	s.invocationMu.Lock()
	defer s.invocationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, transport.DecodeError(nil, err, "plugin.execute")
	}
	if _, exists := s.invocations[invocation.ID]; exists {
		return nil, sdk.NewError(sdk.CategoryAlreadyExists, "plugin.execute", "invocation ID is already in use")
	}
	if s.invocations == nil {
		s.invocations = make(map[string]hostInvocation)
	}
	s.invocations[invocation.ID] = hostInvocation{token: invocation.Token, ctx: ctx}
	return func() {
		s.invocationMu.Lock()
		delete(s.invocations, invocation.ID)
		s.invocationMu.Unlock()
	}, nil
}

// invocationContext 拒绝已返回、过期或取消的用户凭据，空身份仍需宿主独立授权
func (s *hostControl) invocationContext(invocation Invocation) (context.Context, error) {
	if invocation.ID == "" && invocation.Token == "" {
		return nil, nil
	}
	s.invocationMu.Lock()
	defer s.invocationMu.Unlock()
	value, ok := s.invocations[invocation.ID]
	if !ok || invocation.Token == "" || subtle.ConstantTimeCompare([]byte(invocation.Token), []byte(value.token)) != 1 || value.ctx.Err() != nil {
		return nil, sdk.NewError(sdk.CategoryPermissionDenied, "host.authorize", "invocation permission is no longer valid")
	}
	return value.ctx, nil
}

// ExecutionCompleted 校验连接绑定的控制凭据和代次，迟到旧通知无法作用于新实例
func (s *hostControl) ExecutionCompleted(ctx context.Context, request *wire.ExecutionCompletedRequest) (*wire.OperationResponse, error) {
	if s.token == "" || subtle.ConstantTimeCompare([]byte(request.GetControlToken()), []byte(s.token)) != 1 || request.GetInstanceGeneration() != s.generation {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryPermissionDenied, "host.execution_completed", "invalid instance control identity"))}, nil
	}
	if request.GetInvocationId() == "" {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryInvalidArgument, "host.execution_completed", "missing invocation ID"))}, nil
	}
	var err error
	if s.completed != nil {
		err = s.completed(ctx, request.InvocationId, s.generation)
	}
	return &wire.OperationResponse{Error: transport.EncodeError(err, "host.execution_completed")}, nil
}

// ReverseReady 验证控制身份并确认已通过安全反向连接
func (s *hostControl) ReverseReady(ctx context.Context, request *wire.ReverseReadyRequest) (*wire.OperationResponse, error) {
	if subtle.ConstantTimeCompare([]byte(request.GetControlToken()), []byte(s.token)) != 1 {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryPermissionDenied, "host.reverse_ready", "invalid control token"))}, nil
	}
	if s.validate != nil {
		if err := s.validate(ctx); err != nil {
			return &wire.OperationResponse{Error: transport.EncodeError(err, "host.reverse_ready")}, nil
		}
	}
	s.once.Do(func() { close(s.ready) })
	return &wire.OperationResponse{}, nil
}

// hostInterceptor 在可信宿主边界检查每次回调，不将无 token 调用视为管理员
func hostInterceptor(services HostServices, control *hostControl) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recover() != nil {
				response, err = nil, status.Error(codes.Internal, "host operation panicked")
			}
		}()
		if info.FullMethod == wire.HostControlService_ReverseReady_FullMethodName || info.FullMethod == wire.HostControlService_ExecutionCompleted_FullMethodName {
			return handler(ctx, request)
		}
		if services.Authorize == nil {
			return nil, status.Error(codes.PermissionDenied, "host authorization is required")
		}
		var invocation Invocation
		if value, ok := request.(interface{ GetInvocation() *wire.Invocation }); ok {
			invocation.ID, invocation.Token = value.GetInvocation().GetId(), value.GetInvocation().GetToken()
		}
		if control != nil {
			parent, err := control.invocationContext(invocation)
			if err != nil {
				return nil, transport.RPCError(err, info.FullMethod)
			}
			if parent != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				stop := context.AfterFunc(parent, cancel)
				defer stop()
				if deadline, ok := parent.Deadline(); ok {
					var cancelDeadline context.CancelFunc
					ctx, cancelDeadline = context.WithDeadline(ctx, deadline)
					defer cancelDeadline()
				}
			}
		}
		bound, err := services.Authorize(ctx, invocation, info.FullMethod)
		if err != nil {
			return nil, transport.RPCError(err, info.FullMethod)
		}
		if bound == nil {
			return nil, status.Error(codes.Internal, "authorization returned no context")
		}
		// 保留授权上下文的值，同时强制继承原请求的取消和更早的截止时间
		bound, cancel := context.WithCancel(bound)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		if deadline, ok := ctx.Deadline(); ok {
			var cancelDeadline context.CancelFunc
			bound, cancelDeadline = context.WithDeadline(bound, deadline)
			defer cancelDeadline()
		}
		if err := ctx.Err(); err != nil {
			return nil, transport.RPCError(err, info.FullMethod)
		}
		if err := bound.Err(); err != nil {
			return nil, transport.RPCError(err, info.FullMethod)
		}
		return handler(bound, request)
	}
}

// registerHost 将七类 Host 服务及控制确认注册到同一安全连接
func registerHost(server *grpc.Server, services HostServices, control *hostControl) {
	host := services.Host
	if host == nil {
		host = &plugin.Host{}
	}
	wire.RegisterHostControlServiceServer(server, control)
	if host.Messages != nil {
		wire.RegisterHostMessagesServiceServer(server, &hostMessages{client: host.Messages})
	} else {
		wire.RegisterHostMessagesServiceServer(server, &wire.UnimplementedHostMessagesServiceServer{})
	}
	if host.Contacts != nil {
		wire.RegisterHostContactsServiceServer(server, &hostContacts{client: host.Contacts})
	} else {
		wire.RegisterHostContactsServiceServer(server, &wire.UnimplementedHostContactsServiceServer{})
	}
	if host.Chatrooms != nil {
		wire.RegisterHostChatroomsServiceServer(server, &hostChatrooms{client: host.Chatrooms})
	} else {
		wire.RegisterHostChatroomsServiceServer(server, &wire.UnimplementedHostChatroomsServiceServer{})
	}
	if host.Sessions != nil {
		wire.RegisterHostSessionsServiceServer(server, &hostSessions{client: host.Sessions})
	} else {
		wire.RegisterHostSessionsServiceServer(server, &wire.UnimplementedHostSessionsServiceServer{})
	}
	if host.Caller != nil {
		wire.RegisterHostCallerServiceServer(server, &hostCaller{client: host.Caller})
	} else {
		wire.RegisterHostCallerServiceServer(server, &wire.UnimplementedHostCallerServiceServer{})
	}
	if host.Config != nil {
		wire.RegisterHostConfigServiceServer(server, &hostConfig{client: host.Config})
	} else {
		wire.RegisterHostConfigServiceServer(server, &wire.UnimplementedHostConfigServiceServer{})
	}
	if host.Account != nil {
		wire.RegisterHostAccountServiceServer(server, &hostAccount{client: host.Account})
	} else {
		wire.RegisterHostAccountServiceServer(server, &wire.UnimplementedHostAccountServiceServer{})
	}
}

// hostMessages 消息一元 RPC 的业务适配，流控在 T26 接入
type hostMessages struct {
	wire.UnimplementedHostMessagesServiceServer // 未接入的媒体流明确返回 Unimplemented

	client plugin.MessagesClient // 已绑定实例的消息能力
}

// SendText 将文本请求转换为公开业务类型
func (s *hostMessages) SendText(ctx context.Context, r *wire.SendTextRequest) (*wire.SendReceiptResponse, error) {
	value, err := s.client.SendText(ctx, wire.FromWireConversation(r.GetConversation()), r.GetText(), r.GetMentions())
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.send_text")}, nil
}

// Reply 将引用回复请求转换为公开业务类型
func (s *hostMessages) Reply(ctx context.Context, r *wire.ReplyRequest) (*wire.SendReceiptResponse, error) {
	q := r.GetQuote()
	value, err := s.client.Reply(ctx, wire.FromWireConversation(r.GetConversation()), message.Quote{MessageID: q.GetMessageId(), ActorUsername: q.GetActorUsername(), Digest: q.GetDigest()}, r.GetText())
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.reply")}, nil
}

// Revoke 校验引用的传输格式后调用宿主
func (s *hostMessages) Revoke(ctx context.Context, r *wire.RevokeRequest) (*wire.OperationResponse, error) {
	ref, err := wire.FromWireRevokeRef(r.GetRef())
	if err == nil && ref == nil {
		err = sdk.NewError(sdk.CategoryInvalidArgument, "messages.revoke", "missing revoke reference")
	}
	if err == nil {
		err = s.client.Revoke(ctx, *ref)
	}
	return &wire.OperationResponse{Error: transport.EncodeError(err, "messages.revoke")}, nil
}

// hostContacts 本地联系人查询及显式刷新适配
type hostContacts struct {
	wire.UnimplementedHostContactsServiceServer // 为未来兼容方法提供明确失败

	client plugin.ContactsClient // 已绑定实例的联系人能力
}

// Get 查询单个联系人
func (s *hostContacts) Get(ctx context.Context, r *wire.ContactGetRequest) (*wire.ContactResponse, error) {
	value, err := s.client.Get(ctx, r.GetUsername())
	result, conversion := wire.ToWireContact(value)
	return &wire.ContactResponse{Contact: result, Error: hostResultError(result != nil, err, conversion, "contacts.get")}, nil
}

// Find 保留查询条件及分页边界
func (s *hostContacts) Find(ctx context.Context, r *wire.ContactFindRequest) (*wire.ContactPageResponse, error) {
	page, err := wire.FromWirePageRequest(r.GetPage())
	if err != nil {
		return &wire.ContactPageResponse{Error: transport.EncodeError(err, "contacts.find")}, nil
	}
	value, err := s.client.Find(ctx, r.GetQuery(), page)
	result, conversion := wire.ToWireContactPage(value)
	return &wire.ContactPageResponse{Page: result, Error: hostResultError(result != nil, err, conversion, "contacts.find")}, nil
}

// List 规范化分页参数后查询本地列表
func (s *hostContacts) List(ctx context.Context, r *wire.ContactListRequest) (*wire.ContactPageResponse, error) {
	page, err := wire.FromWirePageRequest(r.GetPage())
	if err != nil {
		return &wire.ContactPageResponse{Error: transport.EncodeError(err, "contacts.list")}, nil
	}
	value, err := s.client.List(ctx, page)
	result, conversion := wire.ToWireContactPage(value)
	return &wire.ContactPageResponse{Page: result, Error: hostResultError(result != nil, err, conversion, "contacts.list")}, nil
}

// Refresh 同时保留逐项刷新结果及整体错误
func (s *hostContacts) Refresh(ctx context.Context, r *wire.ContactRefreshRequest) (*wire.ContactBatchResponse, error) {
	value, err := s.client.Refresh(ctx, r.GetUsernames())
	result, conversion := wire.ToWireContactBatchResult(value)
	return &wire.ContactBatchResponse{Result: result, Error: hostResultError(result != nil, err, conversion, "contacts.refresh")}, nil
}

// hostChatrooms 本地群与成员查询适配
type hostChatrooms struct {
	wire.UnimplementedHostChatroomsServiceServer // 为未来兼容方法提供明确失败

	client plugin.ChatroomsClient // 已绑定实例的群能力
}

// Get 查询单个群资料
func (s *hostChatrooms) Get(ctx context.Context, r *wire.ChatroomGetRequest) (*wire.RoomResponse, error) {
	value, err := s.client.Get(ctx, r.GetRoomUsername())
	result, conversion := wire.ToWireRoom(value)
	return &wire.RoomResponse{Room: result, Error: hostResultError(result != nil, err, conversion, "chatrooms.get")}, nil
}

// Members 规范化分页后查询群成员
func (s *hostChatrooms) Members(ctx context.Context, r *wire.ChatroomMembersRequest) (*wire.MemberPageResponse, error) {
	page, err := wire.FromWirePageRequest(r.GetPage())
	if err != nil {
		return &wire.MemberPageResponse{Error: transport.EncodeError(err, "chatrooms.members")}, nil
	}
	value, err := s.client.Members(ctx, r.GetRoomUsername(), page)
	result, conversion := wire.ToWireMemberPage(value)
	return &wire.MemberPageResponse{Page: result, Error: hostResultError(result != nil, err, conversion, "chatrooms.members")}, nil
}

// Member 查询指定群成员
func (s *hostChatrooms) Member(ctx context.Context, r *wire.ChatroomMemberRequest) (*wire.MemberResponse, error) {
	value, err := s.client.Member(ctx, r.GetRoomUsername(), r.GetUserUsername())
	result, conversion := wire.ToWireMember(value)
	return &wire.MemberResponse{Member: result, Error: hostResultError(result != nil, err, conversion, "chatrooms.member")}, nil
}

// Refresh 保留分阶段结果，不将部分失败折叠为成功
func (s *hostChatrooms) Refresh(ctx context.Context, r *wire.ChatroomRefreshRequest) (*wire.ChatroomRefreshResponse, error) {
	value, err := s.client.Refresh(ctx, r.GetRoomUsername())
	result, conversion := wire.ToWireChatroomRefreshResult(value)
	return &wire.ChatroomRefreshResponse{Result: result, Error: hostResultError(result != nil, err, conversion, "chatrooms.refresh")}, nil
}

// hostSessions 会话租约的宿主适配
type hostSessions struct {
	wire.UnimplementedHostSessionsServiceServer // 为未来兼容方法提供明确失败

	client plugin.SessionsClient // 已绑定实例的租约能力
}

// Acquire 为联系人或整群申请租约
func (s *hostSessions) Acquire(ctx context.Context, r *wire.SessionAcquireRequest) (*wire.LeaseResponse, error) {
	value, err := s.client.Acquire(ctx, wire.FromWireConversation(r.GetConversation()), r.GetTopics())
	result, conversion := wire.ToWireLease(value)
	return &wire.LeaseResponse{Lease: result, Error: hostResultError(result != nil, err, conversion, "sessions.acquire")}, nil
}

// Renew 校验续期时长的传输表示后续租
func (s *hostSessions) Renew(ctx context.Context, r *wire.SessionRenewRequest) (*wire.LeaseResponse, error) {
	ttl, err := wire.FromWireDuration(r.GetTtl(), "session.ttl")
	if r.GetTtl() == nil || err != nil {
		return &wire.LeaseResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryInvalidArgument, "sessions.renew", "invalid lease duration"))}, nil
	}
	value, err := s.client.Renew(ctx, r.GetLeaseId(), ttl)
	result, conversion := wire.ToWireLease(value)
	return &wire.LeaseResponse{Lease: result, Error: hostResultError(result != nil, err, conversion, "sessions.renew")}, nil
}

// Release 释放当前实例的租约
func (s *hostSessions) Release(ctx context.Context, r *wire.SessionReleaseRequest) (*wire.OperationResponse, error) {
	return &wire.OperationResponse{Error: transport.EncodeError(s.client.Release(ctx, r.GetLeaseId()), "sessions.release")}, nil
}

// hostCaller 按能力名路由的宿主适配
type hostCaller struct {
	wire.UnimplementedHostCallerServiceServer // 为未来兼容方法提供明确失败

	client plugin.CallerClient // 已绑定实例的能力调用入口
}

// Call 转换能力输入和结果，不允许请求覆盖绑定身份
func (s *hostCaller) Call(ctx context.Context, r *wire.HostCallRequest) (*wire.HostCallResponse, error) {
	value, err := s.client.Call(ctx, wire.FromWireCallRequest(r.GetRequest()))
	var result *wire.CallResult
	var conversion error
	if value != nil {
		result, conversion = wire.ToWireCallResult(*value)
	}
	return &wire.HostCallResponse{Result: result, Error: hostResultError(result != nil, err, conversion, "caller.call")}, nil
}

// hostConfig 插件自身业务配置的宿主适配
type hostConfig struct {
	wire.UnimplementedHostConfigServiceServer // 为未来兼容方法提供明确失败

	client plugin.ConfigClient // 已绑定实例的自身配置入口
}

// Snapshot 查询插件自身配置及当前生效修订
func (s *hostConfig) Snapshot(ctx context.Context, r *wire.ConfigSnapshotRequest) (*wire.ConfigSnapshotResponse, error) {
	value, err := s.client.Snapshot(ctx)
	return &wire.ConfigSnapshotResponse{Snapshot: wire.ToWireConfigSnapshot(value), Error: hostResultError(value != nil, err, nil, "config.snapshot")}, nil
}

// Save 保留预期修订号及配置字节的存在性
func (s *hostConfig) Save(ctx context.Context, r *wire.ConfigSaveRequest) (*wire.ConfigSaveResponse, error) {
	var data []byte
	if r.GetData() != nil {
		data = append([]byte{}, r.Data.Value...)
	}
	value, err := s.client.Save(ctx, r.GetExpectedRevision(), data)
	response := &wire.ConfigSaveResponse{Error: hostResultError(value != nil, err, nil, "config.save")}
	if value != nil {
		response.Result = &wire.ConfigSaveResult{Revision: value.Revision}
	}
	return response, nil
}

// hostAccount 当前账号快照的宿主适配
type hostAccount struct {
	wire.UnimplementedHostAccountServiceServer                      // 为未来兼容方法提供明确失败
	client                                     plugin.AccountClient // 已绑定实例的账号入口
}

// Current 返回不含私有协议材料的当前账号快照
func (s *hostAccount) Current(ctx context.Context, r *wire.AccountCurrentRequest) (*wire.AccountResponse, error) {
	value, err := s.client.Current(ctx)
	result, conversion := wire.ToWireAccount(value)
	return &wire.AccountResponse{Account: result, Error: hostResultError(result != nil, err, conversion, "account.current")}, nil
}

// hostResultError 保留业务失败，拒绝缺失或无法解码的成功结果
func hostResultError(present bool, primary, conversion error, operation string) *wire.Error {
	if primary != nil {
		return transport.EncodeError(primary, operation)
	}
	if conversion != nil || !present {
		return transport.EncodeError(transport.ResultError(false, nil, nil, operation), operation)
	}
	return nil
}
