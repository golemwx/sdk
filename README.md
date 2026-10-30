# Golem SDK

Golem 插件 SDK 定义插件与 frame 之间的稳定业务模型、RPC 协议、生命周期接口和宿主能力。
SDK 只定义公共契约，不依赖 `github.com/golemwx/core`。

## 包结构

- `plugin`：插件描述、生命周期、业务处理接口与自动绑定 Host 的 `Base`
- `event`、`command`、`capability`：事件、命令和跨插件能力契约
- `message`、`contact`、`chatroom`、`session`、`config`：领域模型和类型化 Host 客户端
- `rpc`：共享配对配置、真实插件启动、控制 RPC 和反向 Host 服务
- `internal/wire`：Proto 生成类型和转换，不属于插件公共 API
- `proto/golem/sdk`：唯一 Proto 协议源，不设置版本子目录

## 快速使用

```powershell
$env:GOWORK = "off"
$env:GOFLAGS = ""
task proto
task test
task lint
task build
```

插件入口调用 `plugin.Serve(implementation)`；需要宿主能力时嵌入零值 `plugin.Base`。
SDK 会在反向连接初始化后、`OnLoad` 之前自动绑定 `p.Host`。Host 字段仅供初始化后的
回调读取，不得改写；卸载或断连后旧句柄不可用。

配对配置由 SDK 唯一定义：`ProtocolVersion=1`、`MagicCookieKey=GOLEM_PLUGIN_MAGIC_COOKIE`、
`MagicCookieValue=golem-sdk-grpc`、PluginSet 键为 `golem`。普通发布和兼容的 Proto
新增字段保持这些值不变；只有不兼容的通信或业务语义变更才提高唯一协议版本。

## 中文文档

- [快速开始](docs/快速开始.md)：创建独立插件 module 和本地构建
- [开发指南](docs/开发指南.md)：生成链、版本、测试和模块边界
- [插件通信](docs/插件通信.md)：握手、生命周期、Host 绑定和资源所有权
- [公共契约](docs/公共契约.md)：字段存在性、错误类别和业务结果
- [宿主能力与安全](docs/宿主能力与安全.md)：权限、配置、资料目录和取消
- [故障排查](docs/故障排查.md)：生成、依赖、race 和 Host 问题

## 生成与验证

生成工具固定版本，不使用 `latest`：

```text
Buf                 1.75.0
protoc-gen-go       1.36.12
protoc-gen-go-grpc  1.6.2
```

`task proto` 依次执行格式化、格式检查、lint、生成、临时目录重复生成差异核对和 wire
测试。正式命令显式 `GOWORK=off GOFLAGS=`；`task ci` 额外执行 tidy 检查、全量测试、
vet 和 `CGO_ENABLED=0` 构建。race 通过独立的 `task test:race` 执行，需要 CGO 和 C
编译器。

## 错误与安全

跨进程错误使用 `*sdk.Error`。调用方只按 `Category` 和写操作的 `Outcome` 判断，不匹配
`Reason` 文本；未实现的方法返回 `CategoryUnimplemented`，不能返回空成功。所有 Host
回调都必须经过显式授权，缺少授权默认拒绝；`OutcomeUnknown` 的写操作不得自动重试。

插件业务签名只使用公开领域类型。frame 通过 `sdk/rpc` 接线，不直接导入
`internal/wire`；公共 API 不公开宿主路径、原始 XML 或 CDN 解密材料。
