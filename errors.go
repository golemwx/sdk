package sdk

import (
	"fmt"
	"time"
)

// Category 是跨插件边界可稳定判断的错误类别。
type Category int

const (
	// CategoryUnknown 表示错误类别未知。
	CategoryUnknown Category = iota
	// CategoryInvalidArgument 表示命令、配置或业务参数无效。
	CategoryInvalidArgument
	// CategoryPermissionDenied 表示调用方无权执行操作。
	CategoryPermissionDenied
	// CategoryNotFound 表示联系人、插件、命令或能力不存在。
	CategoryNotFound
	// CategoryFailedPrecondition 表示插件状态、账号状态或消息字段不满足前置条件。
	CategoryFailedPrecondition
	// CategoryResourceExhausted 表示队列、并发或载荷大小超过限制。
	CategoryResourceExhausted
	// CategoryUnimplemented 表示契约已定义但当前实现尚未提供。
	CategoryUnimplemented
	// CategoryDeadlineExceeded 表示调用超过截止时间。
	CategoryDeadlineExceeded
	// CategoryCanceled 表示调用被取消。
	CategoryCanceled
	// CategoryUnavailable 表示 core、插件进程或连接暂不可用。
	CategoryUnavailable
	// CategoryInternal 表示内部约束被违反或上游响应不符合契约。
	CategoryInternal
)

// WriteOutcome 描述有副作用写入是否已经发生。
type WriteOutcome int

const (
	// OutcomeUnspecified 表示结果未知，不能当作成功或失败。
	OutcomeUnspecified WriteOutcome = iota
	// OutcomeNotApplicable 表示只读操作。
	OutcomeNotApplicable
	// OutcomeSucceeded 表示上游明确确认写入成功。
	OutcomeSucceeded
	// OutcomeFailed 表示已确认目标写入未发生。
	OutcomeFailed
	// OutcomeUnknown 表示写入可能已生效，调用方不得自动重试。
	OutcomeUnknown
)

// Error 是可跨进程传递的脱敏业务错误。
//
// 调用方必须依据 Category 和 Outcome 分支，不得匹配 Reason 文本。
type Error struct {
	Category   Category
	Operation  string
	Outcome    WriteOutcome
	Reason     string
	RetryAfter time.Duration
}

// Error 返回供人阅读的脱敏错误说明。
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

// NewError 创建不涉及写副作用的业务错误。
func NewError(category Category, operation, reason string) *Error {
	return &Error{
		Category:  category,
		Operation: operation,
		Outcome:   OutcomeNotApplicable,
		Reason:    reason,
	}
}
