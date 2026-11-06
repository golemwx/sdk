package wire

import "github.com/golemwx/sdk/message"

// ToWireMessage 将完整公开消息转换为 wire 消息。
func ToWireMessage(value *message.Message) (*Message, error) {
	if value == nil {
		return nil, nil
	}
	timestamp, err := toWireTimestamp(value.Timestamp)
	if err != nil {
		return nil, err
	}
	data, dataType, err := toWireData(value.Data)
	if err != nil {
		return nil, err
	}
	if data != nil && value.Type != dataType {
		return nil, invalidArgument("message.data", "data type does not match message type")
	}
	return &Message{
		Id:        value.ID,
		Type:      toWireMessageType(value.Type),
		Sender:    ToWireContact(value.Sender),
		Receiver:  ToWireContact(value.Receiver),
		Member:    ToWireMember(value.Member),
		Content:   value.Content,
		Raw:       ToWireBytes(value.Raw),
		Timestamp: timestamp,
		Data:      data,
	}, nil
}

// FromWireMessage 将 wire 消息转换为完整公开消息。
func FromWireMessage(value *Message) (*message.Message, error) {
	if value == nil {
		return nil, nil
	}
	timestamp, err := fromWireTimestamp(value.GetTimestamp(), "message.timestamp")
	if err != nil {
		return nil, err
	}
	data, dataType, err := fromWireData(value.GetData())
	if err != nil {
		return nil, err
	}
	messageType := fromWireMessageType(value.GetType())
	if data != nil && messageType != dataType {
		return nil, invalidArgument("message.data", "data type does not match message type")
	}
	return &message.Message{
		ID:        value.GetId(),
		Type:      messageType,
		Sender:    FromWireContact(value.GetSender()),
		Receiver:  FromWireContact(value.GetReceiver()),
		Member:    FromWireMember(value.GetMember()),
		Content:   value.GetContent(),
		Raw:       FromWireBytes(value.GetRaw()),
		Timestamp: timestamp,
		Data:      data,
	}, nil
}

func toWireData(value message.Data) (isMessage_Data, message.Type, error) {
	if value == nil {
		return nil, message.TypeUnknown, nil
	}
	switch data := value.(type) {
	case *message.Text:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "text data is nil")
		}
		return &Message_Text{Text: &TextData{}}, message.TypeText, nil
	case *message.Image:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "image data is nil")
		}
		return &Message_Image{Image: &ImageData{
			Media:  toWireMedia(data.Media),
			Width:  data.Width,
			Height: data.Height,
		}}, message.TypeImage, nil
	case *message.Voice:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "voice data is nil")
		}
		duration, err := toWireDuration(data.Duration, "message.voice.duration")
		if err != nil {
			return nil, message.TypeUnknown, err
		}
		return &Message_Voice{Voice: &VoiceData{
			Media:    toWireMedia(data.Media),
			Duration: duration,
			Format:   data.Format,
		}}, message.TypeVoice, nil
	case *message.Video:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "video data is nil")
		}
		duration, err := toWireDuration(data.Duration, "message.video.duration")
		if err != nil {
			return nil, message.TypeUnknown, err
		}
		return &Message_Video{Video: &VideoData{
			Media:         toWireMedia(data.Media),
			Duration:      duration,
			ThumbnailUrl:  data.ThumbnailURL,
			ThumbnailKey:  data.ThumbnailKey,
			ThumbnailSize: data.ThumbnailSize,
			NewMd5:        data.NewMD5,
		}}, message.TypeVideo, nil
	case *message.Emoji:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "emoji data is nil")
		}
		return &Message_Emoji{Emoji: &EmojiData{
			Md5:  data.MD5,
			Url:  data.URL,
			Size: data.Size,
		}}, message.TypeEmoji, nil
	case *message.Location:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "location data is nil")
		}
		return &Message_Location{Location: &LocationData{
			X:       data.X,
			Y:       data.Y,
			Label:   data.Label,
			PoiName: data.POIName,
		}}, message.TypeLocation, nil
	case *message.App:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "app data is nil")
		}
		return &Message_App{App: &AppData{
			Type:        data.Type,
			Title:       data.Title,
			Description: data.Description,
			Url:         data.URL,
		}}, message.TypeApp, nil
	case *message.File:
		if data == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "file data is nil")
		}
		return &Message_File{File: &FileData{
			Media:     toWireMedia(data.Media),
			Name:      data.Name,
			Extension: data.Extension,
			AppId:     data.AppID,
			AttachId:  data.AttachID,
		}}, message.TypeFile, nil
	default:
		return nil, message.TypeUnknown, invalidArgument("message.data", "unsupported message data type")
	}
}

func fromWireData(value isMessage_Data) (message.Data, message.Type, error) {
	if value == nil {
		return nil, message.TypeUnknown, nil
	}
	switch data := value.(type) {
	case *Message_Text:
		if data.Text == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "text data is missing")
		}
		return &message.Text{}, message.TypeText, nil
	case *Message_Image:
		if data.Image == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "image data is missing")
		}
		return &message.Image{
			Media:  fromWireMedia(data.Image.GetMedia()),
			Width:  data.Image.GetWidth(),
			Height: data.Image.GetHeight(),
		}, message.TypeImage, nil
	case *Message_Voice:
		if data.Voice == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "voice data is missing")
		}
		duration, err := fromWireDuration(data.Voice.GetDuration(), "message.voice.duration")
		if err != nil {
			return nil, message.TypeUnknown, err
		}
		return &message.Voice{
			Media:    fromWireMedia(data.Voice.GetMedia()),
			Duration: duration,
			Format:   data.Voice.GetFormat(),
		}, message.TypeVoice, nil
	case *Message_Video:
		if data.Video == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "video data is missing")
		}
		duration, err := fromWireDuration(data.Video.GetDuration(), "message.video.duration")
		if err != nil {
			return nil, message.TypeUnknown, err
		}
		return &message.Video{
			Media:         fromWireMedia(data.Video.GetMedia()),
			Duration:      duration,
			ThumbnailURL:  data.Video.GetThumbnailUrl(),
			ThumbnailKey:  data.Video.GetThumbnailKey(),
			ThumbnailSize: data.Video.GetThumbnailSize(),
			NewMD5:        data.Video.GetNewMd5(),
		}, message.TypeVideo, nil
	case *Message_Emoji:
		if data.Emoji == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "emoji data is missing")
		}
		return &message.Emoji{
			MD5:  data.Emoji.GetMd5(),
			URL:  data.Emoji.GetUrl(),
			Size: data.Emoji.GetSize(),
		}, message.TypeEmoji, nil
	case *Message_Location:
		if data.Location == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "location data is missing")
		}
		return &message.Location{
			X:       data.Location.GetX(),
			Y:       data.Location.GetY(),
			Label:   data.Location.GetLabel(),
			POIName: data.Location.GetPoiName(),
		}, message.TypeLocation, nil
	case *Message_App:
		if data.App == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "app data is missing")
		}
		return &message.App{
			Type:        data.App.GetType(),
			Title:       data.App.GetTitle(),
			Description: data.App.GetDescription(),
			URL:         data.App.GetUrl(),
		}, message.TypeApp, nil
	case *Message_File:
		if data.File == nil {
			return nil, message.TypeUnknown, invalidArgument("message.data", "file data is missing")
		}
		return &message.File{
			Media:     fromWireMedia(data.File.GetMedia()),
			Name:      data.File.GetName(),
			Extension: data.File.GetExtension(),
			AppID:     data.File.GetAppId(),
			AttachID:  data.File.GetAttachId(),
		}, message.TypeFile, nil
	default:
		return nil, message.TypeUnknown, invalidArgument("message.data", "unsupported wire message data type")
	}
}

func toWireMedia(value message.Media) *MediaData {
	return &MediaData{
		FileId: value.FileID,
		Md5:    value.MD5,
		Key:    value.Key,
		Url:    value.URL,
		Size:   value.Size,
	}
}

func fromWireMedia(value *MediaData) message.Media {
	if value == nil {
		return message.Media{}
	}
	return message.Media{
		FileID: value.GetFileId(),
		MD5:    value.GetMd5(),
		Key:    value.GetKey(),
		URL:    value.GetUrl(),
		Size:   value.GetSize(),
	}
}

// ToWireSendResult 转换真实发送结果。
func ToWireSendResult(value *message.SendResult) (*SendResult, error) {
	if value == nil {
		return nil, nil
	}
	createdAt, err := toWireOptionalTimestamp(value.CreatedAt)
	if err != nil {
		return nil, err
	}
	serverTime, err := toWireOptionalTimestamp(value.ServerTime)
	if err != nil {
		return nil, err
	}
	return &SendResult{
		Id:         value.ID,
		NewId:      value.NewID,
		ClientId:   value.ClientID,
		Sender:     ToWireContact(value.Sender),
		Receiver:   ToWireContact(value.Receiver),
		Type:       toWireMessageType(value.Type),
		CreatedAt:  createdAt,
		ServerTime: serverTime,
		Outcome:    toWireOutcome(value.Outcome),
	}, nil
}

// FromWireSendResult 转换真实发送结果。
func FromWireSendResult(value *SendResult) (*message.SendResult, error) {
	if value == nil {
		return nil, nil
	}
	createdAt, err := fromWireOptionalTimestamp(value.GetCreatedAt(), "message.send_result.created_at")
	if err != nil {
		return nil, err
	}
	serverTime, err := fromWireOptionalTimestamp(value.GetServerTime(), "message.send_result.server_time")
	if err != nil {
		return nil, err
	}
	return &message.SendResult{
		ID:         value.GetId(),
		NewID:      value.GetNewId(),
		ClientID:   value.GetClientId(),
		Sender:     FromWireContact(value.GetSender()),
		Receiver:   FromWireContact(value.GetReceiver()),
		Type:       fromWireMessageType(value.GetType()),
		CreatedAt:  createdAt,
		ServerTime: serverTime,
		Outcome:    fromWireOutcome(value.GetOutcome()),
	}, nil
}

func toWireMessageType(value message.Type) MessageType {
	switch value {
	case message.TypeText:
		return MessageType_MESSAGE_TYPE_TEXT
	case message.TypeImage:
		return MessageType_MESSAGE_TYPE_IMAGE
	case message.TypeVoice:
		return MessageType_MESSAGE_TYPE_VOICE
	case message.TypeVideo:
		return MessageType_MESSAGE_TYPE_VIDEO
	case message.TypeEmoji:
		return MessageType_MESSAGE_TYPE_EMOJI
	case message.TypeLocation:
		return MessageType_MESSAGE_TYPE_LOCATION
	case message.TypeApp:
		return MessageType_MESSAGE_TYPE_APP
	case message.TypeFile:
		return MessageType_MESSAGE_TYPE_FILE
	default:
		return MessageType_MESSAGE_TYPE_UNKNOWN
	}
}

func fromWireMessageType(value MessageType) message.Type {
	switch value {
	case MessageType_MESSAGE_TYPE_TEXT:
		return message.TypeText
	case MessageType_MESSAGE_TYPE_IMAGE:
		return message.TypeImage
	case MessageType_MESSAGE_TYPE_VOICE:
		return message.TypeVoice
	case MessageType_MESSAGE_TYPE_VIDEO:
		return message.TypeVideo
	case MessageType_MESSAGE_TYPE_EMOJI:
		return message.TypeEmoji
	case MessageType_MESSAGE_TYPE_LOCATION:
		return message.TypeLocation
	case MessageType_MESSAGE_TYPE_APP:
		return message.TypeApp
	case MessageType_MESSAGE_TYPE_FILE:
		return message.TypeFile
	default:
		return message.TypeUnknown
	}
}
