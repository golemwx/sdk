// Package coreapi declares the current golem-core public business APIs.
//
// The package exposes stable method names and typed clients. It deliberately
// does not expose HTTP paths, core DTOs, CDN AES keys, login verification
// tokens, or arbitrary transport access.
package coreapi

// Method is a stable host-routed core business method.
type Method string

const (
	LoginStart         Method = "login.start"
	LoginStatus        Method = "login.status"
	LoginAccountStatus Method = "login.account_status"
	LoginCancel        Method = "login.cancel"
	LoginLogout        Method = "login.logout"

	ContactList            Method = "contact.list"
	ContactDetail          Method = "contact.detail"
	ContactSearch          Method = "contact.search"
	ContactSendRequest     Method = "contact.send_request"
	ContactPassVerify      Method = "contact.pass_verify"
	ContactSetRemark       Method = "contact.set_remark"
	ContactBlacklistAdd    Method = "contact.blacklist_add"
	ContactBlacklistRemove Method = "contact.blacklist_remove"
	ContactDelete          Method = "contact.delete"

	ChatroomCreate             Method = "chatroom.create"
	ChatroomInfo               Method = "chatroom.info"
	ChatroomQRCode             Method = "chatroom.qrcode"
	ChatroomMembers            Method = "chatroom.members"
	ChatroomMemberDetail       Method = "chatroom.member_detail"
	ChatroomAddMembers         Method = "chatroom.add_members"
	ChatroomInviteMembers      Method = "chatroom.invite_members"
	ChatroomRemoveMembers      Method = "chatroom.remove_members"
	ChatroomAddAdmins          Method = "chatroom.add_admins"
	ChatroomRemoveAdmins       Method = "chatroom.remove_admins"
	ChatroomTransferOwner      Method = "chatroom.transfer_owner"
	ChatroomSetName            Method = "chatroom.set_name"
	ChatroomSetRemark          Method = "chatroom.set_remark"
	ChatroomSetAnnouncement    Method = "chatroom.set_announcement"
	ChatroomSetDisplayName     Method = "chatroom.set_display_name"
	ChatroomSetSavedToContacts Method = "chatroom.set_saved_to_contacts"
	ChatroomQuit               Method = "chatroom.quit"

	MessageSendText        Method = "message.send_text"
	MessageSendEmoji       Method = "message.send_emoji"
	MessageSendImage       Method = "message.send_image"
	MessageSendVoice       Method = "message.send_voice"
	MessageSendVideo       Method = "message.send_video"
	MessageSendApp         Method = "message.send_app"
	MessageSendRecord      Method = "message.send_record"
	MessageSendNote        Method = "message.send_note"
	MessageSendFile        Method = "message.send_file"
	MessageSendLink        Method = "message.send_link"
	MessageSendMusic       Method = "message.send_music"
	MessageSendPosition    Method = "message.send_position"
	MessageSendNameCard    Method = "message.send_name_card"
	MessageRevoke          Method = "message.revoke"
	MessageDownloadVoice   Method = "message.download_voice"
	MessageTranscribeVoice Method = "message.transcribe_voice"
	MessageForwardImage    Method = "message.forward_image"
	MessageForwardVideo    Method = "message.forward_video"
	MessageForwardFile     Method = "message.forward_file"
	MessageForwardCDNImage Method = "message.forward_cdn_image"
	MessageForwardCDNVideo Method = "message.forward_cdn_video"
	MessageForwardCDNFile  Method = "message.forward_cdn_file"

	FavoriteInfo        Method = "favorite.info"
	FavoriteBatchGet    Method = "favorite.batch_get"
	FavoriteBatchDelete Method = "favorite.batch_delete"
	FavoriteSync        Method = "favorite.sync"
	FavoriteShare       Method = "favorite.share"
	FavoriteCheckCDN    Method = "favorite.check_cdn"

	CDNUploadImage        Method = "cdn.upload_image"
	CDNUploadVideo        Method = "cdn.upload_video"
	CDNUploadFile         Method = "cdn.upload_file"
	CDNUploadMomentsImage Method = "cdn.upload_moments_image"
	CDNUploadMomentsVideo Method = "cdn.upload_moments_video"
	CDNDownloadImage      Method = "cdn.download_image"
	CDNDownloadVideo      Method = "cdn.download_video"
	CDNDownloadVideoCover Method = "cdn.download_video_cover"
	CDNDownloadFile       Method = "cdn.download_file"

	LabelList           Method = "label.list"
	LabelAdd            Method = "label.add"
	LabelDelete         Method = "label.delete"
	LabelUpdate         Method = "label.update"
	LabelModifyContacts Method = "label.modify_contacts"

	MiniappJSLogin         Method = "miniapp.js_login"
	MiniappQrcodeAuthLogin Method = "miniapp.qrcode_auth_login"
	MiniappRuntimeSession  Method = "miniapp.runtime_session"
	MiniappSessionQRCode   Method = "miniapp.session_qrcode"
	MiniappOperate         Method = "miniapp.operate"
	MiniappCloudFunction   Method = "miniapp.cloud_function"
	MiniappSendVerifyCode  Method = "miniapp.send_verify_code"
	MiniappCheckVerifyCode Method = "miniapp.check_verify_code"
	MiniappMobiles         Method = "miniapp.mobiles"
	MiniappAddMobile       Method = "miniapp.add_mobile"
	MiniappDeleteMobile    Method = "miniapp.delete_mobile"
	MiniappRandomAvatar    Method = "miniapp.random_avatar"
	MiniappAddAvatar       Method = "miniapp.add_avatar"
	MiniappUploadAvatar    Method = "miniapp.upload_avatar"
	MiniappRecord          Method = "miniapp.record"
	MiniappAddRecord       Method = "miniapp.add_record"
	MiniappOpenID          Method = "miniapp.openid"
	MiniappOauthSDK        Method = "miniapp.oauth_sdk"
	MiniappThirdAppGrant   Method = "miniapp.third_app_grant"

	MomentsTimeline   Method = "moments.timeline"
	MomentsUserPage   Method = "moments.user_page"
	MomentsDetail     Method = "moments.detail"
	MomentsPost       Method = "moments.post"
	MomentsComment    Method = "moments.comment"
	MomentsOperate    Method = "moments.operate"
	MomentsSync       Method = "moments.sync"
	MomentsDelete     Method = "moments.delete"
	MomentsSetPrivacy Method = "moments.set_privacy"

	OfficialFollow         Method = "official.follow"
	OfficialQuit           Method = "official.quit"
	OfficialGetA8Key       Method = "official.get_a8key"
	OfficialJSAPIPreVerify Method = "official.jsapi_pre_verify"
	OfficialOauth          Method = "official.oauth"
	OfficialReadArticle    Method = "official.read_article"
	OfficialLikeArticle    Method = "official.like_article"

	PaymentCards         Method = "payment.cards"
	PaymentQRCode        Method = "payment.qrcode"
	PaymentCollect       Method = "payment.collect"
	PaymentHongBaoDetail Method = "payment.hongbao_detail"
	PaymentHongBaoList   Method = "payment.hongbao_list"
	PaymentGrabHongBao   Method = "payment.grab_hongbao"

	UserProfile          Method = "user.profile"
	UserUpdateProfile    Method = "user.update_profile"
	UserUploadAvatar     Method = "user.upload_avatar"
	UserQRCode           Method = "user.qrcode"
	UserSetPrivacy       Method = "user.set_privacy"
	UserSetAlias         Method = "user.set_alias"
	UserSafetyDevices    Method = "user.safety_devices"
	UserDeleteSafeDevice Method = "user.delete_safe_device"
	UserReportMotion     Method = "user.report_motion"
)

// AllMethods returns every method exposed by this contract.
func AllMethods() []Method {
	return []Method{
		LoginStart, LoginStatus, LoginAccountStatus, LoginCancel, LoginLogout,
		ContactList, ContactDetail, ContactSearch, ContactSendRequest, ContactPassVerify,
		ContactSetRemark, ContactBlacklistAdd, ContactBlacklistRemove, ContactDelete,
		ChatroomCreate, ChatroomInfo, ChatroomQRCode, ChatroomMembers, ChatroomMemberDetail,
		ChatroomAddMembers, ChatroomInviteMembers, ChatroomRemoveMembers, ChatroomAddAdmins,
		ChatroomRemoveAdmins, ChatroomTransferOwner, ChatroomSetName, ChatroomSetRemark,
		ChatroomSetAnnouncement, ChatroomSetDisplayName, ChatroomSetSavedToContacts, ChatroomQuit,
		MessageSendText, MessageSendEmoji, MessageSendImage, MessageSendVoice, MessageSendVideo,
		MessageSendApp, MessageSendRecord, MessageSendNote, MessageSendFile, MessageSendLink,
		MessageSendMusic, MessageSendPosition, MessageSendNameCard, MessageRevoke,
		MessageDownloadVoice, MessageTranscribeVoice, MessageForwardImage, MessageForwardVideo,
		MessageForwardFile, MessageForwardCDNImage, MessageForwardCDNVideo, MessageForwardCDNFile,
		FavoriteInfo, FavoriteBatchGet, FavoriteBatchDelete, FavoriteSync, FavoriteShare, FavoriteCheckCDN,
		CDNUploadImage, CDNUploadVideo, CDNUploadFile, CDNUploadMomentsImage, CDNUploadMomentsVideo,
		CDNDownloadImage, CDNDownloadVideo, CDNDownloadVideoCover, CDNDownloadFile,
		LabelList, LabelAdd, LabelDelete, LabelUpdate, LabelModifyContacts,
		MiniappJSLogin, MiniappQrcodeAuthLogin, MiniappRuntimeSession, MiniappSessionQRCode,
		MiniappOperate, MiniappCloudFunction, MiniappSendVerifyCode, MiniappCheckVerifyCode,
		MiniappMobiles, MiniappAddMobile, MiniappDeleteMobile, MiniappRandomAvatar,
		MiniappAddAvatar, MiniappUploadAvatar, MiniappRecord, MiniappAddRecord, MiniappOpenID,
		MiniappOauthSDK, MiniappThirdAppGrant,
		MomentsTimeline, MomentsUserPage, MomentsDetail, MomentsPost, MomentsComment,
		MomentsOperate, MomentsSync, MomentsDelete, MomentsSetPrivacy,
		OfficialFollow, OfficialQuit, OfficialGetA8Key, OfficialJSAPIPreVerify, OfficialOauth,
		OfficialReadArticle, OfficialLikeArticle,
		PaymentCards, PaymentQRCode, PaymentCollect, PaymentHongBaoDetail, PaymentHongBaoList,
		PaymentGrabHongBao,
		UserProfile, UserUpdateProfile, UserUploadAvatar, UserQRCode, UserSetPrivacy,
		UserSetAlias, UserSafetyDevices, UserDeleteSafeDevice, UserReportMotion,
	}
}
