package wire

import (
	"math"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestDurationRange 验证合法 Proto 时长超出 Go 范围时不会被静默截断
func TestDurationRange(t *testing.T) {
	for _, value := range []*durationpb.Duration{{Seconds: 315576000000}, {Seconds: -315576000000}, {Nanos: 1000000000}} {
		if _, err := FromWireDuration(value, "fixture.duration"); err == nil {
			t.Fatalf("out-of-range duration accepted: %v", value)
		}
	}
	for _, value := range []time.Duration{0, 1, -1, time.Duration(math.MaxInt64), time.Duration(math.MinInt64)} {
		decoded, err := FromWireDuration(durationpb.New(value), "fixture.duration")
		if err != nil || decoded != value {
			t.Fatalf("duration %v changed to %v: %v", value, decoded, err)
		}
	}
}

// TestSendReceiptValidation 验证空成功、失败回执和未知结果撤回引用均被拒绝
func TestSendReceiptValidation(t *testing.T) {
	conversation := sdk.Conversation{AccountUsername: "bot", Kind: sdk.ConversationKindDirect, ID: "friend"}
	valid := &message.SendReceipt{
		MessageID: "1", Conversation: conversation, SentAt: sampleTime(), Outcome: sdk.OutcomeSucceeded,
	}
	if _, err := ToWireSendReceipt(valid); err != nil {
		t.Fatal(err)
	}
	for _, value := range []*message.SendReceipt{
		{Conversation: conversation, SentAt: sampleTime(), Outcome: sdk.OutcomeSucceeded},
		{MessageID: "1", Conversation: conversation, SentAt: sampleTime(), Outcome: sdk.OutcomeFailed},
		{
			Conversation: conversation, SentAt: sampleTime(), Outcome: sdk.OutcomeUnknown,
			RevokeRef: &sdk.RevokeRef{Token: "revoke", ExpiresAt: sampleTime().Add(time.Minute)},
		},
	} {
		if _, err := ToWireSendReceipt(value); err == nil {
			t.Fatalf("非法回执被接受: %+v", value)
		}
	}
}

// TestAccountIdentityAndStatus 验证只有未登录或未知状态可以缺少账号，在线标记必须一致
func TestAccountIdentityAndStatus(t *testing.T) {
	for _, status := range []sdk.AccountStatus{sdk.AccountStatusUnknown, sdk.AccountStatusUnlogged, sdk.AccountStatusLoggedIn, sdk.AccountStatusOnline, sdk.AccountStatusOffline} {
		input := &sdk.Account{Status: status, Online: status == sdk.AccountStatusOnline, UpdatedAt: sampleTime()}
		_, err := ToWireAccount(input)
		if (err == nil) != (status <= sdk.AccountStatusUnlogged) {
			t.Fatalf("unexpected missing username acceptance for %v", status)
		}
	}
	if _, err := FromWireAccount(&Account{Status: AccountStatus_ACCOUNT_STATUS_ONLINE, Online: true, UpdatedAt: timestamppb.New(sampleTime())}); err == nil {
		t.Fatal("online account without username accepted")
	}
	if _, err := FromWireAccount(&Account{Username: "owner", Status: AccountStatus_ACCOUNT_STATUS_ONLINE, UpdatedAt: timestamppb.New(sampleTime())}); err == nil {
		t.Fatal("inconsistent online state accepted")
	}
	future, err := FromWireAccount(&Account{Status: AccountStatus(999), Online: true, UpdatedAt: timestamppb.New(sampleTime())})
	if err != nil || future.Status != sdk.AccountStatusUnknown || !future.Online {
		t.Fatal("future enum discarded other usable fields")
	}
}

// TestMissingCollectionEntriesRejected 验证缺失列表项及成功批量项的缺失值不会被静默忽略
func TestMissingCollectionEntriesRejected(t *testing.T) {
	if _, err := FromWireContactPage(&ContactPage{Items: []*Contact{nil}}); err == nil {
		t.Fatal("missing contact silently dropped")
	}
	if _, err := FromWireMemberPage(&MemberPage{Items: []*Member{nil}}); err == nil {
		t.Fatal("missing member silently dropped")
	}
	if _, err := FromWireContactBatchResult(&ContactBatchResult{Items: []*ContactBatchItem{nil}}); err == nil {
		t.Fatal("missing batch item silently dropped")
	}
	if _, err := FromWireContactBatchResult(&ContactBatchResult{Items: []*ContactBatchItem{{SubjectId: "friend"}}}); err == nil {
		t.Fatal("missing successful batch value accepted")
	}
	if _, err := FromWireChatroomRefreshResult(&ChatroomRefreshResult{Stages: []*RefreshStageResult{nil}}); err == nil {
		t.Fatal("missing stage silently dropped")
	}
}

// TestFailedBatchItemIgnoresValue 验证失败项只依赖错误，不让不可信 Value 覆盖已有资料
func TestFailedBatchItemIgnoresValue(t *testing.T) {
	value := &ContactBatchResult{Items: []*ContactBatchItem{{SubjectId: "friend", Value: &Contact{}, Error: ToWireError(sdk.NewError(sdk.CategoryUnavailable, "contacts.refresh", "unavailable"))}}}
	decoded, err := FromWireContactBatchResult(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].Error == nil || decoded.Items[0].Value.Username != "" {
		t.Fatalf("failed item lost or replaced existing value: %v", decoded)
	}
}
