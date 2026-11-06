# Golem SDK

Golem 插件 SDK 定义 frame 与插件共享的公共模型、通信契约和插件运行时。SDK 不依赖
`github.com/golemwx/core`，也不公开 core 的 DTO、会话或配置文件。

## 当前状态

本修订包含 GOLEM-39 / R02 冻结契约、GOLEM-40 / R03 实例运行时、GOLEM-41 / R04 命令系统、
GOLEM-42 / R05 配置系统、GOLEM-43 / R06 数据目录和 GOLEM-44 / R07 日志桥接：

- 消息、联系人、群、事件、命令、配置、租约、能力和运行时公共签名已冻结
- Proto 是唯一 wire 来源，生成代码只存在于 `internal/wire`
- `plugin.Serve` 提供真实插件进程入口，`plugin.Start` 提供 frame 侧客户端
- Describe、Initialize、Load、Start、Stop、Unload、Host 反向连接和真实子进程收尾已实现
- 插件进程在业务初始化前取得安装目录与插件 ID 对应的 OS 排他锁
- 启用周期 `Base.Context()`、停用取消、在途 handler 等待和断连失效已实现
- Kong 结构体命令、统一 lexer、schema、Run/Reply 注入和 validator/v10 校验已实现
- `Config[T]` 默认值上报、严格 TOML、标签/业务校验、深拷贝、保存、SetConfig、
  待配置状态和变更回调已实现
- 实例级 `Host.Data` 惰性目录、相对路径/盘符/`..`/符号链接边界和重启读回已实现
- 插件 `log/slog` 经 hclog JSON stderr 进入 go-plugin，再由宿主 slog 输出；等级、属性、
  group 和可信 `plugin_id`/`plugin_name` 均保留

未实现行为必须返回 `CategoryUnimplemented`，不能伪装成功。最终跨仓库符合性由 R25
独立核对。

## 包结构

- `message`：完整消息、八类类型化数据、发送结果和消息 Host 能力
- `contact`、`chatroom`、`session`：联系人选择器、群资料和消费租约
- `event`：Topic、Observe/Consume 订阅和事件结果
- `command`：统一 lexer、Kong 结构体命令、schema、owner 上下文、validator 和 Reply
- `capability`：有界跨插件能力调用
- `plugin`：Metadata、可选生命周期、小型 Host、Config[T] 和 Runtime 签名
- `proto/golem/sdk`：唯一 Proto 源
- `internal/wire`：生成类型、服务桩和公开模型转换，不属于公共 API

## 可编译示例

最小可运行插件：

```go
package main

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
	return plugin.Metadata{ID: "example", Name: "示例"}
}

func (*Plugin) Subscriptions() []event.Subscription {
	return []event.Subscription{{Topic: event.Message, Mode: event.Observe}}
}

func (*Plugin) OnEvent(context.Context, event.Event) (event.Result, error) {
	return event.Result{}, nil
}

func main() {
	plugin.Serve(New())
}
```

`plugin.Serve` 只负责当前实例的进程入口和握手；frame 使用 `plugin.Start` 启动、
描述、初始化和推进生命周期。完整签名、锁和退出确认协议见
[公共契约](docs/公共契约.md)与[插件通信](docs/插件通信.md)。

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

- [公共契约](docs/公共契约.md)：公共 API、字段语义、生命周期、错误和服务形状
- [快速开始](docs/快速开始.md)：最小插件和 frame 启停流程
- [开发指南](docs/开发指南.md)：生成链、验证和后续任务分工
- [插件通信](docs/插件通信.md)：Proto 服务、流、单实例锁和 R10 退出确认
- [宿主能力与安全](docs/宿主能力与安全.md)：Host、owner、数据和错误规则
- [故障排查](docs/故障排查.md)：常见生成、依赖和契约问题
- [R02 验收记录](docs/R02验收记录.md)：条款、测试、命令和未运行项
- [R03 验收记录](docs/R03验收记录.md)：实例运行时、锁、子进程和未验证项
- [R04 验收记录](docs/R04验收记录.md)：命令树、lexer、校验、帮助和未验证项
- [R05 验收记录](docs/R05验收记录.md)：配置默认值、校验、保存、回调和未验证项
- [R06 验收记录](docs/R06验收记录.md)：数据目录惰性创建、路径边界和未验证项
- [R07 验收记录](docs/R07验收记录.md)：slog 桥接、可信身份、启动失败日志和未验证项
