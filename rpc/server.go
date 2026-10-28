package rpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Invocation 宿主反向服务边界收到的调用凭据，不从请求决定插件身份
type Invocation struct {
	ID    string // 宿主签发的执行标识，空值仅可按独立后台授权处理
	Token string // 宿主签发的本次执行凭据
}

// LifecycleOperation 是由 SDK 控制面确定的生命周期操作，不接受插件自行声明
type LifecycleOperation string

const (
	LifecycleLoad    LifecycleOperation = "plugin.load"    // 插件初始加载
	LifecycleEnable  LifecycleOperation = "plugin.enable"  // 插件启用
	LifecycleDisable LifecycleOperation = "plugin.disable" // 插件禁用
	LifecycleUnload  LifecycleOperation = "plugin.unload"  // 插件卸载
)

// HostServices 当前实例绑定的宿主实现与授权入口
type HostServices struct {
	Host *plugin.Host // 已绑定实例和账号的七类宿主业务实现
	// 检查实例、调用凭据及操作授权，未提供时默认拒绝
	Authorize    func(context.Context, Invocation, string) (context.Context, error)
	ReverseReady func(context.Context) error // 可选的反向连接就绪校验，失败会使整个启动失败
	// 可选的单次生命周期登记；ctx 保留调用方的值、取消和有效期，返回的撤销函数必须非空
	RegisterLifecycle func(ctx context.Context, invocation Invocation, operation LifecycleOperation) (revoke func(), err error)
	// 仅在 handler 实际退出后接收当前实例的独立完成确认
	ExecutionCompleted func(ctx context.Context, invocationID string, generation uint64) error
}

// hostControl 将就绪确认绑定到当前实例的随机控制凭据
type hostControl struct {
	wire.UnimplementedHostControlServiceServer // 为未来新增控制方法提供明确失败

	token        string                                                                // 当前实例唯一的随机控制凭据
	ready        chan struct{}                                                         // 反向连接就绪信号
	once         sync.Once                                                             // 保证就绪信号只关闭一次
	validate     func(context.Context) error                                           // 可选的宿主就绪校验
	register     func(context.Context, Invocation, LifecycleOperation) (func(), error) // 可选的宿主生命周期登记
	generation   uint64                                                                // 反向连接绑定的实例代次
	completed    func(context.Context, string, uint64) error                           // 宿主实际执行记录的完成入口，不能根据业务 RPC 返回自行触发
	invocationMu sync.Mutex                                                            // 保护用户调用权限，与真实执行完成记录独立
	invocations  map[string]hostInvocation                                             // 仅保存宿主仍在等待且尚未取消的用户调用
}

// bindLifecycle 先让宿主登记可绑定实例策略的单次身份，再开放 SDK 反向调用凭据
func (s *hostControl) bindLifecycle(ctx context.Context, invocation Invocation, operation LifecycleOperation) (func(), error) {
	revokeHost := func() {}
	if s.register != nil {
		var err error
		revokeHost, err = callLifecycleRegistration(s.register, ctx, invocation, operation)
		if err != nil {
			return nil, err
		}
		if revokeHost == nil {
			return nil, sdk.NewError(sdk.CategoryInternal, "plugin.lifecycle", "lifecycle registration returned no revocation")
		}
	}
	revokeLocal, err := s.bindInvocation(ctx, invocation)
	if err != nil {
		revokeHost()
		return nil, err
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			revokeLocal()
			callLifecycleRevocation(revokeHost)
		})
	}
	stop := context.AfterFunc(ctx, cleanup)
	return func() {
		stop()
		cleanup()
	}, nil
}

// callLifecycleRegistration 将宿主回调 panic 转换为可返回错误，避免后续生命周期直接带崩宿主
func callLifecycleRegistration(register func(context.Context, Invocation, LifecycleOperation) (func(), error), ctx context.Context, invocation Invocation, operation LifecycleOperation) (revoke func(), err error) {
	defer func() {
		if recover() != nil {
			revoke = nil
			err = sdk.NewError(sdk.CategoryInternal, "plugin.lifecycle", "lifecycle registration panicked")
		}
	}()
	return register(ctx, invocation, operation)
}

// callLifecycleRevocation 隔离清理回调 panic，特别是取消后由 context.AfterFunc 执行的异步路径
func callLifecycleRevocation(revoke func()) {
	defer func() { _ = recover() }()
	revoke()
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
		var invocation Invocation
		if value, ok := request.(interface{ GetInvocation() *wire.Invocation }); ok {
			invocation.ID, invocation.Token = value.GetInvocation().GetId(), value.GetInvocation().GetToken()
		}
		_, background := request.(*wire.HostBackgroundCallRequest)
		if background {
			value := request.(*wire.HostBackgroundCallRequest)
			if value.GetBackgroundToken() == "" {
				return nil, transport.RPCError(sdk.NewError(sdk.CategoryPermissionDenied, info.FullMethod, "invalid background invocation"), info.FullMethod)
			}
			ctx = transport.WithBackgroundToken(ctx, value.GetBackgroundToken())
		}
		authority := control
		if background {
			authority = nil
		}
		bound, cleanup, err := authorizeHostContext(ctx, services, authority, invocation, info.FullMethod)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		return handler(bound, request)
	}
}

// hostStreamInterceptor 在读取首包后执行与一元 RPC 相同的可信身份绑定
func hostStreamInterceptor(services HostServices, control *hostControl) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recover() != nil {
				err = status.Error(codes.Internal, "host operation panicked")
			}
		}()
		var first proto.Message
		switch info.FullMethod {
		case wire.HostMessagesService_SendMedia_FullMethodName:
			first = new(wire.SendMediaRequest)
		case wire.HostMessagesService_StoreMedia_FullMethodName:
			first = new(wire.StoreMediaRequest)
		case wire.HostMessagesService_Download_FullMethodName:
			first = new(wire.DownloadRequest)
		default:
			return status.Error(codes.PermissionDenied, "host stream is not authorized")
		}
		if err := stream.RecvMsg(first); err != nil {
			return err
		}
		var invocation Invocation
		if value, ok := first.(interface{ GetInvocation() *wire.Invocation }); ok {
			invocation.ID, invocation.Token = value.GetInvocation().GetId(), value.GetInvocation().GetToken()
		} else if request, ok := first.(*wire.SendMediaRequest); ok {
			invocation.ID = request.GetMetadata().GetInvocation().GetId()
			invocation.Token = request.GetMetadata().GetInvocation().GetToken()
		} else if request, ok := first.(*wire.StoreMediaRequest); ok {
			invocation.ID = request.GetMetadata().GetInvocation().GetId()
			invocation.Token = request.GetMetadata().GetInvocation().GetToken()
		}
		bound, cleanup, err := authorizeHostContext(stream.Context(), services, control, invocation, info.FullMethod)
		if err != nil {
			return err
		}
		defer cleanup()
		return handler(server, &replayServerStream{ServerStream: stream, ctx: bound, first: first})
	}
}

// authorizeHostContext 统一继承宿主原调用期限、授权值和取消信号
func authorizeHostContext(ctx context.Context, services HostServices, control *hostControl, invocation Invocation, method string) (context.Context, func(), error) {
	if services.Authorize == nil {
		return nil, nil, status.Error(codes.PermissionDenied, "host authorization is required")
	}
	var cleanups []func()
	cleanup := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}
	if control != nil {
		parent, err := control.invocationContext(invocation)
		if err != nil {
			return nil, nil, transport.RPCError(err, method)
		}
		if parent != nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cleanups = append(cleanups, cancel)
			stop := context.AfterFunc(parent, cancel)
			cleanups = append(cleanups, func() { stop() })
			if deadline, ok := parent.Deadline(); ok {
				ctx, cancel = context.WithDeadline(ctx, deadline)
				cleanups = append(cleanups, cancel)
			}
		}
	}
	bound, err := services.Authorize(ctx, invocation, method)
	if err != nil {
		cleanup()
		return nil, nil, transport.RPCError(err, method)
	}
	if bound == nil {
		cleanup()
		return nil, nil, status.Error(codes.Internal, "authorization returned no context")
	}
	bound, cancel := context.WithCancel(bound)
	cleanups = append(cleanups, cancel)
	stop := context.AfterFunc(ctx, cancel)
	cleanups = append(cleanups, func() { stop() })
	if deadline, ok := ctx.Deadline(); ok {
		bound, cancel = context.WithDeadline(bound, deadline)
		cleanups = append(cleanups, cancel)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, nil, transport.RPCError(err, method)
	}
	if err := bound.Err(); err != nil {
		cleanup()
		return nil, nil, transport.RPCError(err, method)
	}
	return bound, cleanup, nil
}

// replayServerStream 把鉴权时消费的首包原样交还给类型化 handler
type replayServerStream struct {
	grpc.ServerStream
	ctx   context.Context // 已绑定实例和资源范围的可信上下文
	first proto.Message   // 仅回放一次的首包
}

// Context 返回授权后仍继承调用取消与期限的上下文
func (s *replayServerStream) Context() context.Context { return s.ctx }

// RecvMsg 先回放鉴权首包，后续继续读取实际流
func (s *replayServerStream) RecvMsg(target any) error {
	if s.first != nil {
		value, ok := target.(proto.Message)
		if !ok {
			return status.Error(codes.Internal, "stream message is not protobuf")
		}
		proto.Merge(value, s.first)
		s.first = nil
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	// 底层 RecvMsg 只监听连接上下文，单独撤销业务授权无法唤醒它
	// handler 返回后 gRPC 会取消原始流，收尾这一次仍在等待的底层读取
	done := make(chan error, 1)
	go func() { done <- s.ServerStream.RecvMsg(target) }()
	select {
	case err := <-done:
		if canceled := s.ctx.Err(); canceled != nil {
			return canceled
		}
		return err
	case <-s.ctx.Done():
		return s.ctx.Err()
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

// hostMessages 消息 RPC 的业务适配
type hostMessages struct {
	wire.UnimplementedHostMessagesServiceServer // 为未来兼容方法提供明确失败

	client plugin.MessagesClient // 已绑定实例的消息能力
}

// SendMedia 校验首包和累计字节后以 io.Pipe 向宿主提供有界 Reader
func (s *hostMessages) SendMedia(stream grpc.ClientStreamingServer[wire.SendMediaRequest, wire.SendReceiptResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return transport.RPCError(err, "messages.send_media")
	}
	metadata := first.GetMetadata()
	if metadata == nil {
		return transport.RPCError(sdk.NewError(sdk.CategoryInvalidArgument, "messages.send_media", "first media frame must contain metadata"), "messages.send_media")
	}
	upload, err := mediaUploadFromWire(metadata)
	if err != nil {
		return transport.RPCError(err, "messages.send_media")
	}
	if upload.Ref != nil {
		if extra, receiveErr := stream.Recv(); !errors.Is(receiveErr, io.EOF) {
			if receiveErr == nil && extra != nil {
				receiveErr = mediaInputError("media reference stream must not contain data")
			}
			return transport.RPCError(receiveErr, "messages.send_media")
		}
		value, callErr := s.client.SendMedia(stream.Context(), wire.FromWireConversation(metadata.GetConversation()), upload)
		result, conversion := wire.ToWireSendReceipt(value)
		return stream.SendAndClose(&wire.SendReceiptResponse{
			Receipt: result,
			Error:   hostResultError(result != nil, callErr, conversion, "messages.send_media"),
		})
	}
	second, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = mediaInputError("media stream is empty")
		}
		return transport.RPCError(err, "messages.send_media")
	}
	if len(second.GetData()) == 0 || len(second.GetData()) > transport.MaxMediaChunkSize {
		return transport.RPCError(mediaInputError("invalid media chunk"), "messages.send_media")
	}
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	upload.Reader = reader
	received := make(chan error, 1)
	go receiveMedia(stream, writer, upload.Kind, upload.Size, second.GetData(), received)
	value, callErr := s.client.SendMedia(stream.Context(), wire.FromWireConversation(metadata.GetConversation()), upload)
	_ = reader.Close()
	if callErr == nil {
		if receiveErr := <-received; receiveErr != nil {
			callErr = receiveErr
		}
	} else {
		select {
		case receiveErr := <-received:
			if isMediaInputError(receiveErr) {
				callErr = receiveErr
			}
		default:
		}
	}
	result, conversion := wire.ToWireSendReceipt(value)
	return stream.SendAndClose(&wire.SendReceiptResponse{Receipt: result, Error: hostResultError(result != nil, callErr, conversion, "messages.send_media")})
}

// StoreMedia 校验首包和累计字节后将有界 Reader 交给宿主引用存储
func (s *hostMessages) StoreMedia(stream grpc.ClientStreamingServer[wire.StoreMediaRequest, wire.MediaRefResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return transport.RPCError(err, "messages.store_media")
	}
	metadata := first.GetMetadata()
	if metadata == nil {
		return transport.RPCError(mediaStoreInputError("first media frame must contain metadata"), "messages.store_media")
	}
	upload, err := storedMediaUploadFromWire(metadata)
	if err != nil {
		return transport.RPCError(err, "messages.store_media")
	}
	second, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = mediaStoreInputError("media stream is empty")
		}
		return transport.RPCError(err, "messages.store_media")
	}
	if len(second.GetData()) == 0 || len(second.GetData()) > transport.MaxMediaChunkSize {
		return transport.RPCError(mediaStoreInputError("invalid media chunk"), "messages.store_media")
	}
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	upload.Reader = reader
	received := make(chan error, 1)
	go receiveStoredMedia(stream, writer, upload.Kind, upload.Size, second.GetData(), received)
	value, callErr := s.client.StoreMedia(stream.Context(), upload)
	_ = reader.Close()
	if callErr == nil {
		if receiveErr := <-received; receiveErr != nil {
			callErr = receiveErr
		}
	}
	result, conversion := wire.ToWireMediaRef(value)
	return stream.SendAndClose(&wire.MediaRefResponse{
		Media: result,
		Error: hostResultError(result != nil, callErr, conversion, "messages.store_media"),
	})
}

// isMediaInputError 判断接收端已确认的协议错误，优先于由此引发的宿主读取失败
func isMediaInputError(err error) bool {
	var value *sdk.Error
	return errors.As(err, &value) && (value.Category == sdk.CategoryInvalidArgument || value.Category == sdk.CategoryResourceExhausted)
}

// mediaUploadFromWire 转换并验证媒体上传首包
func mediaUploadFromWire(value *wire.SendMediaMetadata) (message.MediaUpload, error) {
	upload := message.MediaUpload{
		Kind:     sdk.MediaKind(value.GetKind()),
		FileName: value.GetFileName(),
		Size:     value.GetSize(),
	}
	if value.GetRef() != nil {
		ref, err := wire.FromWireMediaRef(value.GetRef())
		if err != nil {
			return message.MediaUpload{}, err
		}
		upload.Ref = ref
		upload.Kind = ref.Kind
		upload.FileName = ref.FileName
		upload.Size = ref.SizeBytes
	}
	if voice := value.GetVoice(); voice != nil {
		if err := voice.GetDuration().CheckValid(); err != nil {
			return message.MediaUpload{}, mediaInputError("invalid voice duration")
		}
		upload.Voice = &message.VoiceOptions{Duration: voice.GetDuration().AsDuration(), Encoding: message.VoiceEncoding(voice.GetEncoding())}
	}
	// Reader 占位只用于复用同一份元数据校验，实际 pipe 在通过后注入
	if upload.Ref == nil {
		upload.Reader = emptyReader{}
	}
	if err := transport.ValidateMediaUpload(upload); err != nil {
		return message.MediaUpload{}, err
	}
	upload.Reader = nil
	return upload, nil
}

// storedMediaUploadFromWire 转换媒体暂存首包并复用上传边界
func storedMediaUploadFromWire(value *wire.StoreMediaMetadata) (message.MediaUpload, error) {
	upload := message.MediaUpload{
		Kind: sdk.MediaKind(value.GetKind()), FileName: value.GetFileName(), Size: value.GetSize(),
		Reader: emptyReader{},
	}
	if voice := value.GetVoice(); voice != nil {
		if err := voice.GetDuration().CheckValid(); err != nil {
			return message.MediaUpload{}, mediaStoreInputError("invalid voice duration")
		}
		upload.Voice = &message.VoiceOptions{
			Duration: voice.GetDuration().AsDuration(), Encoding: message.VoiceEncoding(voice.GetEncoding()),
		}
	}
	if err := transport.ValidateMediaStore(upload); err != nil {
		return message.MediaUpload{}, err
	}
	upload.Reader = nil
	return upload, nil
}

// emptyReader 只标记首包选择 Reader 上传，不参与实际读取
type emptyReader struct{}

// Read 不应在元数据校验阶段被调用
func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

// receiveMedia 按序写入 pipe，并在 EOF 时核对声明长度
func receiveMedia(stream grpc.ClientStreamingServer[wire.SendMediaRequest, wire.SendReceiptResponse], writer *io.PipeWriter, kind sdk.MediaKind, declared int64, first []byte, result chan<- error) {
	var final error
	defer func() {
		result <- final
		_ = writer.CloseWithError(final)
	}()
	var total int64
	write := func(data []byte) bool {
		if len(data) == 0 || len(data) > transport.MaxMediaChunkSize {
			final = mediaProtocolError("invalid media chunk")
			return false
		}
		total += int64(len(data))
		if final = transport.ValidateMediaTotal(kind, declared, total, false); final != nil {
			return false
		}
		if _, final = writer.Write(data); final != nil {
			return false
		}
		return true
	}
	if !write(first) {
		return
	}
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			final = transport.ValidateMediaTotal(kind, declared, total, true)
			return
		}
		if err != nil {
			final = err
			return
		}
		if request.GetMetadata() != nil || !write(request.GetData()) {
			if final == nil {
				final = mediaProtocolError("metadata may only appear in the first frame")
			}
			return
		}
	}
}

// receiveStoredMedia 按序写入媒体暂存 pipe，并在 EOF 时核对声明长度
func receiveStoredMedia(stream grpc.ClientStreamingServer[wire.StoreMediaRequest, wire.MediaRefResponse], writer *io.PipeWriter, kind sdk.MediaKind, declared int64, first []byte, result chan<- error) {
	var final error
	defer func() {
		result <- final
		_ = writer.CloseWithError(final)
	}()
	var total int64
	write := func(data []byte) bool {
		if len(data) == 0 || len(data) > transport.MaxMediaChunkSize {
			final = mediaStoreProtocolError("invalid media chunk")
			return false
		}
		total += int64(len(data))
		if final = transport.ValidateMediaStoreTotal(kind, declared, total, false); final != nil {
			return false
		}
		if _, final = writer.Write(data); final != nil {
			return false
		}
		return true
	}
	if !write(first) {
		return
	}
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			final = transport.ValidateMediaStoreTotal(kind, declared, total, true)
			return
		}
		if err != nil {
			final = err
			return
		}
		if request.GetMetadata() != nil || !write(request.GetData()) {
			if final == nil {
				final = mediaStoreProtocolError("metadata may only appear in the first frame")
			}
			return
		}
	}
}

// mediaInputError 表示尚未进入宿主业务的参数错误
func mediaInputError(reason string) *sdk.Error {
	value := sdk.NewError(sdk.CategoryInvalidArgument, "messages.send_media", reason)
	value.Outcome = sdk.OutcomeFailed
	return value
}

// mediaProtocolError 表示已经接收媒体流后发现的协议错误
func mediaProtocolError(reason string) *sdk.Error {
	value := sdk.NewError(sdk.CategoryInvalidArgument, "messages.send_media", reason)
	value.Outcome = sdk.OutcomeUnknown
	return value
}

// mediaStoreInputError 表示尚未进入宿主暂存业务的参数错误
func mediaStoreInputError(reason string) *sdk.Error {
	return sdk.NewError(sdk.CategoryInvalidArgument, "messages.store_media", reason)
}

// mediaStoreProtocolError 表示开始接收后发现的暂存流协议错误
func mediaStoreProtocolError(reason string) *sdk.Error {
	return sdk.NewError(sdk.CategoryInvalidArgument, "messages.store_media", reason)
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

// Forward 校验宿主转发引用后调用业务入口
func (s *hostMessages) Forward(ctx context.Context, r *wire.ForwardRequest) (*wire.SendReceiptResponse, error) {
	ref, err := wire.FromWireForwardRef(r.GetRef())
	if err == nil && ref == nil {
		err = sdk.NewError(sdk.CategoryInvalidArgument, "messages.forward", "missing forward reference")
	}
	var value *message.SendReceipt
	if err == nil {
		value, err = s.client.Forward(ctx, wire.FromWireConversation(r.GetConversation()), *ref)
	}
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{
		Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.forward"),
	}, nil
}

// SendLink 校验结构化链接后调用业务入口
func (s *hostMessages) SendLink(ctx context.Context, r *wire.SendLinkRequest) (*wire.SendReceiptResponse, error) {
	link := message.Link{Title: r.GetTitle(), Description: r.GetDescription(), URL: r.GetUrl()}
	err := transport.ValidateLink(link)
	var value *message.SendReceipt
	if err == nil {
		value, err = s.client.SendLink(ctx, wire.FromWireConversation(r.GetConversation()), link)
	}
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{
		Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.send_link"),
	}, nil
}

// SendLocation 校验结构化位置后调用业务入口
func (s *hostMessages) SendLocation(ctx context.Context, r *wire.SendLocationRequest) (*wire.SendReceiptResponse, error) {
	location := message.Location{
		Label: r.GetLabel(), POIName: r.GetPoiName(), Longitude: r.GetLongitude(),
		Latitude: r.GetLatitude(), Scale: r.GetScale(),
	}
	err := transport.ValidateLocation(location)
	var value *message.SendReceipt
	if err == nil {
		value, err = s.client.SendLocation(ctx, wire.FromWireConversation(r.GetConversation()), location)
	}
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{
		Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.send_location"),
	}, nil
}

// SendNameCard 校验结构化名片后调用业务入口
func (s *hostMessages) SendNameCard(ctx context.Context, r *wire.SendNameCardRequest) (*wire.SendReceiptResponse, error) {
	card := message.NameCard{Username: r.GetUsername(), Nickname: r.GetNickname(), Alias: r.GetAlias()}
	err := transport.ValidateNameCard(card)
	var value *message.SendReceipt
	if err == nil {
		value, err = s.client.SendNameCard(ctx, wire.FromWireConversation(r.GetConversation()), card)
	}
	result, conversion := wire.ToWireSendReceipt(value)
	return &wire.SendReceiptResponse{
		Receipt: result, Error: hostResultError(result != nil, err, conversion, "messages.send_name_card"),
	}, nil
}

// Download 将宿主授权的媒体 Reader 转为有界 gRPC 分块流
func (s *hostMessages) Download(r *wire.DownloadRequest, stream grpc.ServerStreamingServer[wire.MediaChunk]) error {
	ref, err := wire.FromWireMediaRef(r.GetMedia())
	if err == nil && ref == nil {
		err = sdk.NewError(sdk.CategoryInvalidArgument, "messages.download", "missing media reference")
	}
	if err != nil {
		return transport.RPCError(err, "messages.download")
	}
	reader, err := s.client.Download(stream.Context(), *ref)
	if err != nil {
		return transport.RPCError(err, "messages.download")
	}
	if reader == nil {
		return transport.RPCError(sdk.NewError(sdk.CategoryInternal, "messages.download", "missing media stream"), "messages.download")
	}
	defer func() { _ = reader.Close() }()
	buffer := make([]byte, transport.MaxMediaChunkSize)
	var total int64
	emptyReads := 0
	for {
		n, readErr := transport.ReadMediaChunk(reader, buffer, &emptyReads)
		if n > 0 {
			total += int64(n)
			if err := transport.ValidateMediaDownloadTotal(ref.Kind, ref.SizeBytes, total, false); err != nil {
				return transport.RPCError(err, "messages.download")
			}
			if err := stream.Send(&wire.MediaChunk{Data: append([]byte(nil), buffer[:n]...)}); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return transport.RPCError(
					transport.ValidateMediaDownloadTotal(ref.Kind, ref.SizeBytes, total, true),
					"messages.download",
				)
			}
			return transport.RPCError(readErr, "messages.download")
		}
	}
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

// BackgroundConversation 返回反向服务端验证前的后台目标，业务层仍须独立授权
func BackgroundConversation(ctx context.Context) (sdk.Conversation, bool) {
	return transport.BackgroundConversation(ctx)
}

// BackgroundToken 返回后台启用令牌，仅供宿主二次绑定校验
func BackgroundToken(ctx context.Context) (string, bool) {
	return transport.BackgroundToken(ctx)
}

// WithBackgroundToken 在宿主边界测试中模拟已提取的后台令牌
func WithBackgroundToken(ctx context.Context, token string) context.Context {
	return transport.WithBackgroundToken(ctx, token)
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

// CallBackground 使用无 token 独立授权，目标不进入能力业务请求
func (s *hostCaller) CallBackground(ctx context.Context, r *wire.HostBackgroundCallRequest) (*wire.HostCallResponse, error) {
	var value *capability.CallResult
	var err error
	client, ok := s.client.(interface {
		CallBackground(context.Context, sdk.Conversation, capability.CallRequest) (*capability.CallResult, error)
	})
	if ok {
		ctx = transport.WithBackgroundConversation(ctx, wire.FromWireConversation(r.GetConversation()))
		value, err = client.CallBackground(ctx, wire.FromWireConversation(r.GetConversation()), wire.FromWireCallRequest(r.GetRequest()))
	} else {
		err = sdk.NewError(sdk.CategoryUnimplemented, "caller.call", "background calls are not supported")
	}
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
