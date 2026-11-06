package plugin

import (
	"context"
	"errors"
	"io"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// hostServer 将 frame 的公开 Host 小接口适配为反向 gRPC 服务。
type hostServer struct {
	wire.UnimplementedHostAccountServiceServer
	wire.UnimplementedHostMessagesServiceServer
	wire.UnimplementedHostContactsServiceServer
	wire.UnimplementedHostChatroomsServiceServer
	wire.UnimplementedHostSessionsServiceServer
	wire.UnimplementedHostCallerServiceServer
	wire.UnimplementedHostConfigServiceServer

	host   *Host
	config ConfigClient
}

func registerHostServer(server *grpc.Server, host *Host, config ConfigClient) {
	implementation := &hostServer{host: host, config: config}
	wire.RegisterHostAccountServiceServer(server, implementation)
	wire.RegisterHostMessagesServiceServer(server, implementation)
	wire.RegisterHostContactsServiceServer(server, implementation)
	wire.RegisterHostChatroomsServiceServer(server, implementation)
	wire.RegisterHostSessionsServiceServer(server, implementation)
	wire.RegisterHostCallerServiceServer(server, implementation)
	wire.RegisterHostConfigServiceServer(server, implementation)
}

func (s *hostServer) Current(ctx context.Context, _ *emptypb.Empty) (*wire.AccountResponse, error) {
	const operation = "account.current"
	response := &wire.AccountResponse{}
	if s.host == nil || s.host.Account == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "account capability"), operation)
		return response, nil
	}
	account, err := s.host.Account.Current(ctx)
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	if account == nil {
		response.Error = transport.EncodeError(missingHostResult(operation), operation)
		return response, nil
	}
	response.Account = wire.ToWireAccount(account)
	return response, nil
}

func (s *hostServer) SendText(ctx context.Context, request *wire.SendTextRequest) (*wire.SendResultResponse, error) {
	const operation = "messages.send_text"
	if s.host == nil || s.host.Messages == nil {
		return sendErrorResponse(unimplementedHost(operation, "message capability"), operation), nil
	}
	if request == nil {
		return sendErrorResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "send request is missing"), operation), nil
	}
	result, err := s.host.Messages.SendText(ctx, request.GetTarget(), request.GetText(), append([]string(nil), request.GetMentions()...))
	return sendResultResponse(result, err, operation), nil
}

func (s *hostServer) SendApp(ctx context.Context, request *wire.SendAppRequest) (*wire.SendResultResponse, error) {
	const operation = "messages.send_app"
	if s.host == nil || s.host.Messages == nil {
		return sendErrorResponse(unimplementedHost(operation, "message capability"), operation), nil
	}
	if request == nil || request.GetApp() == nil {
		return sendErrorResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "send request is missing"), operation), nil
	}
	result, err := s.host.Messages.SendApp(ctx, request.GetTarget(), message.App{
		Type:        request.GetApp().GetType(),
		Title:       request.GetApp().GetTitle(),
		Description: request.GetApp().GetDescription(),
		URL:         request.GetApp().GetUrl(),
	})
	return sendResultResponse(result, err, operation), nil
}

func (s *hostServer) SendMedia(stream grpc.ClientStreamingServer[wire.SendMediaRequest, wire.SendResultResponse]) error {
	const operation = "messages.send_media"
	response := &wire.SendResultResponse{}
	if s.host == nil || s.host.Messages == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "message capability"), operation)
		return stream.SendAndClose(response)
	}
	first, err := stream.Recv()
	if err != nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "media metadata is missing"), operation)
		return stream.SendAndClose(response)
	}
	metadata := first.GetMetadata()
	if metadata == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "media metadata must be the first chunk"), operation)
		return stream.SendAndClose(response)
	}
	upload := message.Upload{
		Type:   fromWireMessageType(metadata.GetType()),
		Name:   metadata.GetName(),
		Size:   metadata.GetSize(),
		Reader: &serverMediaReader{stream: stream, declared: metadata.GetSize(), operation: operation},
	}
	if metadata.GetVoice() != nil {
		upload.Voice = &message.VoiceUpload{
			Duration: metadata.GetVoice().GetDuration().AsDuration(),
			Format:   metadata.GetVoice().GetFormat(),
		}
	}
	result, callErr := s.host.Messages.SendMedia(stream.Context(), metadata.GetTarget(), upload)
	reader := upload.Reader.(*serverMediaReader)
	if callErr == nil {
		if err := reader.drain(); err != nil {
			callErr = err
		}
	}
	return stream.SendAndClose(sendResultResponse(result, callErr, operation))
}

func (s *hostServer) Forward(ctx context.Context, request *wire.ForwardRequest) (*wire.SendResultResponse, error) {
	const operation = "messages.forward"
	if s.host == nil || s.host.Messages == nil {
		return sendErrorResponse(unimplementedHost(operation, "message capability"), operation), nil
	}
	if request == nil || request.GetSource() == nil {
		return sendErrorResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "source message is missing"), operation), nil
	}
	source, err := wire.FromWireMessage(request.GetSource())
	if err != nil {
		return sendErrorResponse(err, operation), nil
	}
	result, err := s.host.Messages.Forward(ctx, request.GetTarget(), *source)
	return sendResultResponse(result, err, operation), nil
}

func (s *hostServer) Revoke(ctx context.Context, request *wire.RevokeRequest) (*wire.OperationResponse, error) {
	const operation = "messages.revoke"
	if s.host == nil || s.host.Messages == nil {
		return operationResponse(unimplementedHost(operation, "message capability"), operation), nil
	}
	if request == nil || request.GetSent() == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "sent result is missing"), operation), nil
	}
	sent, err := wire.FromWireSendResult(request.GetSent())
	if err != nil {
		return operationResponse(err, operation), nil
	}
	if sent == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "sent result is missing"), operation), nil
	}
	return operationResponse(s.host.Messages.Revoke(ctx, *sent), operation), nil
}

func (s *hostServer) Download(request *wire.DownloadRequest, stream grpc.ServerStreamingServer[wire.DownloadResponse]) error {
	const operation = "messages.download"
	if s.host == nil || s.host.Messages == nil {
		return sendDownloadError(stream, unimplementedHost(operation, "message capability"), operation)
	}
	if request == nil || request.GetSource() == nil {
		return sendDownloadError(stream, sdk.NewError(sdk.CategoryInvalidArgument, operation, "source message is missing"), operation)
	}
	source, err := wire.FromWireMessage(request.GetSource())
	if err != nil {
		return sendDownloadError(stream, err, operation)
	}
	reader, err := s.host.Messages.Download(stream.Context(), *source)
	if err != nil {
		return sendDownloadError(stream, err, operation)
	}
	if reader == nil {
		return sendDownloadError(stream, missingHostResult(operation), operation)
	}
	defer func() { _ = reader.Close() }()

	buffer := make([]byte, transport.MaxMediaChunkBytes)
	for {
		count, readErr := io.ReadFull(reader, buffer)
		if count > 0 {
			if err := stream.Send(&wire.DownloadResponse{Value: &wire.DownloadResponse_Data{
				Data: append([]byte(nil), buffer[:count]...),
			}}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			return nil
		}
		if readErr != nil {
			return sendDownloadError(stream, readErr, operation)
		}
	}
}

func (s *hostServer) Find(ctx context.Context, request *wire.ContactFindRequest) (*wire.ContactResponse, error) {
	const operation = "contacts.find"
	response := &wire.ContactResponse{}
	if s.host == nil || s.host.Contacts == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "contact capability"), operation)
		return response, nil
	}
	if request == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "selector is missing"), operation)
		return response, nil
	}
	selector := contact.Selector(request.GetSelector())
	if _, _, err := selector.Split(); err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	found, err := s.host.Contacts.Find(ctx, selector)
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	if found == nil {
		response.Error = transport.EncodeError(missingHostResult(operation), operation)
		return response, nil
	}
	response.Contact = wire.ToWireContact(*found)
	return response, nil
}

func (s *hostServer) Get(ctx context.Context, request *wire.ChatroomGetRequest) (*wire.ChatroomResponse, error) {
	const operation = "chatrooms.get"
	response := &wire.ChatroomResponse{}
	if s.host == nil || s.host.Rooms == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "chatroom capability"), operation)
		return response, nil
	}
	if request == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "chatroom username is missing"), operation)
		return response, nil
	}
	room, err := s.host.Rooms.Get(ctx, request.GetUsername())
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	if room == nil {
		response.Error = transport.EncodeError(missingHostResult(operation), operation)
		return response, nil
	}
	response.Chatroom = wire.ToWireChatroom(room)
	return response, nil
}

func (s *hostServer) Members(ctx context.Context, request *wire.ChatroomMembersRequest) (*wire.ChatroomMembersResponse, error) {
	const operation = "chatrooms.members"
	response := &wire.ChatroomMembersResponse{}
	if s.host == nil || s.host.Rooms == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "chatroom capability"), operation)
		return response, nil
	}
	if request == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "chatroom username is missing"), operation)
		return response, nil
	}
	members, err := s.host.Rooms.Members(ctx, request.GetUsername())
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	response.Members = make([]*wire.Member, len(members))
	for index := range members {
		response.Members[index] = wire.ToWireMember(&members[index])
	}
	return response, nil
}

func (s *hostServer) Acquire(ctx context.Context, request *wire.AcquireLeaseRequest) (*wire.LeaseResponse, error) {
	const operation = "sessions.acquire"
	if s.host == nil || s.host.Sessions == nil {
		return leaseErrorResponse(unimplementedHost(operation, "session capability"), operation), nil
	}
	if request == nil {
		return leaseErrorResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "lease request is missing"), operation), nil
	}
	topics := make([]event.Topic, len(request.GetTopics()))
	for index, topic := range request.GetTopics() {
		topics[index] = event.Topic(topic)
	}
	lease, err := s.host.Sessions.Acquire(ctx, request.GetTarget(), topics)
	return leaseResultResponse(lease, err, operation), nil
}

func (s *hostServer) Renew(ctx context.Context, request *wire.RenewLeaseRequest) (*wire.LeaseResponse, error) {
	const operation = "sessions.renew"
	if s.host == nil || s.host.Sessions == nil {
		return leaseErrorResponse(unimplementedHost(operation, "session capability"), operation), nil
	}
	if request == nil {
		return leaseErrorResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "lease request is missing"), operation), nil
	}
	lease, err := s.host.Sessions.Renew(ctx, request.GetLeaseId(), request.GetTtl().AsDuration())
	return leaseResultResponse(lease, err, operation), nil
}

func (s *hostServer) Release(ctx context.Context, request *wire.ReleaseLeaseRequest) (*wire.OperationResponse, error) {
	const operation = "sessions.release"
	if s.host == nil || s.host.Sessions == nil {
		return operationResponse(unimplementedHost(operation, "session capability"), operation), nil
	}
	if request == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "lease ID is missing"), operation), nil
	}
	return operationResponse(s.host.Sessions.Release(ctx, request.GetLeaseId()), operation), nil
}

func (s *hostServer) Call(ctx context.Context, request *wire.CallCapabilityRequest) (*wire.CallCapabilityResponse, error) {
	const operation = "caller.call"
	response := &wire.CallCapabilityResponse{}
	if s.host == nil || s.host.Caller == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "capability caller"), operation)
		return response, nil
	}
	if request == nil || request.GetRequest() == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, operation, "capability request is missing"), operation)
		return response, nil
	}
	value := wire.FromWireCapabilityRequest(request.GetRequest())
	if len(value.Payload) > capability.MaxInlineSize {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryResourceExhausted, operation, "inline payload is too large"), operation)
		return response, nil
	}
	result, err := s.host.Caller.Call(ctx, capability.Request{
		Name: value.Name, ContentType: value.ContentType, Payload: append([]byte(nil), value.Payload...),
	})
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	if result == nil {
		response.Error = transport.EncodeError(missingHostResult(operation), operation)
		return response, nil
	}
	if len(result.Payload) > capability.MaxInlineSize {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryResourceExhausted, operation, "inline result is too large"), operation)
		return response, nil
	}
	response.Result = wire.ToWireCapabilityResult(*result)
	return response, nil
}

func (s *hostServer) Read(ctx context.Context, _ *emptypb.Empty) (*wire.ReadConfigResponse, error) {
	const operation = "config.read"
	response := &wire.ReadConfigResponse{}
	if s.config == nil {
		response.Error = transport.EncodeError(unimplementedHost(operation, "configuration access"), operation)
		return response, nil
	}
	value, err := s.config.Read(ctx)
	if err != nil {
		response.Error = transport.EncodeError(err, operation)
		return response, nil
	}
	response.Config = wire.ToWireBytes(value)
	return response, nil
}

func (s *hostServer) Save(ctx context.Context, request *wire.SaveConfigRequest) (*wire.OperationResponse, error) {
	const operation = "config.save"
	if s.config == nil {
		return operationResponse(unimplementedHost(operation, "configuration access"), operation), nil
	}
	if request == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, operation, "configuration is missing"), operation), nil
	}
	return operationResponse(s.config.Save(ctx, wire.FromWireBytes(request.GetConfig())), operation), nil
}

type serverMediaReader struct {
	stream    grpc.ClientStreamingServer[wire.SendMediaRequest, wire.SendResultResponse]
	declared  int64
	total     int64
	pending   []byte
	operation string
	terminal  error
}

func (r *serverMediaReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	if len(r.pending) > 0 {
		count := copy(destination, r.pending)
		r.pending = r.pending[count:]
		return count, nil
	}
	if r.terminal != nil {
		return 0, r.terminal
	}
	request, err := r.stream.Recv()
	if errors.Is(err, io.EOF) {
		r.terminal = io.EOF
		if r.declared >= 0 && r.total != r.declared {
			r.terminal = sdk.NewError(sdk.CategoryInvalidArgument, r.operation, "media stream length does not match declared size")
		}
		return 0, r.terminal
	}
	if err != nil {
		r.terminal = transport.NormalizeError(err, r.operation)
		return 0, r.terminal
	}
	data := request.GetData()
	if request.GetMetadata() != nil || data == nil {
		r.terminal = sdk.NewError(sdk.CategoryInvalidArgument, r.operation, "media stream contains an invalid chunk")
		return 0, r.terminal
	}
	r.total += int64(len(data))
	if r.declared >= 0 && r.total > r.declared {
		r.terminal = sdk.NewError(sdk.CategoryInvalidArgument, r.operation, "media stream exceeds declared size")
		return 0, r.terminal
	}
	r.pending = append(r.pending[:0], data...)
	count := copy(destination, r.pending)
	r.pending = r.pending[count:]
	return count, nil
}

func (r *serverMediaReader) drain() error {
	buffer := make([]byte, transport.MaxMediaChunkBytes)
	for {
		_, err := r.Read(buffer)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func sendResultResponse(result *message.SendResult, err error, operation string) *wire.SendResultResponse {
	response := &wire.SendResultResponse{Error: transport.EncodeError(err, operation)}
	if result == nil {
		if err == nil {
			response.Error = transport.EncodeError(missingHostResult(operation), operation)
		}
		return response
	}
	encoded, conversionErr := wire.ToWireSendResult(result)
	if conversionErr != nil {
		if response.Error == nil {
			response.Error = transport.EncodeError(conversionErr, operation)
		}
		return response
	}
	response.Result = encoded
	return response
}

func sendErrorResponse(err error, operation string) *wire.SendResultResponse {
	return &wire.SendResultResponse{Error: transport.EncodeError(err, operation)}
}

func leaseResultResponse(lease *session.Lease, err error, operation string) *wire.LeaseResponse {
	response := &wire.LeaseResponse{Error: transport.EncodeError(err, operation)}
	if lease == nil {
		if err == nil {
			response.Error = transport.EncodeError(missingHostResult(operation), operation)
		}
		return response
	}
	encoded, conversionErr := wire.ToWireLease(lease)
	if conversionErr != nil {
		if response.Error == nil {
			response.Error = transport.EncodeError(conversionErr, operation)
		}
		return response
	}
	response.Lease = encoded
	return response
}

func leaseErrorResponse(err error, operation string) *wire.LeaseResponse {
	return &wire.LeaseResponse{Error: transport.EncodeError(err, operation)}
}

func sendDownloadError(stream grpc.ServerStreamingServer[wire.DownloadResponse], err error, operation string) error {
	return stream.Send(&wire.DownloadResponse{Value: &wire.DownloadResponse_Error{
		Error: transport.EncodeError(err, operation),
	}})
}

func unimplementedHost(operation, capabilityName string) error {
	return sdk.NewError(sdk.CategoryUnimplemented, operation, capabilityName+" is not configured")
}

func missingHostResult(operation string) error {
	return sdk.NewError(sdk.CategoryInternal, operation, "host returned no result")
}

func fromWireMessageType(value wire.MessageType) message.Type {
	switch value {
	case wire.MessageType_MESSAGE_TYPE_TEXT:
		return message.TypeText
	case wire.MessageType_MESSAGE_TYPE_IMAGE:
		return message.TypeImage
	case wire.MessageType_MESSAGE_TYPE_VOICE:
		return message.TypeVoice
	case wire.MessageType_MESSAGE_TYPE_VIDEO:
		return message.TypeVideo
	case wire.MessageType_MESSAGE_TYPE_EMOJI:
		return message.TypeEmoji
	case wire.MessageType_MESSAGE_TYPE_LOCATION:
		return message.TypeLocation
	case wire.MessageType_MESSAGE_TYPE_APP:
		return message.TypeApp
	case wire.MessageType_MESSAGE_TYPE_FILE:
		return message.TypeFile
	default:
		return message.TypeUnknown
	}
}
