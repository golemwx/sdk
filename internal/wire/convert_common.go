package wire

import (
	"math"
	"time"

	"github.com/golemwx/sdk"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ToWireError 将公开错误转换为 wire 错误。
func ToWireError(value *sdk.Error) (*Error, error) {
	if value == nil {
		return nil, nil
	}
	if value.RetryAfter < 0 {
		return nil, invalidArgument("sdk.error.retry_after", "retry_after must not be negative")
	}
	result := &Error{
		Category:  toWireCategory(value.Category),
		Operation: value.Operation,
		Outcome:   toWireOutcome(value.Outcome),
		Reason:    value.Reason,
	}
	if value.RetryAfter != 0 {
		result.RetryAfter = durationpb.New(value.RetryAfter)
	}
	return result, nil
}

// FromWireError 将 wire 错误转换为公开错误。
func FromWireError(value *Error) (*sdk.Error, error) {
	if value == nil {
		return nil, nil
	}
	retryAfter, err := fromWireDuration(value.GetRetryAfter(), "sdk.error.retry_after")
	if err != nil {
		return nil, err
	}
	result := &sdk.Error{
		Category:   fromWireCategory(value.GetCategory()),
		Operation:  value.GetOperation(),
		Outcome:    fromWireOutcome(value.GetOutcome()),
		Reason:     value.GetReason(),
		RetryAfter: retryAfter,
	}
	return result, nil
}

func toWireCategory(value sdk.Category) Category {
	switch value {
	case sdk.CategoryInvalidArgument:
		return Category_CATEGORY_INVALID_ARGUMENT
	case sdk.CategoryPermissionDenied:
		return Category_CATEGORY_PERMISSION_DENIED
	case sdk.CategoryNotFound:
		return Category_CATEGORY_NOT_FOUND
	case sdk.CategoryFailedPrecondition:
		return Category_CATEGORY_FAILED_PRECONDITION
	case sdk.CategoryResourceExhausted:
		return Category_CATEGORY_RESOURCE_EXHAUSTED
	case sdk.CategoryUnimplemented:
		return Category_CATEGORY_UNIMPLEMENTED
	case sdk.CategoryDeadlineExceeded:
		return Category_CATEGORY_DEADLINE_EXCEEDED
	case sdk.CategoryCanceled:
		return Category_CATEGORY_CANCELED
	case sdk.CategoryUnavailable:
		return Category_CATEGORY_UNAVAILABLE
	case sdk.CategoryInternal:
		return Category_CATEGORY_INTERNAL
	default:
		return Category_CATEGORY_UNKNOWN
	}
}

func fromWireCategory(value Category) sdk.Category {
	switch value {
	case Category_CATEGORY_INVALID_ARGUMENT:
		return sdk.CategoryInvalidArgument
	case Category_CATEGORY_PERMISSION_DENIED:
		return sdk.CategoryPermissionDenied
	case Category_CATEGORY_NOT_FOUND:
		return sdk.CategoryNotFound
	case Category_CATEGORY_FAILED_PRECONDITION:
		return sdk.CategoryFailedPrecondition
	case Category_CATEGORY_RESOURCE_EXHAUSTED:
		return sdk.CategoryResourceExhausted
	case Category_CATEGORY_UNIMPLEMENTED:
		return sdk.CategoryUnimplemented
	case Category_CATEGORY_DEADLINE_EXCEEDED:
		return sdk.CategoryDeadlineExceeded
	case Category_CATEGORY_CANCELED:
		return sdk.CategoryCanceled
	case Category_CATEGORY_UNAVAILABLE:
		return sdk.CategoryUnavailable
	case Category_CATEGORY_INTERNAL:
		return sdk.CategoryInternal
	default:
		return sdk.CategoryUnknown
	}
}

func toWireOutcome(value sdk.WriteOutcome) WriteOutcome {
	switch value {
	case sdk.OutcomeNotApplicable:
		return WriteOutcome_WRITE_OUTCOME_NOT_APPLICABLE
	case sdk.OutcomeSucceeded:
		return WriteOutcome_WRITE_OUTCOME_SUCCEEDED
	case sdk.OutcomeFailed:
		return WriteOutcome_WRITE_OUTCOME_FAILED
	case sdk.OutcomeUnknown:
		return WriteOutcome_WRITE_OUTCOME_UNKNOWN
	default:
		return WriteOutcome_WRITE_OUTCOME_UNSPECIFIED
	}
}

func fromWireOutcome(value WriteOutcome) sdk.WriteOutcome {
	switch value {
	case WriteOutcome_WRITE_OUTCOME_NOT_APPLICABLE:
		return sdk.OutcomeNotApplicable
	case WriteOutcome_WRITE_OUTCOME_SUCCEEDED:
		return sdk.OutcomeSucceeded
	case WriteOutcome_WRITE_OUTCOME_FAILED:
		return sdk.OutcomeFailed
	case WriteOutcome_WRITE_OUTCOME_UNKNOWN:
		return sdk.OutcomeUnknown
	default:
		return sdk.OutcomeUnspecified
	}
}

// ToWireBytes 保留 nil 与显式空字节的差异。
func ToWireBytes(value []byte) *BytesValue {
	if value == nil {
		return nil
	}
	return &BytesValue{Value: cloneBytes(value)}
}

// FromWireBytes 保留 nil 与显式空字节的差异。
func FromWireBytes(value *BytesValue) []byte {
	if value == nil {
		return nil
	}
	if value.GetValue() == nil {
		return []byte{}
	}
	return cloneBytes(value.GetValue())
}

func toWireTimestamp(value time.Time) (*timestamppb.Timestamp, error) {
	if value.IsZero() {
		return nil, invalidArgument("wire.timestamp", "timestamp is required")
	}
	result := timestamppb.New(value.UTC())
	if err := result.CheckValid(); err != nil {
		return nil, invalidArgument("wire.timestamp", "timestamp is out of range")
	}
	return result, nil
}

func toWireOptionalTimestamp(value *time.Time) (*timestamppb.Timestamp, error) {
	if value == nil {
		return nil, nil
	}
	return toWireTimestamp(*value)
}

func toWireDuration(value time.Duration, operation string) (*durationpb.Duration, error) {
	if value < 0 {
		return nil, invalidArgument(operation, "duration must not be negative")
	}
	result := durationpb.New(value)
	if err := result.CheckValid(); err != nil {
		return nil, invalidArgument(operation, "duration is out of range")
	}
	return result, nil
}

func fromWireTimestamp(value *timestamppb.Timestamp, operation string) (time.Time, error) {
	if value == nil {
		return time.Time{}, invalidArgument(operation, "timestamp is required")
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, invalidArgument(operation, "timestamp is out of range")
	}
	return value.AsTime().UTC(), nil
}

func fromWireOptionalTimestamp(value *timestamppb.Timestamp, operation string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	result, err := fromWireTimestamp(value, operation)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func fromWireDuration(value *durationpb.Duration, operation string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	if err := value.CheckValid(); err != nil {
		return 0, invalidArgument(operation, "duration is out of range")
	}
	const (
		maxSeconds = int64(math.MaxInt64 / int64(time.Second))
		maxNanos   = int32(math.MaxInt64 % int64(time.Second))
	)
	if value.GetSeconds() > maxSeconds ||
		(value.GetSeconds() == maxSeconds && value.GetNanos() > maxNanos) {
		return 0, invalidArgument(operation, "duration is out of time.Duration range")
	}
	result := value.AsDuration()
	if result < 0 {
		return 0, invalidArgument(operation, "duration must not be negative")
	}
	return result, nil
}

func toWireInt32(value int, operation string) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, invalidArgument(operation, "integer is out of range")
	}
	return int32(value), nil
}

func invalidArgument(operation, reason string) error {
	return sdk.NewError(sdk.CategoryInvalidArgument, operation, reason)
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte{}, value...)
}

func cloneStrings(value []string) []string {
	if value == nil {
		return nil
	}
	return append([]string{}, value...)
}
