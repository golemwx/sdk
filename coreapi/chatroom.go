package coreapi

import (
	"context"
	"encoding/json"

	"github.com/golemwx/sdk"
)

// ChatroomClient 访问 core 的建群、成员、管理员和资料修改 API。
type ChatroomClient Client

// Create 创建群聊。
func (c *ChatroomClient) Create(ctx context.Context, usernames []string) (json.RawMessage, error) {
	if len(usernames) == 0 || len(usernames) > 1000 || containsBlank(usernames) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomCreate), "1-1000 initial members are required")
	}
	return (*Client)(c).call(ctx, ChatroomCreate, ChatroomCreateRequest{Usernames: append([]string(nil), usernames...)})
}

// Info 获取群扩展信息。
func (c *ChatroomClient) Info(ctx context.Context, room string) (json.RawMessage, error) {
	if err := required(operation(ChatroomInfo), room); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ChatroomInfo, ChatroomRequest{Chatroom: room})
}

// QRCode 获取群二维码。
func (c *ChatroomClient) QRCode(ctx context.Context, room string) (json.RawMessage, error) {
	if err := required(operation(ChatroomQRCode), room); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ChatroomQRCode, ChatroomRequest{Chatroom: room})
}

// Members 获取群成员列表。
func (c *ChatroomClient) Members(ctx context.Context, room string) (json.RawMessage, error) {
	if err := required(operation(ChatroomMembers), room); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ChatroomMembers, ChatroomRequest{Chatroom: room})
}

// MemberDetail 批量获取群成员资料。
func (c *ChatroomClient) MemberDetail(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	if room == "" || len(usernames) == 0 || len(usernames) > 1000 || containsBlank(usernames) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomMemberDetail), "room and 1-1000 members are required")
	}
	return (*Client)(c).call(ctx, ChatroomMemberDetail, ChatroomMembersRequest{Chatroom: room, Usernames: append([]string(nil), usernames...)})
}

func (c *ChatroomClient) memberWrite(ctx context.Context, method Method, room string, usernames []string) (json.RawMessage, error) {
	if room == "" || len(usernames) == 0 || len(usernames) > 1000 || containsBlank(usernames) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "room and 1-1000 members are required")
	}
	return (*Client)(c).call(ctx, method, ChatroomMembersRequest{Chatroom: room, Usernames: append([]string(nil), usernames...)})
}

// AddMembers 直接添加群成员。
func (c *ChatroomClient) AddMembers(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	return c.memberWrite(ctx, ChatroomAddMembers, room, usernames)
}

// InviteMembers 邀请联系人入群。
func (c *ChatroomClient) InviteMembers(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	return c.memberWrite(ctx, ChatroomInviteMembers, room, usernames)
}

// RemoveMembers 移除群成员。
func (c *ChatroomClient) RemoveMembers(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	return c.memberWrite(ctx, ChatroomRemoveMembers, room, usernames)
}

// AddAdmins 添加群管理员。
func (c *ChatroomClient) AddAdmins(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	return c.memberWrite(ctx, ChatroomAddAdmins, room, usernames)
}

// RemoveAdmins 取消群管理员。
func (c *ChatroomClient) RemoveAdmins(ctx context.Context, room string, usernames []string) (json.RawMessage, error) {
	return c.memberWrite(ctx, ChatroomRemoveAdmins, room, usernames)
}

// TransferOwner 转让群主。
func (c *ChatroomClient) TransferOwner(ctx context.Context, room, username string) (json.RawMessage, error) {
	if room == "" || username == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomTransferOwner), "room and username are required")
	}
	return (*Client)(c).call(ctx, ChatroomTransferOwner, ChatroomTransferRequest{Chatroom: room, Username: username})
}

// SetName 修改群名称。
func (c *ChatroomClient) SetName(ctx context.Context, room, name string) (json.RawMessage, error) {
	if room == "" || name == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomSetName), "room and name are required")
	}
	return (*Client)(c).call(ctx, ChatroomSetName, ChatroomNameRequest{Chatroom: room, Name: name})
}

// SetRemark 设置或清除群备注。
func (c *ChatroomClient) SetRemark(ctx context.Context, room string, remark *string) (json.RawMessage, error) {
	if room == "" || remark == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomSetRemark), "room and explicit remark are required")
	}
	return (*Client)(c).call(ctx, ChatroomSetRemark, ChatroomRemarkRequest{Chatroom: room, Remark: remark})
}

// SetAnnouncement 修改群公告。
func (c *ChatroomClient) SetAnnouncement(ctx context.Context, request ChatroomAnnouncementRequest) (json.RawMessage, error) {
	if request.Chatroom == "" || request.Content == nil || request.InfoVersion == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomSetAnnouncement), "room, content and info_version are required")
	}
	return (*Client)(c).call(ctx, ChatroomSetAnnouncement, request)
}

// SetDisplayName 修改本人群昵称。
func (c *ChatroomClient) SetDisplayName(ctx context.Context, request ChatroomDisplayNameRequest) (json.RawMessage, error) {
	if request.Chatroom == "" || request.DisplayName == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomSetDisplayName), "room and explicit display_name are required")
	}
	return (*Client)(c).call(ctx, ChatroomSetDisplayName, request)
}

// SetSavedToContacts 设置群是否保存到通讯录。
func (c *ChatroomClient) SetSavedToContacts(ctx context.Context, request ChatroomSavedRequest) (json.RawMessage, error) {
	if request.Chatroom == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(ChatroomSetSavedToContacts), "room is required")
	}
	return (*Client)(c).call(ctx, ChatroomSetSavedToContacts, request)
}

// Quit 退出群聊。
func (c *ChatroomClient) Quit(ctx context.Context, room string) (json.RawMessage, error) {
	if err := required(operation(ChatroomQuit), room); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, ChatroomQuit, ChatroomRequest{Chatroom: room})
}
