package sdk

import (
	"fmt"
	"time"
)

// Category 可机器判断的错误类别
type Category int

const (
	// CategoryUnknown 未知错误类别
	CategoryUnknown Category = iota
	// CategoryInvalidArgument 请求参数无效
	CategoryInvalidArgument
	// CategoryPermissionDenied 调用方无权执行操作
	CategoryPermissionDenied
	// CategoryNotFound 请求的业务对象或能力不存在
	CategoryNotFound
	// CategoryAlreadyExists 目标业务对象已存在
	CategoryAlreadyExists
	// CategoryConflict 修订号或业务状态冲突
	CategoryConflict
	// CategoryFailedPrecondition 未初始化、引用失效或其他业务前置条件不满足
	CategoryFailedPrecondition
	// CategoryResourceExhausted 调用额度或资源预算耗尽
	CategoryResourceExhausted
	// CategoryUnimplemented 声明的方法尚未实现
	CategoryUnimplemented
	// CategoryDeadlineExceeded 调用已超过截止时间
	CategoryDeadlineExceeded
	// CategoryCanceled 调用已取消
	CategoryCanceled
	// CategoryUnavailable 依赖或服务暂不可用
	CategoryUnavailable
	// CategoryInternal 内部错误或上游响应违反契约
	CategoryInternal
)

// Error 跨进程错误载体
type Error struct {
	// Category 机器判断的错误类别，不通过匹配 Reason 判断
	Category Category
	// Operation 发生错误的业务操作名
	Operation string
	// InvocationID 关联执行的唯一标识
	InvocationID string
	// Outcome 目标写入副作用结果，只读操作使用 NotApplicable
	Outcome WriteOutcome
	// Reason 供人阅读的脱敏错误说明，不用于机器分支判断
	Reason string
	// RetryAfter 建议重试等待时长，不构成自动重试写操作的授权
	RetryAfter time.Duration
}

// Error 返回脱敏的错误说明
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Operation == "" {
		return e.Reason
	}
	if e.Reason == "" {
		return fmt.Sprintf("%s: category %d", e.Operation, e.Category)
	}

	return e.Operation + ": " + e.Reason
}

// NewError 创建可跨进程传递的业务错误
func NewError(category Category, operation, reason string) *Error {
	return &Error{
		Category:  category,
		Operation: operation,
		Outcome:   OutcomeNotApplicable,
		Reason:    reason,
	}
}
