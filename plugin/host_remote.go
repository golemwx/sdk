package plugin

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// remoteHostState 是反向连接对插件侧可见的实例状态。
//
// terminal 一旦写入就不允许恢复；Stop 只关闭 active，可再次 Start。
type remoteHostState struct {
	mu       sync.RWMutex
	active   bool
	terminal *sdk.Error
	conn     *grpc.ClientConn

	closeOnce sync.Once
}

func newRemoteHostState(conn *grpc.ClientConn) *remoteHostState {
	return &remoteHostState{active: true, conn: conn}
}

func (s *remoteHostState) check(ctx context.Context, operation string) error {
	if s == nil {
		return sdk.NewError(sdk.CategoryInternal, operation, "host connection state is missing")
	}
	s.mu.RLock()
	terminal := cloneBusinessError(s.terminal)
	active := s.active
	s.mu.RUnlock()
	if terminal != nil {
		return terminal
	}
	if !active {
		return sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin host is not enabled")
	}
	if ctx == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "context is nil")
	}
	if err := ctx.Err(); err != nil {
		return transport.NormalizeError(err, operation)
	}
	return nil
}

func (s *remoteHostState) setActive(active bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal == nil {
		s.active = active
	}
}

func (s *remoteHostState) invalidate(reason *sdk.Error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.terminal == nil {
		s.terminal = cloneBusinessError(reason)
	}
	s.active = false
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		s.closeOnce.Do(func() { _ = conn.Close() })
	}
}

func newRemoteHost(state *remoteHostState) *Host {
	conn := state.conn
	host := NewHost(
		&remoteAccount{state: state, client: wire.NewHostAccountServiceClient(conn)},
		&remoteMessages{state: state, client: wire.NewHostMessagesServiceClient(conn)},
		&remoteContacts{state: state, client: wire.NewHostContactsServiceClient(conn)},
		&remoteChatrooms{state: state, client: wire.NewHostChatroomsServiceClient(conn)},
		&remoteSessions{state: state, client: wire.NewHostSessionsServiceClient(conn)},
		&remoteCaller{state: state, client: wire.NewHostCallerServiceClient(conn)},
		unsupportedData{},
	)
	host.state = state
	return host
}

// setActive 切换当前启用周期对反向 Host 的访问许可。
func (h *Host) setActive(active bool) {
	if h == nil || h.state == nil {
		return
	}
	h.state.setActive(active)
}

type remoteAccount struct {
	state  *remoteHostState
	client wire.HostAccountServiceClient
}

func (c *remoteAccount) Current(ctx context.Context) (*sdk.Account, error) {
	const operation = "account.current"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.Current(ctx, &emptypb.Empty{})
	if err := transport.DecodeError(response.GetError(), err, operation); err != nil {
		return nil, err
	}
	if response.GetAccount() == nil {
		return nil, missingResult(operation)
	}
	return wire.FromWireAccount(response.GetAccount()), nil
}

type remoteMessages struct {
	state  *remoteHostState
	client wire.HostMessagesServiceClient
}

func (c *remoteMessages) SendText(ctx context.Context, target, text string, mentions []string) (*message.SendResult, error) {
	const operation = "messages.send_text"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.SendText(ctx, &wire.SendTextRequest{
		Target: target, Text: text, Mentions: cloneStrings(mentions),
	})
	return remoteSendResult(response, err, operation)
}

func (c *remoteMessages) SendApp(ctx context.Context, target string, app message.App) (*message.SendResult, error) {
	const operation = "messages.send_app"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.SendApp(ctx, &wire.SendAppRequest{
		Target: target,
		App: &wire.AppData{
			Type: app.Type, Title: app.Title, Description: app.Description, Url: app.URL,
		},
	})
	return remoteSendResult(response, err, operation)
}

func (c *remoteMessages) SendMedia(ctx context.Context, target string, upload message.Upload) (*message.SendResult, error) {
	const operation = "messages.send_media"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	if err := validateUpload(upload); err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.client.SendMedia(streamCtx)
	if err != nil {
		return nil, transport.NormalizeError(err, operation)
	}
	metadata := &wire.SendMediaMetadata{
		Target: target,
		Type:   toWireMessageType(upload.Type),
		Name:   upload.Name,
		Size:   upload.Size,
	}
	if upload.Voice != nil {
		metadata.Voice = &wire.VoiceUpload{
			Duration: durationpb.New(upload.Voice.Duration),
			Format:   upload.Voice.Format,
		}
	}
	if err := stream.Send(&wire.SendMediaRequest{
		Value: &wire.SendMediaRequest_Metadata{Metadata: metadata},
	}); err != nil {
		return finishMediaSend(stream, err, operation)
	}

	buffer := make([]byte, transport.MaxMediaChunkBytes)
	var total int64
	for {
		n, readErr := io.ReadFull(upload.Reader, buffer)
		if n > 0 {
			total += int64(n)
			if upload.Size >= 0 && total > upload.Size {
				return nil, unknownWriteError(operation, "media reader exceeded declared size")
			}
			if err := stream.Send(&wire.SendMediaRequest{
				Value: &wire.SendMediaRequest_Data{Data: append([]byte(nil), buffer[:n]...)},
			}); err != nil {
				return finishMediaSend(stream, err, operation)
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
		if readErr != nil {
			return nil, unknownWriteError(operation, "media reader failed")
		}
	}
	if upload.Size >= 0 && total != upload.Size {
		return nil, unknownWriteError(operation, "media reader did not match declared size")
	}
	response, err := stream.CloseAndRecv()
	return remoteSendResult(response, err, operation)
}

func (c *remoteMessages) Forward(ctx context.Context, target string, source message.Message) (*message.SendResult, error) {
	const operation = "messages.forward"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	encoded, err := wire.ToWireMessage(&source)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Forward(ctx, &wire.ForwardRequest{Target: target, Source: encoded})
	return remoteSendResult(response, err, operation)
}

func (c *remoteMessages) Revoke(ctx context.Context, sent message.SendResult) error {
	const operation = "messages.revoke"
	if err := c.state.check(ctx, operation); err != nil {
		return err
	}
	encoded, err := wire.ToWireSendResult(&sent)
	if err != nil {
		return err
	}
	response, err := c.client.Revoke(ctx, &wire.RevokeRequest{Sent: encoded})
	return transport.DecodeError(response.GetError(), err, operation)
}

func (c *remoteMessages) Download(ctx context.Context, source message.Message) (io.ReadCloser, error) {
	const operation = "messages.download"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	encoded, err := wire.ToWireMessage(&source)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := c.client.Download(streamCtx, &wire.DownloadRequest{Source: encoded})
	if err != nil {
		cancel()
		return nil, transport.NormalizeError(err, operation)
	}
	reader := &remoteDownloadReader{
		stream: stream, cancel: cancel, operation: operation,
	}
	if source.Data != nil {
		if media, ok := source.Data.(interface{ mediaSize() uint64 }); ok {
			reader.expected = media.mediaSize()
			reader.hasExpected = reader.expected > 0
		}
	}
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		reader.terminal = io.EOF
		return reader, nil
	}
	if err != nil {
		cancel()
		return nil, transport.NormalizeError(err, operation)
	}
	if err := reader.accept(first); err != nil {
		_ = reader.Close()
		return nil, err
	}
	return reader, nil
}

func validateUpload(upload message.Upload) error {
	const operation = "messages.send_media"
	if upload.Reader == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "media reader is nil")
	}
	if upload.Size < -1 {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "media size must be -1 or non-negative")
	}
	switch upload.Type {
	case message.TypeImage, message.TypeVoice, message.TypeVideo, message.TypeFile:
	default:
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "unsupported media type")
	}
	if upload.Voice != nil {
		if upload.Type != message.TypeVoice {
			return sdk.NewError(sdk.CategoryInvalidArgument, operation, "voice options require a voice message")
		}
		if upload.Voice.Duration < 0 {
			return sdk.NewError(sdk.CategoryInvalidArgument, operation, "voice duration must not be negative")
		}
	}
	return nil
}

func finishMediaSend(stream grpc.ClientStreamingClient[wire.SendMediaRequest, wire.SendResultResponse], sendErr error, operation string) (*message.SendResult, error) {
	response, receiveErr := stream.CloseAndRecv()
	if receiveErr != nil {
		return nil, transport.NormalizeError(receiveErr, operation)
	}
	if response == nil {
		return nil, transport.NormalizeError(sendErr, operation)
	}
	return remoteSendResult(response, nil, operation)
}

func remoteSendResult(response *wire.SendResultResponse, callErr error, operation string) (*message.SendResult, error) {
	if err := transport.DecodeError(response.GetError(), callErr, operation); err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, missingResult(operation)
	}
	result, err := wire.FromWireSendResult(response.GetResult())
	if err != nil || result == nil {
		return nil, invalidHostResult(operation)
	}
	return result, nil
}

type remoteDownloadReader struct {
	stream    grpc.ServerStreamingClient[wire.DownloadResponse]
	cancel    context.CancelFunc
	operation string

	readMu sync.Mutex
	mu     sync.Mutex

	pending     []byte
	total       int64
	expected    uint64
	hasExpected bool
	terminal    error
	closed      bool
}

func (r *remoteDownloadReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.mu.Lock()
	if len(r.pending) > 0 {
		n := copy(destination, r.pending)
		r.pending = r.pending[n:]
		r.mu.Unlock()
		return n, nil
	}
	if r.closed {
		err := r.terminal
		if err == nil {
			err = io.ErrClosedPipe
		}
		r.mu.Unlock()
		return 0, err
	}
	if r.terminal != nil {
		err := r.terminal
		r.mu.Unlock()
		return 0, err
	}
	r.mu.Unlock()

	response, err := r.stream.Recv()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if errors.Is(err, io.EOF) {
		if r.hasExpected && uint64(r.total) != r.expected {
			r.terminal = sdk.NewError(sdk.CategoryInternal, r.operation, "download size does not match message metadata")
		} else {
			r.terminal = io.EOF
		}
		return 0, r.terminal
	}
	if err != nil {
		r.terminal = transport.NormalizeError(err, r.operation)
		return 0, r.terminal
	}
	if err := r.acceptLocked(response); err != nil {
		r.terminal = err
		return 0, err
	}
	if len(r.pending) == 0 {
		return 0, nil
	}
	n := copy(destination, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *remoteDownloadReader) accept(response *wire.DownloadResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acceptLocked(response)
}

func (r *remoteDownloadReader) acceptLocked(response *wire.DownloadResponse) error {
	if response == nil {
		return sdk.NewError(sdk.CategoryInternal, r.operation, "host returned an empty download response")
	}
	switch value := response.Value.(type) {
	case *wire.DownloadResponse_Data:
		r.pending = append(r.pending[:0], value.Data...)
		r.total += int64(len(value.Data))
		if r.hasExpected && uint64(r.total) > r.expected {
			return sdk.NewError(sdk.CategoryResourceExhausted, r.operation, "download exceeded message metadata size")
		}
		return nil
	case *wire.DownloadResponse_Error:
		err := transport.DecodeError(value.Error, nil, r.operation)
		if err == nil {
			return sdk.NewError(sdk.CategoryInternal, r.operation, "host returned an empty download error")
		}
		return err
	default:
		return sdk.NewError(sdk.CategoryInternal, r.operation, "host returned an invalid download response")
	}
}

func (r *remoteDownloadReader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.pending = nil
	r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

type remoteContacts struct {
	state  *remoteHostState
	client wire.HostContactsServiceClient
}

func (c *remoteContacts) Find(ctx context.Context, selector contact.Selector) (*contact.Contact, error) {
	const operation = "contacts.find"
	if _, _, err := selector.Split(); err != nil {
		return nil, err
	}
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.Find(ctx, &wire.ContactFindRequest{Selector: selector.String()})
	if err := transport.DecodeError(response.GetError(), err, operation); err != nil {
		return nil, err
	}
	if response.GetContact() == nil {
		return nil, missingResult(operation)
	}
	result := wire.FromWireContact(response.GetContact())
	return &result, nil
}

type remoteChatrooms struct {
	state  *remoteHostState
	client wire.HostChatroomsServiceClient
}

func (c *remoteChatrooms) Get(ctx context.Context, username string) (*chatroom.Chatroom, error) {
	const operation = "chatrooms.get"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.Get(ctx, &wire.ChatroomGetRequest{Username: username})
	if err := transport.DecodeError(response.GetError(), err, operation); err != nil {
		return nil, err
	}
	if response.GetChatroom() == nil {
		return nil, missingResult(operation)
	}
	return wire.FromWireChatroom(response.GetChatroom()), nil
}

func (c *remoteChatrooms) Members(ctx context.Context, username string) ([]chatroom.Member, error) {
	const operation = "chatrooms.members"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.Members(ctx, &wire.ChatroomMembersRequest{Username: username})
	if err := transport.DecodeError(response.GetError(), err, operation); err != nil {
		return nil, err
	}
	members := make([]chatroom.Member, len(response.GetMembers()))
	for index, member := range response.GetMembers() {
		if member == nil {
			return nil, invalidHostResult(operation)
		}
		members[index] = *wire.FromWireMember(member)
	}
	return members, nil
}

type remoteSessions struct {
	state  *remoteHostState
	client wire.HostSessionsServiceClient
}

func (c *remoteSessions) Acquire(ctx context.Context, target string, topics []event.Topic) (*session.Lease, error) {
	const operation = "sessions.acquire"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	response, err := c.client.Acquire(ctx, &wire.AcquireLeaseRequest{
		Target: target, Topics: topicsToStrings(topics),
	})
	return remoteLease(response, err, operation)
}

func (c *remoteSessions) Renew(ctx context.Context, leaseID string, ttl time.Duration) (*session.Lease, error) {
	const operation = "sessions.renew"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	if ttl < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation, "lease duration must not be negative")
	}
	response, err := c.client.Renew(ctx, &wire.RenewLeaseRequest{
		LeaseId: leaseID, Ttl: durationpb.New(ttl),
	})
	return remoteLease(response, err, operation)
}

func (c *remoteSessions) Release(ctx context.Context, leaseID string) error {
	const operation = "sessions.release"
	if err := c.state.check(ctx, operation); err != nil {
		return err
	}
	response, err := c.client.Release(ctx, &wire.ReleaseLeaseRequest{LeaseId: leaseID})
	return transport.DecodeError(response.GetError(), err, operation)
}

func remoteLease(response *wire.LeaseResponse, callErr error, operation string) (*session.Lease, error) {
	if err := transport.DecodeError(response.GetError(), callErr, operation); err != nil {
		return nil, err
	}
	if response.GetLease() == nil {
		return nil, missingResult(operation)
	}
	result, err := wire.FromWireLease(response.GetLease())
	if err != nil || result == nil {
		return nil, invalidHostResult(operation)
	}
	return result, nil
}

type remoteCaller struct {
	state  *remoteHostState
	client wire.HostCallerServiceClient
}

func (c *remoteCaller) Call(ctx context.Context, request capability.Request) (*capability.Result, error) {
	const operation = "caller.call"
	if err := c.state.check(ctx, operation); err != nil {
		return nil, err
	}
	if len(request.Payload) > capability.MaxInlineSize {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, operation, "inline payload is too large")
	}
	response, err := c.client.Call(ctx, &wire.CallCapabilityRequest{
		Request: wire.ToWireCapabilityRequest(request),
	})
	if err := transport.DecodeError(response.GetError(), err, operation); err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, missingResult(operation)
	}
	result := wire.FromWireCapabilityResult(response.GetResult())
	if len(result.Payload) > capability.MaxInlineSize {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, operation, "inline result is too large")
	}
	return &result, nil
}

type unsupportedData struct{}

func (unsupportedData) Open(string, int, fs.FileMode) (*os.File, error) {
	return nil, sdk.NewError(sdk.CategoryUnimplemented, "plugin.data.open", "plugin data is implemented by R06")
}

func (unsupportedData) ReadFile(string) ([]byte, error) {
	return nil, sdk.NewError(sdk.CategoryUnimplemented, "plugin.data.read", "plugin data is implemented by R06")
}

func (unsupportedData) WriteFile(string, []byte, fs.FileMode) error {
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.data.write", "plugin data is implemented by R06")
}

func (unsupportedData) Remove(string) error {
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.data.remove", "plugin data is implemented by R06")
}

func missingResult(operation string) error {
	return sdk.NewError(sdk.CategoryInternal, operation, "host response is missing its result")
}

func invalidHostResult(operation string) error {
	return sdk.NewError(sdk.CategoryInternal, operation, "host response is invalid")
}

func unknownWriteError(operation, reason string) error {
	value := sdk.NewError(sdk.CategoryInvalidArgument, operation, reason)
	value.Outcome = sdk.OutcomeUnknown
	return value
}

func cloneBusinessError(value *sdk.Error) *sdk.Error {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneStrings(value []string) []string {
	if value == nil {
		return nil
	}
	return append([]string{}, value...)
}

func topicsToStrings(topics []event.Topic) []string {
	result := make([]string, len(topics))
	for index, topic := range topics {
		result[index] = string(topic)
	}
	return result
}

func toWireMessageType(value message.Type) wire.MessageType {
	switch value {
	case message.TypeText:
		return wire.MessageType_MESSAGE_TYPE_TEXT
	case message.TypeImage:
		return wire.MessageType_MESSAGE_TYPE_IMAGE
	case message.TypeVoice:
		return wire.MessageType_MESSAGE_TYPE_VOICE
	case message.TypeVideo:
		return wire.MessageType_MESSAGE_TYPE_VIDEO
	case message.TypeEmoji:
		return wire.MessageType_MESSAGE_TYPE_EMOJI
	case message.TypeLocation:
		return wire.MessageType_MESSAGE_TYPE_LOCATION
	case message.TypeApp:
		return wire.MessageType_MESSAGE_TYPE_APP
	case message.TypeFile:
		return wire.MessageType_MESSAGE_TYPE_FILE
	default:
		return wire.MessageType_MESSAGE_TYPE_UNKNOWN
	}
}
