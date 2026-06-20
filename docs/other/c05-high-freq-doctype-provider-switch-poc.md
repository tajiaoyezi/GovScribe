# c05 高频文种 c01 后端切换 PoC（8.2）

## 结论

c05 生成编排已通过 c01 窄抽象在 `direct` 后端下跑通完整生成与流式输出，本 PoC 未修改 c05 生产代码。`internal/draft` 的生产导入图仍不得出现 OpenAI / Anthropic SDK 或 LiteLLM 专有客户端依赖，c05 只依赖 `internal/llm` 的窄抽象接口。

本 PoC 仅证明 c01 从 LiteLLM Proxy 切到 Go direct backend 时，c05 编排层不用改业务代码且 5.2 / 5.4 行为仍成立；不替代 7.3 的真实范文与目标模型校准，不替代 8.1 国产模型效果摸底，也不替代 8.3 国产 CPU / OS 端到端运行验证。

## PoC 方法

| 验证点 | 方法 | 证据 |
| --- | --- | --- |
| c05 complete 编排可在 c01 direct 后端运行 | `runtime.BackendFactory` 选择 `BackendDirect`，后端实现为 `direct.NewClient()`；用本地 `httptest` OpenAI-compatible `/chat/completions` 端点返回完整响应。 | `TestHighFreqDraftOrchestratorCompleteRunsThroughC01DirectBackend` |
| c05 SSE 流式编排可在 c01 direct 后端运行 | 同一 direct 后端通过 OpenAI-compatible SSE chunk 返回 `delta` 与 `done`，c05 继续复用 c01 三类归一事件并在流首 / 流尾追加 c05 元数据。 | `TestHighFreqDraftOrchestratorStreamRunsThroughC01DirectBackend` |
| c05 生产代码不耦合 direct SDK 或 LiteLLM | `TestDraftImportGraphHasNoConcreteModelSDKs` 继续扫描 `./internal/draft` 生产依赖图，拒绝 `openai-go` / `anthropic-sdk-go` / `litellm`。 | `internal/draft/architecture_test.go` |

## 边界

- 本 PoC 使用本地假 OpenAI-compatible 服务承接 direct SDK 请求，避免引入外部 API key 或真实模型输出。
- direct SDK 代码路径来自 c01 的 `internal/llm/direct.Client`，c05 不直接 import SDK。
- 本 PoC 不产生采纳率、速度阈值、国产模型质量或国产硬件可运行性结论。
