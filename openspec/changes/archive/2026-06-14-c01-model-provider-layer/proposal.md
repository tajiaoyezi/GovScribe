## Why

GovScribe 的全部生成能力都依赖大模型，而交付环境从公网（OpenAI / Anthropic）一路延伸到信创 / 私有化（国产或私有兼容端点），供应商、`base_url`、`api_key`、`model` 都必须由管理员灵活配置并随时切换（PRD 里程碑 1、Decisions「模型接入」）。如果让业务代码直接耦合某家 SDK 或某个 Proxy，信创阶段就要重写调用链。本 change 先把"如何可靠地把请求发给一个可配置模型并流式返回"这件最底层的事收敛成一层窄抽象，让上层（脱敏网关、密级路由、文种生成、RAG）只面向稳定接口编程，后续换后端不动业务代码。

## What Changes

- 新增一层 Go **窄抽象接口**封装大模型供应商（对齐 ADR-0001 D1），上层业务只依赖该接口，不直接 import 任何具体 SDK；接口覆盖文本生成（chat completion）与流式生成两种调用形态。
- MVP 阶段提供一个**经 LiteLLM Proxy** 的后端实现，统一对接 OpenAI / Anthropic / 私有 OpenAI 兼容端点；信创 / 私有化阶段可切换为 **Go 直连官方 SDK** 的后端实现（`openai-go` 导入路径必须带 `/v3`；`anthropic-sdk-go` 需 Go 1.24+，详见 ADR-0001 D3），切换对业务代码透明。
- 模型配置（供应商类型、`base_url`、`api_key`、`model` 及启用状态）作为权威数据落 **PostgreSQL**（ADR-0001 D5），由管理员维护；支持**运行时切换**当前生效配置，无需重启应用层。
- 新增模型**连通性测试**能力：保存 / 切换配置前可对目标端点发起一次最小探活调用，返回可用性与基础错误归因，避免把不可用配置推上线。
- 统一**流式归一**：接入层负责把不同后端的流式分片归一为统一事件 / 增量契约（对齐 ADR-0001 D2），供上层 c05 / c07 的生成端点封装并向前端透传，本 change 不持有面向前端写作工作台的生成端点；显式吸收"Anthropic 经 LiteLLM 的 OpenAI 格式流式存在字段映射损耗 / 丢失"等已核验坑（ADR-0001 D3），用宽松 JSON 接收非标字段。
- 抽象接口预留**启动期后端选择**（LiteLLM 后端 / Go 直连后端，随发布事件选定、非运行时热切），使 MVP→信创的后端替换成为启动配置项而非代码改造；运行时热切仅指模型配置。
- 向 c02 暴露"按请求单次定向到指定（私有）供应商配置出站、不改全局、无可用私有配置时返回明确拒绝信号"的能力（对应 `model-provider-abstraction` 同名 Requirement）。

不在本 change 范围：脱敏与密级路由判定（c02）、RAG 检索（c03）、embedding / rerank 等非生成类模型调用。本 change 只保证"把请求可靠发给可配置模型并流式返回结果"。

## Capabilities

### New Capabilities

- `model-provider-abstraction`: 定义 GovScribe 调用大模型的统一窄抽象接口及其后端可替换契约——上层只面向接口编程，MVP 走 LiteLLM Proxy 后端、信创阶段走 Go 直连官方 SDK 后端，二者行为对业务等价，切换不改业务代码。
- `model-config-management`: 模型接入配置（供应商类型 / `base_url` / `api_key` / `model` / 启用状态）的持久化与生命周期管理——落 PostgreSQL 作为权威源，支持管理员增改、运行时切换当前生效配置、以及保存 / 切换前的连通性探活测试。
- `model-streaming-passthrough`: 模型生成结果的统一流式归一契约——把不同后端的流式分片归一为统一事件 / 增量契约（供 c05 / c07 的生成端点封装后向前端透传），并规定流中断、后端字段损耗等异常下的可观察行为；本 change 不持有面向前端写作工作台的生成端点。

### Modified Capabilities

（无。本 change 为最底层基础设施，`openspec/specs/` 下尚无既有 capability，不修改任何现有规格。）

## Impact

- **保密红线 / 密级路由 / 脱敏**：本 change **不触及**脱敏与密级路由判定，相关逻辑由 c02 在本接入层之上叠加；接入层须把"当前生效供应商是公网还是私有"作为可读状态暴露给 c02，以支撑其密级路由与 fail-closed 决策；并向 c02 暴露"按请求单次定向到指定（私有）供应商配置出站、不改全局、无可用私有配置时返回明确拒绝信号"的能力（对应 `model-provider-abstraction` 同名 Requirement）；除"读公网 / 私有状态""按请求定向私有配置"外，再向 c02 暴露"按请求承载并透传出站内容密级供其密级路由（c01 不解读）"的能力——窄抽象调用入参承载出站请求内容密级（取值：非密 / 敏感 / 涉密；允许缺省为「缺失/未知」），c01 原样透出供 c02 拦截点读取（对应 `model-provider-abstraction`「按请求承载出站内容密级供 c02 拦截点读取」Requirement）。落在 **MVP 范围内**（PRD 里程碑 1 上半）。
- **代码 / 接口**：新增 Go 模型供应商抽象包及其两个后端实现（LiteLLM Proxy 后端、Go 直连 SDK 后端）；新增管理端"模型接入配置"读写与连通性测试 API；新增把后端流式分片归一为统一事件 / 增量契约的接入层（供 c05 / c07 的生成端点封装），不含面向前端写作工作台的生成端点（归 c05 / c07）。
- **依赖**：MVP 引入 LiteLLM Proxy（Python + Postgres，非单二进制，信创阶段退场）；信创阶段引入 `github.com/openai/openai-go/v3`（Apache-2.0）与 `github.com/anthropics/anthropic-sdk-go`（MIT，Go 1.24+）。版本 / 许可一律以 ADR-0001 D3 及技术栈版本表为准。依赖 c04 rbac-authorization（`model.config` 权限点）对模型配置管理端入口做鉴权，本 change 不自建鉴权。
- **数据 / 系统**：PostgreSQL 新增模型配置表（含 `api_key` 等敏感字段，需妥善保管，不入审计明文）；LiteLLM 运行时热切换有"对在途流不生效、多 worker 约 30s 传播延迟、Redis 缓存 TTL 陈旧期"等已知行为（ADR-0001 D3），切换语义须据此设计。
- **上层依赖方**：后续 c02（脱敏 / 密级路由）、文种生成、RAG 编排均以本 change 的抽象接口为唯一模型调用入口。
