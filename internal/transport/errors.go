package transport

import (
	"context"
	"errors"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NormalizeError 将内部错误限制为可跨进程传递的脱敏业务错误。
func NormalizeError(err error, operation string) *sdk.Error {
	if err == nil {
		return nil
	}
	var business *sdk.Error
	if errors.As(err, &business) && business != nil {
		copy := *business
		if copy.Operation == "" {
			copy.Operation = operation
		}
		return &copy
	}
	if errors.Is(err, context.Canceled) {
		return sdk.NewError(sdk.CategoryCanceled, operation, "operation was canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return sdk.NewError(sdk.CategoryDeadlineExceeded, operation, "operation deadline was exceeded")
	}
	switch status.Code(err) {
	case codes.Canceled:
		return sdk.NewError(sdk.CategoryCanceled, operation, "operation was canceled")
	case codes.DeadlineExceeded:
		return sdk.NewError(sdk.CategoryDeadlineExceeded, operation, "operation deadline was exceeded")
	case codes.InvalidArgument:
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "request was rejected")
	case codes.PermissionDenied, codes.Unauthenticated:
		return sdk.NewError(sdk.CategoryPermissionDenied, operation, "operation was denied")
	case codes.NotFound:
		return sdk.NewError(sdk.CategoryNotFound, operation, "resource was not found")
	case codes.FailedPrecondition, codes.AlreadyExists, codes.Aborted:
		return sdk.NewError(sdk.CategoryFailedPrecondition, operation, "operation precondition failed")
	case codes.ResourceExhausted:
		return sdk.NewError(sdk.CategoryResourceExhausted, operation, "operation exceeded a resource limit")
	case codes.Unimplemented:
		return sdk.NewError(sdk.CategoryUnimplemented, operation, "operation is not implemented")
	case codes.Unavailable:
		return sdk.NewError(sdk.CategoryUnavailable, operation, "service is unavailable")
	default:
		return sdk.NewError(sdk.CategoryInternal, operation, "transport operation failed")
	}
}

// EncodeError 将任意错误转换为真正的业务错误。
func EncodeError(err error, operation string) *wire.Error {
	business := NormalizeError(err, operation)
	if business == nil {
		return nil
	}
	encoded, conversionErr := wire.ToWireError(business)
	if conversionErr == nil {
		return encoded
	}
	fallback, _ := wire.ToWireError(sdk.NewError(sdk.CategoryInternal, operation, "error could not be encoded"))
	return fallback
}

// DecodeError 优先保留传输错误，其次解码响应中的业务错误。
func DecodeError(value *wire.Error, callErr error, operation string) error {
	if callErr != nil {
		return NormalizeError(callErr, operation)
	}
	if value == nil {
		return nil
	}
	decoded, err := wire.FromWireError(value)
	if err != nil {
		return sdk.NewError(sdk.CategoryInternal, operation, "host returned an invalid error")
	}
	return decoded
}
