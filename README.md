# Golem SDK

Golem 插件 SDK 定义 frame 与插件共享的公共模型和通信契约。SDK 不依赖
`github.com/golemwx/core`，也不公开 core 的 DTO、会话或配置文件。

## R02 状态

本修订对应 GOLEM-39 / R02：

- 已冻结消息、联系人、群、事件、命令、配置、租约、能力和运行时公共签名
- Proto 是唯一 wire 来源，生成代码只存在于 `internal/wire`
- 八类消息使用 Proto `oneof` 表达结构化 `Data`
- 不提供媒体、撤回或转发业务 token；不提供配置 revision/active/apply 状态
- 运行时、Kong 命令、配置注入、数据目录和日志桥接分别由 R03–R07 实现

R02 只交付可引用的契约，不是可直接运行的插件框架。当前没有 `plugin.Serve`，
Runtime 也没有生产实现；未实现行为必须返回 `CategoryUnimplemented`，不能伪装成功。

## 包结构

- `message`：完整消息、八类类型化数据、发送结果和消息 Host 能力
- `contact`、`chatroom`、`session`：联系人选择器、群资料和消费租约
- `event`：Topic、Observe/Consume 订阅和事件结果
- `command`：结构体命令 schema、调用输入、owner 上下文和 Reply
- `capability`：有界跨插件能力调用
- `plugin`：Metadata、可选生命周期、小型 Host、Config[T] 和 Runtime 签名
- `proto/golem/sdk`：唯一 Proto 源
- `internal/wire`：生成类型、服务桩和公开模型转换，不属于公共 API

## 可编译示例

下面的包只使用 R02 已存在的类型；它不会启动插件进程：

```go
package fixture

import (
	"context"

	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/plugin"
)

type Plugin struct {
	*plugin.Base
}

func New() *Plugin {
	return &Plugin{Base: plugin.NewBase()}
}

func (*Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{ID: "fixture", Name: "示例"}
}

func (*Plugin) Subscriptions() []event.Subscription {
	return []event.Subscription{{Topic: event.Message, Mode: event.Observe}}
}

func (*Plugin) OnEvent(context.Context, event.Event) (event.Result, error) {
	return event.Result{}, nil
}
```

完整签名和字段语义见[公共契约](docs/公共契约.md)。

## 生成与验证

固定工具版本：

```text
Buf                 1.75.0
protoc-gen-go       1.36.12
protoc-gen-go-grpc  1.6.2
```

```powershell
task proto
task deps:check
task test
task lint
task build
```

`task proto` 会格式化、lint、生成、在临时目录重复生成并核对字节一致性。
`task test:race` 需要 CGO 和 C 编译器；本机没有 GCC 时不得把 race 记为通过。

## 文档

- [公共契约](docs/公共契约.md)：R02 公共 API、字段语义、错误和服务形状
- [快速开始](docs/快速开始.md)：编译期示例和 R02 边界
- [开发指南](docs/开发指南.md)：生成链、验证和后续任务分工
- [插件通信](docs/插件通信.md)：Proto 服务、流和生命周期边界
- [宿主能力与安全](docs/宿主能力与安全.md)：Host、owner、数据和错误规则
- [故障排查](docs/故障排查.md)：常见生成、依赖和契约问题
- [R02 验收记录](docs/R02验收记录.md)：条款、测试、命令和未运行项
