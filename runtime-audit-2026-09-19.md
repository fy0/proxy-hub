# 调度与服务创建审计

日期：2026-09-19

本轮重点检查动态调度、运行时实例生命周期、SOCKS/HTTP 监听地址、节点更新、健康记录、API 同步辅助函数和前端未使用依赖。以下功能问题均已修复；不代表对所有代理协议及所有部署环境的完整认证。

## 已确认并修复

### P1：并发同步重复绑定端口并遗留监听

`service/proxy/runtime_lifecycle.go` 原先只保护实例表的读写，没有保护“读取实例、创建或替换实例、登记结果”这一整个操作。

同一映射并发同步时，多个请求会创建相同监听地址。后完成的失败路径可能删除成功实例的登记，最终状态显示端口占用，停止服务后仍不能重新绑定端口。

修复：串行化运行时变更；共享内部同步/移除入口避免重复加锁；动态同步失败时关闭被移出实例表的实例。

复现与回归：`TestConcurrentRuntimeSyncDoesNotLeakListener`。修复前出现 `listen port is already in use`，且 `RuntimeStop` 后重新绑定失败。

### P1：运行时错误继承创建请求的取消信号

`service/proxy/runtime_plan.go` 创建长期运行实例时直接使用调用方上下文。请求结束或健康同步的超时上下文被取消后，端口仍可能存在，但后续 SOCKS 请求失败。

修复：长期实例使用 `context.WithoutCancel`，由 `Core.Close` 管理生命周期；临时健康探测仍保留自己的超时。

复现与回归：`TestRuntimeOutlivesCreationContext`。修复前取消创建上下文后，本地 HTTP 请求经 SOCKS 返回 `general SOCKS server failure`。

### P1：修改节点地址或凭据没有更新实际出口

`syncDynamicGroupMembers` 按节点 ID 判断成员已存在，`Core.CreateOutbound` 也直接复用已有标签。更新数据库后，已有出口仍使用旧配置。

修复：运行时保存已应用的出口配置；相同标签的配置改变时重建受影响映射的实例。只增减成员或调整组策略仍走原有动态同步路径。

代价：修改实际出口连接参数会中断该映射当前连接。其余映射不受影响。

复现与回归：`TestRuntimeSyncAppliesUpdatedNodeConfiguration`，并保留动态增减成员不替换实例的原有测试。

### P2：策略刷新重置轮询位置，并发读取调度器不安全

`core/singboxcore/group.go` 每次更新策略都重新创建调度器，即使只变更健康探测间隔，也会重新从首个节点开始。候选选择在锁外读取可被替换的调度器字段。

修复：策略类型不变时保留调度器及轮询位置；策略、候选和调度器在同一把锁下取得快照。延迟优选的回退轮询也采用相同处理。

验证：`TestPolicyUpdatePreservesRoundRobinPosition`、`TestConcurrentPolicyUpdateAndSelection`，以及竞态检测。

### P2：监听地址解析重复且 IPv6 测速地址错误

创建、备份恢复、运行时监听分别校验地址；`localhost`、`[::1]` 被当作非法地址。测速函数先将 `::` 转为 `127.0.0.1`，使后续 IPv6 分支无法处理常见输入；监听信息直接拼接冒号，也没有正确表示 IPv6 的端口。

修复：复用地址解析，接受 `localhost` 和带方括号的 IPv6，持久化规范化 IP；IPv6 通配地址的测速使用 `::1`；使用 `net.JoinHostPort` 构造地址。

验证：`TestMappingProbeProxyURLAddressFamily`、`TestSOCKSMappingListenAddresses`。本机实际验证 `127.0.0.1`、`localhost`、`::1`、`[::1]` 的 SOCKS 服务启动与 TCP 连接。

没有复现“所有非 0.0.0.0 地址都会失败”。监听地址必须属于服务所在主机或容器的网络空间；绑定回环地址也不意味着可以从其他机器连接。原始故障未提供具体地址和部署方式，因此不能把上述修复视为对原始环境的完整复现。

### P2：解除黑名单没有恢复运行时成员

`NodeRelease` 原先仅更新健康记录。自动策略已经移出的节点不会随解除操作重新进入候选列表。

修复：解除后同步受影响映射。手动指定策略继续忽略健康黑名单。

验证：在 `TestNodeBlacklistSyncRemovesNodeFromRuntimeGroup` 中验证拉黑后移出、解除后恢复。

### P2：探测关闭与回调生命周期不一致

原有竞态测试曾出现后台探测在测试数据库关闭后继续触发运行时同步的崩溃。手动探测和后台探测轮次也没有串行保护。

修复：串行化探测轮次；探测与流量失败记录携带实例上下文，在取消后停止处理；进入运行时同步时再次检查取消；修正快速切换策略时探测循环可能无法重新启动的窗口。

## 死代码与重复逻辑

- 删除 Staticcheck 确认未使用的 `optionOutboundBlock`、`probeNode`、`groupNameMapForRouteHop`、`parseVMessURI`、`clashProxyURIs`。
- 删除空循环、无效初始赋值和终止循环前的无效赋值。
- 将映射和组重复的健康策略配置合并到 `runtimePolicy`，移除仅包装一次条件判断的三个函数。
- 删除没有生产调用的 `reloadRuntimeAfterMutation`，将其测试改为验证实际重载处理器的失败状态。
- 清理永远返回 `nil` 的 API 同步函数及调用方不可达错误分支；去重统一使用现有 `proxyuri.UniqueNonEmpty`。
- 删除未被业务页面引用的 Vue 欢迎页、示例组件、五个配套图标及计数器 store，共九个文件。
- 调整 HTTP 探测函数的返回值顺序，并显式关闭临时 Transport 的空闲连接。

## 本机其他网卡地址实测

补充执行 `TestSOCKSMappingLocalInterfaces`，通过环境变量 `PROXYHUB_TEST_LISTEN_ADDRESSES` 传入 Windows 中处于 Preferred 状态的全部非回环地址。此测试默认跳过，显式启用才会监听实际网卡；使用内存数据库、随机临时端口和随机 SOCKS 密码。

以下七个 IPv4 地址全部通过：

| 网卡 | 地址 |
| --- | --- |
| vEthernet (上网) | `192.168.123.10` |
| ZeroTier One | `10.11.12.3` |
| vEthernet (虚拟内网) | `192.168.137.1` |
| vEthernet (Default Switch) | `192.168.176.1` |
| vEthernet (WSL) | `172.27.80.1` |
| et_10_di2a | `10.126.126.100` |
| et_10_q6qv | `10.127.127.10` |

这七个网卡对应的 IPv6 链路本地地址（包含作用域 ID）也全部通过，共 14 个地址。每个地址均验证：系统绑定、映射创建、运行时启动、SOCKS5 认证协商、认证后的本地 HTTP 请求转发和停止后的端口释放。

未使用已断开网卡的 Tentative/Deprecated 地址。结果证明当前代码可使用本机有效的非通配地址；测试流量来自本机，不覆盖其他主机访问时的防火墙、路由或 NAT 路径。

## 验证与边界

通过 `go test ./...`、`go test -race ./core/singboxcore ./service/proxy ./api/proxy`、`go vet ./...`、Staticcheck 2025.1.1、前端 `pnpm run type-check` 和 `git diff --check`。

没有启动前端开发服务器，没有修改生成的静态资源。依赖外部代理凭据的可选集成测试仍需要对应环境变量；未对 Docker、公网映射或真实上游代理执行部署验证。

API 仍保留原有“配置保存成功与运行时启动状态分开”的行为，绑定失败通过运行时 `Failures` 返回。本轮未改写该接口契约，也没有为一般性结构问题引入新的框架。JSON 关系扫描及大型页面的职责划分仍可独立评估。
