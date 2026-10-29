package plugin

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/config"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

// newRemoteHost 在唯一反向连接上构造七类类型化客户端
func newRemoteHost(conn *grpc.ClientConn, dataDir string) *Host {
	host := NewHost(
		&remoteMessages{wire.NewHostMessagesServiceClient(conn)},
		&remoteContacts{wire.NewHostContactsServiceClient(conn)},
		&remoteChatrooms{wire.NewHostChatroomsServiceClient(conn)},
		&remoteSessions{wire.NewHostSessionsServiceClient(conn)},
		&remoteCaller{client: wire.NewHostCallerServiceClient(conn)},
		&remoteConfig{wire.NewHostConfigServiceClient(conn)},
		&remoteAccount{wire.NewHostAccountServiceClient(conn)}, dataDir, nil,
	)
	host.Caller = &remoteCaller{client: wire.NewHostCallerServiceClient(conn), host: host}
	return host
}

// remoteMessages 复用反向连接的消息客户端
type remoteMessages struct {
	client wire.HostMessagesServiceClient // 消息服务传输入口
}

// SendText 保留调用上下文并发送文本
func (c *remoteMessages) SendText(ctx context.Context, conversation sdk.Conversation, text string, mentions []string) (*message.SendReceipt, error) {
	response, err := c.client.SendText(ctx, &wire.SendTextRequest{Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation), Text: text, Mentions: mentions})
	return receiptResult(response, err, "messages.send_text")
}

// Reply 保留调用上下文并发送引用回复
func (c *remoteMessages) Reply(ctx context.Context, conversation sdk.Conversation, quote message.Quote, text string) (*message.SendReceipt, error) {
	response, err := c.client.Reply(ctx, &wire.ReplyRequest{Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation), Quote: &wire.Quote{MessageId: quote.MessageID, ActorUsername: quote.ActorUsername, Digest: quote.Digest}, Text: text})
	return receiptResult(response, err, "messages.reply")
}

// receiptResult 保留伴随业务错误的有效回执，损坏回执返回 Internal 和 OutcomeUnknown
func receiptResult(response *wire.SendReceiptResponse, callErr error, operation string) (*message.SendReceipt, error) {
	result, err := wire.FromWireSendReceipt(response.GetReceipt())
	if err != nil {
		return nil, transport.ResultError(false, nil, nil, operation)
	}
	return result, transport.ResultError(result != nil, response.GetError(), callErr, operation)
}

// Revoke 使用宿主签发的撤回引用
func (c *remoteMessages) Revoke(ctx context.Context, ref sdk.RevokeRef) error {
	if err := transport.ValidateRevokeRef(ref); err != nil {
		return err
	}
	value, err := wire.ToWireRevokeRef(&ref)
	if err != nil {
		return err
	}
	response, err := c.client.Revoke(ctx, &wire.RevokeRequest{Invocation: transport.InvocationFromContext(ctx), Ref: value})
	return transport.DecodeError(response.GetError(), err, "messages.revoke")
}

// SendMedia 使用首包元数据和有界分块上传插件已经打开的媒体字节
func (c *remoteMessages) SendMedia(ctx context.Context, conversation sdk.Conversation, upload message.MediaUpload) (*message.SendReceipt, error) {
	if err := transport.ValidateMediaUpload(upload); err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.client.SendMedia(streamCtx)
	if err != nil {
		return nil, transport.DecodeError(nil, err, "messages.send_media")
	}
	metadata := &wire.SendMediaMetadata{
		Invocation:   transport.InvocationFromContext(ctx),
		Conversation: wire.ToWireConversation(conversation),
		Kind:         wire.MediaKind(upload.Kind),
		FileName:     upload.FileName,
		Size:         upload.Size,
	}
	if upload.Ref != nil {
		metadata.Ref, err = wire.ToWireMediaRef(upload.Ref)
		if err != nil {
			return nil, err
		}
		metadata.Kind = wire.MediaKind(upload.Ref.Kind)
		metadata.FileName = upload.Ref.FileName
		metadata.Size = upload.Ref.SizeBytes
	}
	if upload.Voice != nil {
		metadata.Voice = &wire.VoiceOptions{
			Duration: durationpb.New(upload.Voice.Duration),
			Encoding: wire.VoiceEncoding(upload.Voice.Encoding),
		}
	}
	if err := stream.Send(&wire.SendMediaRequest{Value: &wire.SendMediaRequest_Metadata{Metadata: metadata}}); err != nil {
		return finishMediaSend(stream, err, "messages.send_media")
	}
	if upload.Ref != nil {
		response, err := stream.CloseAndRecv()
		return receiptResult(response, err, "messages.send_media")
	}
	buffer := make([]byte, transport.MaxMediaChunkSize)
	var total int64
	emptyReads := 0
	for {
		if err := streamCtx.Err(); err != nil {
			return nil, transport.DecodeError(nil, err, "messages.send_media")
		}
		n, readErr := transport.ReadMediaChunk(upload.Reader, buffer, &emptyReads)
		if err := streamCtx.Err(); err != nil {
			return nil, transport.DecodeError(nil, err, "messages.send_media")
		}
		if n > 0 {
			total += int64(n)
			if err := transport.ValidateMediaTotal(upload.Kind, upload.Size, total, false); err != nil {
				return nil, err
			}
			if err := stream.Send(&wire.SendMediaRequest{Value: &wire.SendMediaRequest_Data{Data: buffer[:n]}}); err != nil {
				return finishMediaSend(stream, err, "messages.send_media")
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				value := sdk.NewError(sdk.CategoryInternal, "messages.send_media", "media reader failed")
				value.Outcome = sdk.OutcomeUnknown
				return nil, value
			}
			break
		}
		if err := streamCtx.Err(); err != nil {
			return nil, transport.DecodeError(nil, err, "messages.send_media")
		}
	}
	if err := transport.ValidateMediaTotal(upload.Kind, upload.Size, total, true); err != nil {
		return nil, err
	}
	response, err := stream.CloseAndRecv()
	return receiptResult(response, err, "messages.send_media")
}

// StoreMedia 暂存插件打开的有界媒体流并取得宿主签发的引用
func (c *remoteMessages) StoreMedia(ctx context.Context, upload message.MediaUpload) (*sdk.MediaRef, error) {
	if err := transport.ValidateMediaStore(upload); err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.client.StoreMedia(streamCtx)
	if err != nil {
		return nil, transport.DecodeError(nil, err, "messages.store_media")
	}
	metadata := &wire.StoreMediaMetadata{
		Invocation: transport.InvocationFromContext(ctx),
		Kind:       wire.MediaKind(upload.Kind), FileName: upload.FileName, Size: upload.Size,
	}
	if upload.Voice != nil {
		metadata.Voice = &wire.VoiceOptions{
			Duration: durationpb.New(upload.Voice.Duration),
			Encoding: wire.VoiceEncoding(upload.Voice.Encoding),
		}
	}
	if err := stream.Send(&wire.StoreMediaRequest{Value: &wire.StoreMediaRequest_Metadata{Metadata: metadata}}); err != nil {
		return finishMediaStore(stream, err)
	}
	buffer := make([]byte, transport.MaxMediaChunkSize)
	var total int64
	emptyReads := 0
	for {
		if err := streamCtx.Err(); err != nil {
			return nil, transport.DecodeError(nil, err, "messages.store_media")
		}
		n, readErr := transport.ReadMediaChunk(upload.Reader, buffer, &emptyReads)
		if err := streamCtx.Err(); err != nil {
			return nil, transport.DecodeError(nil, err, "messages.store_media")
		}
		if n > 0 {
			total += int64(n)
			if err := transport.ValidateMediaStoreTotal(upload.Kind, upload.Size, total, false); err != nil {
				return nil, err
			}
			if err := stream.Send(&wire.StoreMediaRequest{Value: &wire.StoreMediaRequest_Data{Data: buffer[:n]}}); err != nil {
				return finishMediaStore(stream, err)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, sdk.NewError(sdk.CategoryInternal, "messages.store_media", "media reader failed")
			}
			break
		}
	}
	if err := transport.ValidateMediaStoreTotal(upload.Kind, upload.Size, total, true); err != nil {
		return nil, err
	}
	response, err := stream.CloseAndRecv()
	return mediaRefResult(response, err)
}

// finishMediaStore 读取暂存流服务端的明确业务结果
func finishMediaStore(stream grpc.ClientStreamingClient[wire.StoreMediaRequest, wire.MediaRefResponse], sendErr error) (*sdk.MediaRef, error) {
	response, err := stream.CloseAndRecv()
	if err != nil {
		return nil, transport.DecodeError(nil, err, "messages.store_media")
	}
	if response == nil {
		return nil, transport.DecodeError(nil, sendErr, "messages.store_media")
	}
	return mediaRefResult(response, nil)
}

// mediaRefResult 校验宿主签发的媒体引用
func mediaRefResult(response *wire.MediaRefResponse, callErr error) (*sdk.MediaRef, error) {
	result, err := wire.FromWireMediaRef(response.GetMedia())
	if err != nil {
		return nil, transport.ResultError(false, nil, nil, "messages.store_media")
	}
	return result, transport.ResultError(result != nil, response.GetError(), callErr, "messages.store_media")
}

// finishMediaSend 在发送失败后读取服务端最终状态，避免把明确业务拒绝降级为未知传输错误
func finishMediaSend(stream grpc.ClientStreamingClient[wire.SendMediaRequest, wire.SendReceiptResponse], sendErr error, operation string) (*message.SendReceipt, error) {
	response, err := stream.CloseAndRecv()
	if err != nil {
		return nil, transport.DecodeError(nil, err, operation)
	}
	if response == nil {
		return nil, transport.DecodeError(nil, sendErr, operation)
	}
	return receiptResult(response, nil, operation)
}

// Download 预取首块以尽早返回引用错误，并把后续分块包装成有界读取流
func (c *remoteMessages) Download(ctx context.Context, media sdk.MediaRef) (io.ReadCloser, error) {
	value, err := wire.ToWireMediaRef(&media)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := c.client.Download(streamCtx, &wire.DownloadRequest{
		Invocation: transport.InvocationFromContext(ctx), Media: value,
	})
	if err != nil {
		cancel()
		return nil, transport.DecodeError(nil, err, "messages.download")
	}
	first, err := stream.Recv()
	if err != nil {
		cancel()
		return nil, transport.DecodeError(nil, err, "messages.download")
	}
	reader := &remoteMediaReader{
		ctx: ctx, stream: stream, cancel: cancel, pending: append([]byte(nil), first.GetData()...),
		expected: media.SizeBytes, limit: media.SizeBytes,
	}
	if sizeLimit, ok := transport.MediaSizeLimit(media.Kind); ok {
		reader.limit = sizeLimit
	}
	if err := reader.validateChunk(reader.pending); err != nil {
		_ = reader.Close()
		return nil, err
	}
	return reader, nil
}

// Forward 使用宿主签发的源消息引用转发，不接收原始协议内容
func (c *remoteMessages) Forward(ctx context.Context, conversation sdk.Conversation, ref sdk.ForwardRef) (*message.SendReceipt, error) {
	if err := transport.ValidateForwardRef(ref); err != nil {
		return nil, err
	}
	value, err := wire.ToWireForwardRef(&ref)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Forward(ctx, &wire.ForwardRequest{
		Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation), Ref: value,
	})
	return receiptResult(response, err, "messages.forward")
}

// SendLink 发送结构化链接，URL 只作为消息内容传递
func (c *remoteMessages) SendLink(ctx context.Context, conversation sdk.Conversation, link message.Link) (*message.SendReceipt, error) {
	if err := transport.ValidateLink(link); err != nil {
		return nil, err
	}
	response, err := c.client.SendLink(ctx, &wire.SendLinkRequest{
		Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation),
		Title: link.Title, Description: link.Description, Url: link.URL,
	})
	return receiptResult(response, err, "messages.send_link")
}

// SendLocation 发送结构化位置
func (c *remoteMessages) SendLocation(ctx context.Context, conversation sdk.Conversation, location message.Location) (*message.SendReceipt, error) {
	if err := transport.ValidateLocation(location); err != nil {
		return nil, err
	}
	response, err := c.client.SendLocation(ctx, &wire.SendLocationRequest{
		Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation),
		Label: location.Label, PoiName: location.POIName, Longitude: location.Longitude,
		Latitude: location.Latitude, Scale: location.Scale,
	})
	return receiptResult(response, err, "messages.send_location")
}

// SendNameCard 发送结构化联系人名片
func (c *remoteMessages) SendNameCard(ctx context.Context, conversation sdk.Conversation, card message.NameCard) (*message.SendReceipt, error) {
	if err := transport.ValidateNameCard(card); err != nil {
		return nil, err
	}
	response, err := c.client.SendNameCard(ctx, &wire.SendNameCardRequest{
		Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation),
		Username: card.Username, Nickname: card.Nickname, Alias: card.Alias,
	})
	return receiptResult(response, err, "messages.send_name_card")
}

// remoteMediaReader 将 gRPC 分块流包装成可取消和可关闭的有界 Reader
type remoteMediaReader struct {
	ctx      context.Context                             // 调用方上下文
	stream   grpc.ServerStreamingClient[wire.MediaChunk] // 宿主下载流
	cancel   context.CancelFunc                          // Close 立即取消底层传输
	pending  []byte                                      // 预取的首块或上次未读完的块
	total    int64                                       // 已接收总字节数
	expected int64                                       // 引用声明字节数
	limit    int64                                       // 本地总量上限
	terminal error                                       // 已确认的终态
	mu       sync.Mutex                                  // 保护状态字段
	readMu   sync.Mutex                                  // 串行化底层 Recv
	closed   bool                                        // 是否已经关闭
	once     sync.Once                                   // 取消只执行一次
}

// Read 逐块读取媒体，并核对单块、总量和声明长度
func (r *remoteMediaReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.mu.Lock()
	if r.closed {
		err := r.terminal
		if err == nil {
			err = io.ErrClosedPipe
		}
		r.mu.Unlock()
		return 0, err
	}
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			r.terminal = transport.DecodeError(nil, err, "messages.download")
			err = r.terminal
			r.mu.Unlock()
			return 0, err
		}
	}
	if len(r.pending) != 0 {
		n := copy(destination, r.pending)
		r.pending = r.pending[n:]
		r.mu.Unlock()
		return n, nil
	}
	if r.terminal != nil {
		err := r.terminal
		r.mu.Unlock()
		return 0, err
	}
	r.mu.Unlock()

	chunk, err := r.stream.Recv()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		if r.terminal != nil {
			return 0, r.terminal
		}
		return 0, io.ErrClosedPipe
	}
	if errors.Is(err, io.EOF) {
		if r.expected >= 0 && r.total != r.expected {
			r.terminal = sdk.NewError(sdk.CategoryInternal, "messages.download", "download size does not match reference")
		} else {
			r.terminal = io.EOF
		}
		return 0, r.terminal
	}
	if err != nil {
		r.terminal = transport.DecodeError(nil, err, "messages.download")
		return 0, r.terminal
	}
	r.pending = append(r.pending[:0], chunk.GetData()...)
	if err := r.validateChunkLocked(r.pending); err != nil {
		r.terminal = err
		r.pending = nil
		return 0, err
	}
	n := copy(destination, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// validateChunk 在交给插件前累计并校验宿主分块
func (r *remoteMediaReader) validateChunk(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validateChunkLocked(data)
}

// validateChunkLocked 在持有状态锁时校验宿主分块
func (r *remoteMediaReader) validateChunkLocked(data []byte) error {
	if len(data) == 0 || len(data) > transport.MaxMediaChunkSize {
		return sdk.NewError(sdk.CategoryInternal, "messages.download", "invalid media chunk")
	}
	r.total += int64(len(data))
	if r.limit > 0 && r.total > r.limit || r.expected >= 0 && r.total > r.expected {
		return sdk.NewError(sdk.CategoryResourceExhausted, "messages.download", "download exceeds media reference")
	}
	return nil
}

// Close 取消底层下载并释放连接额度
func (r *remoteMediaReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.pending = nil
	if r.terminal == nil {
		r.terminal = io.ErrClosedPipe
	}
	r.mu.Unlock()
	if r.cancel != nil {
		r.once.Do(r.cancel)
	}
	return nil
}

// remoteContacts 复用反向连接的联系人客户端
type remoteContacts struct {
	client wire.HostContactsServiceClient // 联系人传输入口
}

// Get 查询指定本地联系人
func (c *remoteContacts) Get(ctx context.Context, username string) (*contact.Contact, error) {
	response, err := c.client.Get(ctx, &wire.ContactGetRequest{Invocation: transport.InvocationFromContext(ctx), Username: username})
	if err := transport.ResultError(response.GetContact() != nil, response.GetError(), err, "contacts.get"); err != nil {
		return nil, err
	}
	return wire.FromWireContact(response.GetContact())
}

// Find 按字面子串分页查询本地联系人
func (c *remoteContacts) Find(ctx context.Context, query string, page sdk.PageRequest) (*sdk.Page[contact.Contact], error) {
	value, err := wire.ToWirePageRequest(page)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Find(ctx, &wire.ContactFindRequest{Invocation: transport.InvocationFromContext(ctx), Query: query, Page: value})
	result, decodeErr := wire.FromWireContactPage(response.GetPage())
	return result, firstError(transport.ResultError(response.GetPage() != nil, response.GetError(), err, "contacts.find"), decodeErr)
}

// List 分页列出本地联系人
func (c *remoteContacts) List(ctx context.Context, page sdk.PageRequest) (*sdk.Page[contact.Contact], error) {
	value, err := wire.ToWirePageRequest(page)
	if err != nil {
		return nil, err
	}
	response, err := c.client.List(ctx, &wire.ContactListRequest{Invocation: transport.InvocationFromContext(ctx), Page: value})
	result, decodeErr := wire.FromWireContactPage(response.GetPage())
	return result, firstError(transport.ResultError(response.GetPage() != nil, response.GetError(), err, "contacts.list"), decodeErr)
}

// Refresh 保留部分成功时的逐项结果及整体错误
func (c *remoteContacts) Refresh(ctx context.Context, usernames []string) (*sdk.BatchResult[contact.Contact], error) {
	response, err := c.client.Refresh(ctx, &wire.ContactRefreshRequest{Invocation: transport.InvocationFromContext(ctx), Usernames: usernames})
	result, decodeErr := wire.FromWireContactBatchResult(response.GetResult())
	return result, firstError(transport.ResultError(response.GetResult() != nil, response.GetError(), err, "contacts.refresh"), decodeErr)
}

// remoteChatrooms 复用反向连接的群客户端
type remoteChatrooms struct {
	client wire.HostChatroomsServiceClient // 群服务传输入口
}

// Get 查询本地群资料
func (c *remoteChatrooms) Get(ctx context.Context, username string) (*chatroom.Room, error) {
	response, err := c.client.Get(ctx, &wire.ChatroomGetRequest{Invocation: transport.InvocationFromContext(ctx), RoomUsername: username})
	if err := transport.ResultError(response.GetRoom() != nil, response.GetError(), err, "chatrooms.get"); err != nil {
		return nil, err
	}
	return wire.FromWireRoom(response.GetRoom())
}

// Members 分页查询本地群成员
func (c *remoteChatrooms) Members(ctx context.Context, username string, page sdk.PageRequest) (*sdk.Page[chatroom.Member], error) {
	value, err := wire.ToWirePageRequest(page)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Members(ctx, &wire.ChatroomMembersRequest{Invocation: transport.InvocationFromContext(ctx), RoomUsername: username, Page: value})
	result, decodeErr := wire.FromWireMemberPage(response.GetPage())
	return result, firstError(transport.ResultError(response.GetPage() != nil, response.GetError(), err, "chatrooms.members"), decodeErr)
}

// Member 查询指定群成员
func (c *remoteChatrooms) Member(ctx context.Context, room, user string) (*chatroom.Member, error) {
	response, err := c.client.Member(ctx, &wire.ChatroomMemberRequest{Invocation: transport.InvocationFromContext(ctx), RoomUsername: room, UserUsername: user})
	if err := transport.ResultError(response.GetMember() != nil, response.GetError(), err, "chatrooms.member"); err != nil {
		return nil, err
	}
	return wire.FromWireMember(response.GetMember())
}

// Refresh 保留分阶段刷新结果及整体错误
func (c *remoteChatrooms) Refresh(ctx context.Context, username string) (*chatroom.ChatroomRefreshResult, error) {
	response, err := c.client.Refresh(ctx, &wire.ChatroomRefreshRequest{Invocation: transport.InvocationFromContext(ctx), RoomUsername: username})
	result, decodeErr := wire.FromWireChatroomRefreshResult(response.GetResult())
	return result, firstError(transport.ResultError(response.GetResult() != nil, response.GetError(), err, "chatrooms.refresh"), decodeErr)
}

// remoteSessions 复用反向连接的会话租约客户端
type remoteSessions struct {
	client wire.HostSessionsServiceClient // 会话租约传输入口
}

// Acquire 为联系人或整群申请租约
func (c *remoteSessions) Acquire(ctx context.Context, conversation sdk.Conversation, topics []string) (*session.Lease, error) {
	response, err := c.client.Acquire(ctx, &wire.SessionAcquireRequest{Invocation: transport.InvocationFromContext(ctx), Conversation: wire.ToWireConversation(conversation), Topics: topics})
	if err := transport.ResultError(response.GetLease() != nil, response.GetError(), err, "sessions.acquire"); err != nil {
		return nil, err
	}
	return wire.FromWireLease(response.GetLease())
}

// Renew 续期当前实例持有的租约
func (c *remoteSessions) Renew(ctx context.Context, id string, ttl time.Duration) (*session.Lease, error) {
	response, err := c.client.Renew(ctx, &wire.SessionRenewRequest{Invocation: transport.InvocationFromContext(ctx), LeaseId: id, Ttl: durationpb.New(ttl)})
	if err := transport.ResultError(response.GetLease() != nil, response.GetError(), err, "sessions.renew"); err != nil {
		return nil, err
	}
	return wire.FromWireLease(response.GetLease())
}

// Release 释放当前实例持有的租约
func (c *remoteSessions) Release(ctx context.Context, id string) error {
	response, err := c.client.Release(ctx, &wire.SessionReleaseRequest{Invocation: transport.InvocationFromContext(ctx), LeaseId: id})
	return transport.DecodeError(response.GetError(), err, "sessions.release")
}

// remoteCaller 复用反向连接的能力调用客户端
type remoteCaller struct {
	client wire.HostCallerServiceClient // 能力调用传输入口
	host   *Host                        // 当前实例的后台启用令牌所有者
}

// Call 按能力名调用，不接受插件目标身份
func (c *remoteCaller) Call(ctx context.Context, request capability.CallRequest) (*capability.CallResult, error) {
	invocation := transport.InvocationFromContext(ctx)
	if invocation.GetId() == "" {
		target, exists := capability.BackgroundTarget(ctx)
		if !exists {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "caller.call", "background target is required")
		}
		if c.host == nil {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "caller.call", "background host is unavailable")
		}
		backgroundToken, ok := c.host.backgroundToken(ctx)
		if !ok {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "caller.call", "background context does not belong to the active enable cycle")
		}
		if len(request.Payload) > transport.MaxInlinePayloadSize {
			return nil, sdk.NewError(sdk.CategoryResourceExhausted, "caller.call", "inline payload is too large")
		}
		response, err := c.client.CallBackground(ctx, &wire.HostBackgroundCallRequest{BackgroundToken: backgroundToken, Conversation: wire.ToWireConversation(target), Request: wire.ToWireCallRequest(request)})
		result, decodeErr := wire.FromWireCallResult(response.GetResult())
		if response.GetResult() == nil || decodeErr != nil {
			return nil, firstError(transport.ResultError(false, response.GetError(), err, "caller.call"), decodeErr)
		}
		return &result, firstError(transport.ResultError(response.GetResult() != nil, response.GetError(), err, "caller.call"), decodeErr)
	}
	if invocation.GetToken() == "" {
		return nil, sdk.NewError(sdk.CategoryPermissionDenied, "caller.call", "invocation token is required")
	}
	if len(request.Payload) > transport.MaxInlinePayloadSize {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, "caller.call", "inline payload is too large")
	}
	response, err := c.client.Call(ctx, &wire.HostCallRequest{Invocation: invocation, Request: wire.ToWireCallRequest(request)})
	result, decodeErr := wire.FromWireCallResult(response.GetResult())
	if response.GetResult() == nil || decodeErr != nil {
		return nil, firstError(transport.ResultError(false, response.GetError(), err, "caller.call"), decodeErr)
	}
	return &result, firstError(transport.ResultError(response.GetResult() != nil, response.GetError(), err, "caller.call"), decodeErr)
}

// remoteConfig 复用反向连接的自身配置客户端
type remoteConfig struct {
	client wire.HostConfigServiceClient // 自身配置传输入口
}

// Snapshot 读取插件自身业务配置快照
func (c *remoteConfig) Snapshot(ctx context.Context) (*config.ConfigSnapshot, error) {
	response, err := c.client.Snapshot(ctx, &wire.ConfigSnapshotRequest{Invocation: transport.InvocationFromContext(ctx)})
	if err := transport.ResultError(response.GetSnapshot() != nil, response.GetError(), err, "config.snapshot"); err != nil {
		return nil, err
	}
	return wire.FromWireConfigSnapshot(response.GetSnapshot()), nil
}

// Save 以预期修订号保存业务配置
func (c *remoteConfig) Save(ctx context.Context, revision uint64, data []byte) (*config.ConfigSaveResult, error) {
	request := &wire.ConfigSaveRequest{Invocation: transport.InvocationFromContext(ctx), ExpectedRevision: revision}
	if data != nil {
		request.Data = &wire.BytesValue{Value: data}
	}
	response, err := c.client.Save(ctx, request)
	if err := transport.ResultError(response.GetResult() != nil, response.GetError(), err, "config.save"); err != nil {
		return nil, err
	}
	return &config.ConfigSaveResult{Revision: response.Result.Revision}, nil
}

// remoteAccount 复用反向连接的账号客户端
type remoteAccount struct {
	client wire.HostAccountServiceClient // 当前账号传输入口
}

// Current 读取当前账号业务快照
func (c *remoteAccount) Current(ctx context.Context) (*sdk.Account, error) {
	response, err := c.client.Current(ctx, &wire.AccountCurrentRequest{Invocation: transport.InvocationFromContext(ctx)})
	if err := transport.ResultError(response.GetAccount() != nil, response.GetError(), err, "account.current"); err != nil {
		return nil, err
	}
	return wire.FromWireAccount(response.GetAccount())
}

// firstError 在保留部分结果的同时优先返回原业务或传输错误
func firstError(primary, conversion error) error {
	if primary != nil {
		return primary
	}
	return conversion
}
