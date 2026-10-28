package transport

import (
	"context"
	"errors"
	"strings"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EncodeError 保留业务错误类别，并对普通插件错误使用脱敏说明
func EncodeError(err error, operation string) *wire.Error {
	if err == nil {
		return nil
	}
	var value *sdk.Error
	if errors.As(err, &value) && value != nil {
		return wire.ToWireError(value)
	}
	category := sdk.CategoryInternal
	if errors.Is(err, context.Canceled) {
		category = sdk.CategoryCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		category = sdk.CategoryDeadlineExceeded
	}
	value = sdk.NewError(category, operation, "plugin operation failed")
	if isWrite(operation) {
		value.Outcome = sdk.OutcomeUnknown
	}
	return wire.ToWireError(value)
}

// DecodeError 将业务错误或传输失败转换成可机器判断的公开错误
func DecodeError(value *wire.Error, err error, operation string) error {
	if err == nil {
		decoded, decodeErr := wire.FromWireError(value)
		if decodeErr != nil {
			return ResultError(false, nil, nil, operation)
		}
		if decoded == nil {
			return nil
		}
		return decoded
	}
	category := sdk.CategoryUnavailable
	for _, detail := range status.Convert(err).Details() {
		if business, ok := detail.(*wire.Error); ok {
			return DecodeError(business, nil, operation)
		}
	}
	code := status.Code(err)
	if errors.Is(err, context.Canceled) {
		code = codes.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = codes.DeadlineExceeded
	}
	switch code {
	case codes.Canceled:
		category = sdk.CategoryCanceled
	case codes.DeadlineExceeded:
		category = sdk.CategoryDeadlineExceeded
	case codes.Unimplemented:
		category = sdk.CategoryUnimplemented
	case codes.PermissionDenied:
		category = sdk.CategoryPermissionDenied
	case codes.ResourceExhausted:
		category = sdk.CategoryResourceExhausted
	case codes.InvalidArgument:
		category = sdk.CategoryInvalidArgument
	case codes.NotFound:
		category = sdk.CategoryNotFound
	case codes.AlreadyExists:
		category = sdk.CategoryAlreadyExists
	case codes.Aborted:
		category = sdk.CategoryConflict
	case codes.FailedPrecondition:
		category = sdk.CategoryFailedPrecondition
	case codes.Internal, codes.DataLoss, codes.Unknown:
		category = sdk.CategoryInternal
	}
	valueError := sdk.NewError(category, operation, "RPC transport failed")
	if isWrite(operation) {
		valueError.Outcome = sdk.OutcomeUnknown
	}
	return valueError
}

// isWrite 判断当前已开放的目标写入操作，传输失败不能降为确定失败或只读结果
func isWrite(operation string) bool {
	return strings.HasPrefix(operation, "messages.send_") || operation == "messages.reply" || operation == "messages.revoke" || operation == "plugin.capability" || operation == "caller.call"
}

// ResultError 拒绝没有业务结果的空成功，同时保留部分结果对应的原业务错误
func ResultError(present bool, value *wire.Error, err error, operation string) error {
	if err := DecodeError(value, err, operation); err != nil {
		return err
	}
	if present {
		return nil
	}
	result := sdk.NewError(sdk.CategoryInternal, operation, "missing business result")
	if isWrite(operation) {
		result.Outcome = sdk.OutcomeUnknown
	}
	return result
}

// RPCError 在通用 gRPC 拦截器中保留公开业务错误详情及机器类别
func RPCError(err error, operation string) error {
	value := EncodeError(err, operation)
	if value == nil {
		return nil
	}
	code := codes.Internal
	switch sdk.Category(value.Category) {
	case sdk.CategoryInvalidArgument:
		code = codes.InvalidArgument
	case sdk.CategoryPermissionDenied:
		code = codes.PermissionDenied
	case sdk.CategoryNotFound:
		code = codes.NotFound
	case sdk.CategoryAlreadyExists:
		code = codes.AlreadyExists
	case sdk.CategoryConflict:
		code = codes.Aborted
	case sdk.CategoryFailedPrecondition:
		code = codes.FailedPrecondition
	case sdk.CategoryResourceExhausted:
		code = codes.ResourceExhausted
	case sdk.CategoryUnimplemented:
		code = codes.Unimplemented
	case sdk.CategoryDeadlineExceeded:
		code = codes.DeadlineExceeded
	case sdk.CategoryCanceled:
		code = codes.Canceled
	case sdk.CategoryUnavailable:
		code = codes.Unavailable
	}
	result, detailErr := status.New(code, "host operation failed").WithDetails(value)
	if detailErr != nil {
		return status.Error(codes.Internal, "could not encode business error")
	}
	return result.Err()
}
