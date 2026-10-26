package wire

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
)

// ToWireContactPage 转换联系人分页结果
func ToWireContactPage(value *sdk.Page[contact.Contact]) (*ContactPage, error) {
	if value == nil {
		return nil, nil
	}

	items := make([]*Contact, 0, len(value.Items))
	for index := range value.Items {
		item, err := ToWireContact(&value.Items[index])
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return &ContactPage{
		Items:        items,
		NextCursor:   value.NextCursor,
		Total:        value.Total,
		Completeness: toCompleteness(value.Completeness),
	}, nil
}

// FromWireContactPage 转换 wire 联系人分页结果
func FromWireContactPage(value *ContactPage) (*sdk.Page[contact.Contact], error) {
	if value == nil {
		return nil, nil
	}

	items := make([]contact.Contact, 0, len(value.Items))
	for _, wireItem := range value.Items {
		if wireItem == nil {
			return nil, decodeError("contact page contains a missing item")
		}
		item, err := FromWireContact(wireItem)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}

	return &sdk.Page[contact.Contact]{
		Items:        items,
		NextCursor:   value.NextCursor,
		Total:        value.Total,
		Completeness: fromCompleteness(value.Completeness),
	}, nil
}

// ToWireMemberPage 转换群成员分页结果
func ToWireMemberPage(value *sdk.Page[chatroom.Member]) (*MemberPage, error) {
	if value == nil {
		return nil, nil
	}

	items := make([]*Member, 0, len(value.Items))
	for index := range value.Items {
		item, err := ToWireMember(&value.Items[index])
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return &MemberPage{
		Items:        items,
		NextCursor:   value.NextCursor,
		Total:        value.Total,
		Completeness: toCompleteness(value.Completeness),
	}, nil
}

// FromWireMemberPage 转换 wire 群成员分页结果
func FromWireMemberPage(value *MemberPage) (*sdk.Page[chatroom.Member], error) {
	if value == nil {
		return nil, nil
	}

	items := make([]chatroom.Member, 0, len(value.Items))
	for _, wireItem := range value.Items {
		if wireItem == nil {
			return nil, decodeError("member page contains a missing item")
		}
		item, err := FromWireMember(wireItem)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}

	return &sdk.Page[chatroom.Member]{
		Items:        items,
		NextCursor:   value.NextCursor,
		Total:        value.Total,
		Completeness: fromCompleteness(value.Completeness),
	}, nil
}

// ToWireContactBatchResult 转换联系人批量结果
func ToWireContactBatchResult(value *sdk.BatchResult[contact.Contact]) (*ContactBatchResult, error) {
	if value == nil {
		return nil, nil
	}

	items := make([]*ContactBatchItem, 0, len(value.Items))
	for index := range value.Items {
		item := &value.Items[index]
		var contactValue *Contact
		if item.Error == nil {
			var err error
			contactValue, err = ToWireContact(&item.Value)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, &ContactBatchItem{
			SubjectId: item.SubjectID,
			Value:     contactValue,
			Outcome:   toWriteOutcome(item.Outcome),
			Error:     ToWireError(item.Error),
		})
	}

	return &ContactBatchResult{Items: items}, nil
}

// FromWireContactBatchResult 转换 wire 联系人批量结果
func FromWireContactBatchResult(value *ContactBatchResult) (*sdk.BatchResult[contact.Contact], error) {
	if value == nil {
		return nil, nil
	}

	items := make([]sdk.BatchItem[contact.Contact], 0, len(value.Items))
	for _, wireItem := range value.Items {
		if wireItem == nil {
			return nil, decodeError("contact batch contains a missing item")
		}
		itemError, err := FromWireError(wireItem.Error)
		if err != nil {
			return nil, err
		}
		var itemValue contact.Contact
		if itemError == nil {
			if wireItem.Value == nil {
				return nil, decodeError("successful contact batch item has no value")
			}
			contactValue, err := FromWireContact(wireItem.Value)
			if err != nil {
				return nil, err
			}
			itemValue = *contactValue
		}
		items = append(items, sdk.BatchItem[contact.Contact]{
			SubjectID: wireItem.SubjectId,
			Value:     itemValue,
			Outcome:   fromWriteOutcome(wireItem.Outcome),
			Error:     itemError,
		})
	}

	return &sdk.BatchResult[contact.Contact]{Items: items}, nil
}

// ToWireChatroomRefreshResult 转换群刷新结果
func ToWireChatroomRefreshResult(value *chatroom.ChatroomRefreshResult) (*ChatroomRefreshResult, error) {
	if value == nil {
		return nil, nil
	}

	room, err := ToWireRoom(value.Room)
	if err != nil {
		return nil, err
	}
	members, err := ToWireMemberPage(value.Members)
	if err != nil {
		return nil, err
	}
	stages := make([]*RefreshStageResult, 0, len(value.Stages))
	for _, stage := range value.Stages {
		stages = append(stages, &RefreshStageResult{
			Name:    stage.Name,
			Outcome: toWriteOutcome(stage.Outcome),
			Error:   ToWireError(stage.Error),
		})
	}

	return &ChatroomRefreshResult{Room: room, Members: members, Stages: stages}, nil
}

// FromWireChatroomRefreshResult 转换 wire 群刷新结果
func FromWireChatroomRefreshResult(value *ChatroomRefreshResult) (*chatroom.ChatroomRefreshResult, error) {
	if value == nil {
		return nil, nil
	}

	room, err := FromWireRoom(value.Room)
	if err != nil {
		return nil, err
	}
	members, err := FromWireMemberPage(value.Members)
	if err != nil {
		return nil, err
	}
	stages := make([]chatroom.RefreshStageResult, 0, len(value.Stages))
	for _, wireStage := range value.Stages {
		if wireStage == nil {
			return nil, decodeError("chatroom refresh contains a missing stage")
		}
		stageError, err := FromWireError(wireStage.Error)
		if err != nil {
			return nil, err
		}
		stages = append(stages, chatroom.RefreshStageResult{
			Name:    wireStage.Name,
			Outcome: fromWriteOutcome(wireStage.Outcome),
			Error:   stageError,
		})
	}

	return &chatroom.ChatroomRefreshResult{Room: room, Members: members, Stages: stages}, nil
}
