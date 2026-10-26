package wire

import (
	"fmt"
	"time"

	"github.com/golemwx/sdk"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// encodeError 创建传输编码参数错误
func encodeError(reason string) error {
	return sdk.NewError(sdk.CategoryInvalidArgument, "wire.encode", reason)
}

// decodeError 创建传输解码契约错误
func decodeError(reason string) error {
	return sdk.NewError(sdk.CategoryInternal, "wire.decode", reason)
}

// toRequiredTimestamp 校验并编码必填 UTC 时间，保留纳秒精度
func toRequiredTimestamp(value time.Time, field string) (*timestamppb.Timestamp, error) {
	if value.IsZero() {
		return nil, encodeError(field + " must not be zero")
	}

	result := timestamppb.New(value.UTC())
	if err := result.CheckValid(); err != nil {
		return nil, encodeError(fmt.Sprintf("%s is invalid: %v", field, err))
	}

	return result, nil
}

// fromRequiredTimestamp 校验并解码必填 UTC 时间
func fromRequiredTimestamp(value *timestamppb.Timestamp, field string) (time.Time, error) {
	if value == nil {
		return time.Time{}, decodeError(field + " is missing")
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, decodeError(fmt.Sprintf("%s is invalid: %v", field, err))
	}

	result := value.AsTime().UTC()
	if result.IsZero() {
		return time.Time{}, decodeError(field + " must not be zero")
	}

	return result, nil
}

// toOptionalTimestamp 保留缺失状态并编码可选时间
func toOptionalTimestamp(value *time.Time, field string) (*timestamppb.Timestamp, error) {
	if value == nil {
		return nil, nil
	}

	return toRequiredTimestamp(*value, field)
}

// fromOptionalTimestamp 保留缺失状态并解码可选时间
func fromOptionalTimestamp(value *timestamppb.Timestamp, field string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	result, err := fromRequiredTimestamp(value, field)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// FromWireDuration 解码可选时长并拒绝超出 time.Duration 的值，避免静默饱和截断
func FromWireDuration(value *durationpb.Duration, field string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	if err := value.CheckValid(); err != nil {
		return 0, decodeError(fmt.Sprintf("%s is invalid: %v", field, err))
	}

	result := value.AsDuration()
	roundTrip := durationpb.New(result)
	if roundTrip.Seconds != value.Seconds || roundTrip.Nanos != value.Nanos {
		return 0, decodeError(field + " exceeds time.Duration range")
	}
	return result, nil
}

// toStringList 通过消息包装保留字符串列表存在性
func toStringList(value []string) *StringList {
	if value == nil {
		return nil
	}

	return &StringList{Values: append([]string(nil), value...)}
}

// fromStringList 解码字符串列表并区分缺失与显式空
func fromStringList(value *StringList) []string {
	if value == nil {
		return nil
	}

	return append([]string{}, value.Values...)
}

// toBytesValue 通过消息包装保留字节载荷存在性
func toBytesValue(value []byte) *BytesValue {
	if value == nil {
		return nil
	}

	return &BytesValue{Value: append([]byte(nil), value...)}
}

// fromBytesValue 解码字节载荷并区分缺失与显式空
func fromBytesValue(value *BytesValue) []byte {
	if value == nil {
		return nil
	}

	return append([]byte{}, value.Value...)
}

// cloneString 复制可选字符串，避免共享可变指针
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

// cloneInt32 复制可选整数，保留显式零值
func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

// cloneFloat64 复制可选浮点数，保留显式零值
func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

// toCategory 编码错误类别，未知值归一为零值
func toCategory(value sdk.Category) Category {
	if value < sdk.CategoryUnknown || value > sdk.CategoryInternal {
		return Category_CATEGORY_UNKNOWN
	}

	return Category(value)
}

// fromCategory 解码错误类别，未知值归一为零值
func fromCategory(value Category) sdk.Category {
	if value < Category_CATEGORY_UNKNOWN || value > Category_CATEGORY_INTERNAL {
		return sdk.CategoryUnknown
	}

	return sdk.Category(value)
}

// toWriteOutcome 编码写结果，未知值归一为零值
func toWriteOutcome(value sdk.WriteOutcome) WriteOutcome {
	if value < sdk.OutcomeUnspecified || value > sdk.OutcomeUnknown {
		return WriteOutcome_WRITE_OUTCOME_UNSPECIFIED
	}

	return WriteOutcome(value)
}

// fromWriteOutcome 解码写结果，未知值归一为零值
func fromWriteOutcome(value WriteOutcome) sdk.WriteOutcome {
	if value < WriteOutcome_WRITE_OUTCOME_UNSPECIFIED || value > WriteOutcome_WRITE_OUTCOME_UNKNOWN {
		return sdk.OutcomeUnspecified
	}

	return sdk.WriteOutcome(value)
}

// toCompleteness 编码资料完整性，未知值归一为零值
func toCompleteness(value sdk.Completeness) Completeness {
	if value < sdk.CompletenessUnknown || value > sdk.CompletenessComplete {
		return Completeness_COMPLETENESS_UNKNOWN
	}

	return Completeness(value)
}

// fromCompleteness 解码资料完整性，未知值归一为零值
func fromCompleteness(value Completeness) sdk.Completeness {
	if value < Completeness_COMPLETENESS_UNKNOWN || value > Completeness_COMPLETENESS_COMPLETE {
		return sdk.CompletenessUnknown
	}

	return sdk.Completeness(value)
}

// ToWireError 转换业务错误
func ToWireError(value *sdk.Error) *Error {
	if value == nil {
		return nil
	}

	return &Error{
		Category:     toCategory(value.Category),
		Operation:    value.Operation,
		InvocationId: value.InvocationID,
		Outcome:      toWriteOutcome(value.Outcome),
		Reason:       value.Reason,
		RetryAfter:   durationpb.New(value.RetryAfter),
	}
}

// FromWireError 转换 wire 错误
func FromWireError(value *Error) (*sdk.Error, error) {
	if value == nil {
		return nil, nil
	}

	retryAfter, err := FromWireDuration(value.RetryAfter, "error.retry_after")
	if err != nil {
		return nil, err
	}

	return &sdk.Error{
		Category:     fromCategory(value.Category),
		Operation:    value.Operation,
		InvocationID: value.InvocationId,
		Outcome:      fromWriteOutcome(value.Outcome),
		Reason:       value.Reason,
		RetryAfter:   retryAfter,
	}, nil
}

// ToWirePageRequest 转换分页请求
func ToWirePageRequest(value sdk.PageRequest) (*PageRequest, error) {
	normalized, err := sdk.NormalizePageRequest("page", value)
	if err != nil {
		return nil, err
	}

	return &PageRequest{Limit: int32(normalized.Limit), Cursor: normalized.Cursor}, nil
}

// FromWirePageRequest 转换 wire 分页请求
func FromWirePageRequest(value *PageRequest) (sdk.PageRequest, error) {
	if value == nil {
		return sdk.NormalizePageRequest("page", sdk.PageRequest{})
	}

	return sdk.NormalizePageRequest("page", sdk.PageRequest{Limit: int(value.Limit), Cursor: value.Cursor})
}
