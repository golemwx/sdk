# Golem SDK

Golem 插件 SDK 定义插件与 frame 之间的稳定业务模型、RPC 协议和宿主能力接口。

## 包结构

- `plugin`：插件描述、生命周期、业务处理接口与显式 `Host`
- `event`、`command`、`capability`：事件、命令和跨插件能力契约
- `message`、`contact`、`chatroom`、`session`、`config`：领域模型及类型化宿主客户端
- `rpc`：共享配对配置、真实插件启动、生命周期控制及实例绑定的反向宿主服务
- `internal/wire`：由 Proto 生成的传输类型和业务类型转换，不属于插件公共 API
- `proto/golem/sdk`：唯一 Proto 协议源，不设置版本子目录

## 生成与验证

生成工具和插件均固定版本，不使用 `latest`：

```powershell
task proto
gofmt -w <本次修改的 Go 文件>
go test ./...
```

`task proto` 使用 Buf 1.75.0、`protoc-gen-go` 1.36.12 和
`protoc-gen-go-grpc` 1.6.2，依次执行格式化、格式检查、lint、生成、临时目录重复生成差异核对和 wire 测试。

## 错误约定

跨进程错误使用 `*sdk.Error`。调用方只按 `Category` 和写操作的 `Outcome`
判断，不匹配 `Reason` 文本；未实现的方法返回 `CategoryUnimplemented`，不能返回空成功。

插件业务签名只使用公开领域类型。frame 通过 `sdk/rpc` 接线，不直接导入
`internal/wire`；SDK 不依赖 `github.com/golemwx/core`。

## 插件通信

插件入口调用 `plugin.Serve(implementation)`；宿主通过 `rpc.Start(ctx, options)`
启动实例，启动成功后必须显式 `Close`，或调用会同时回收资源的 `Unload`。
初始化按握手、Describe、宿主校验、反向服务、Initialize、反向就绪确认、OnLoad 的顺序执行。

配对配置由 SDK 唯一定义：`ProtocolVersion=1`、`MagicCookieKey=GOLEM_PLUGIN_MAGIC_COOKIE`、
`MagicCookieValue=golem-sdk-grpc`、PluginSet 键为 `golem`。普通发布与兼容的 Proto 新增字段
保持这些值不变；只有不兼容的通信或业务语义变更才提高唯一协议版本。

固定使用 `github.com/hashicorp/go-plugin v1.8.0`，仅启用 gRPC、AutoMTLS 和 SkipHostEnv。
七类 Host 服务共用一条反向连接，所有宿主回调都必须经过显式授权，缺少授权默认拒绝。
调用命令的 `Env` 只提供明确允许的变量，不会自动继承宿主环境。

详细用法、资源所有权和阶段边界见 [插件通信](docs/插件通信.md)。
T08 的业务 handler、invocation 透传、启用周期 context 和实际执行跟踪尚未接入；
业务处理、Drain 和执行完成确认明确返回 Unimplemented，媒体流在 T26 接入。
