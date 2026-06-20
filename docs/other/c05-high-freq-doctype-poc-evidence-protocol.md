# c05 高频文种 8.1 / 8.3 PoC 证据协议

建立日期：2026-06-20

## 目标

本协议用于把 c05 剩余高风险 PoC 变成可审计证据链：

- 8.1：国产 / 私有化模型在 9 个高频文种上的效果摸底。
- 8.3：龙芯 LoongArch64 与 ARM64 + 麒麟下生成编排服务端到端运行验证。

当前状态：尚未取得目标国产 / 私有化模型端点，也尚未取得龙芯 LoongArch64 或 ARM64 + 麒麟运行环境。因此本协议只建立记录格式与机器闸门，不得据此勾选 8.1 或 8.3。

## 边界

- 8.1 必须走 c05 真实编排路径：c06 场景上下文、c03 可检索脱敏样例、c05 结构契约、c01 窄抽象模型调用。
- 8.1 不得用 7.1 种子样稿分数或本地假服务响应替代国产 / 私有化模型输出。
- 8.1 的人工评分口径复用 7.1 rubric 四维与采纳标签，不能只记录模型连通性。
- 8.3 必须在目标平台真实运行：龙芯 LoongArch64 至少一次，ARM64 + 麒麟至少一次。
- 8.3 必须验证端到端依赖连通：PostgreSQL、MinIO、c01、c03、SSE 流式完成。仅交叉编译成功或本机 Windows / x86_64 运行不算通过。
- 表内只记录脱敏后的输出引用、运行日志引用或环境证明引用，不记录 API key、原文路径、裸 `各类文件` 目录名、原文标题、正文或原始 Office / PDF 文件引用（如 `.doc` / `.docx` / `.pdf` / `.xlsx` / `.et`）。`output_ref`、`evidence_ref`、`evidence_refs`、`model_endpoint_evidence_ref`、`platform_fingerprint_ref` 可以指向已脱敏审计包或脱敏输出对象，但不得指向原始语料路径。

## 记录文件

- `docs/other/c05-high-freq-doctype-private-model-runs.csv`：国产 / 私有化模型真实运行记录。
- `docs/other/c05-high-freq-doctype-private-model-reviews.csv`：人工 rubric 评分与采纳标签。
- `docs/other/c05-high-freq-doctype-private-model-decisions.csv`：各文种国产 / 私有化模型达标结论。
- `docs/other/c05-high-freq-doctype-xinchuang-runtime-runs.csv`：目标 CPU / OS 端到端运行记录。
- `docs/other/c05-high-freq-doctype-xinchuang-runtime-decisions.csv`：目标平台运行通过 / 未通过结论。

## 8.1 机器闸门

`private-model-runs.csv` 一旦填写，必须满足：

- `doctype` 属于 c05 9 个高频文种。
- `deployment_scope` 必须为 `private` / `domestic` / `xinchuang_private` 之一。
- `model_endpoint_evidence_ref` 必须指向真实国产 / 私有化模型端点、部署清单或供应商网关证明引用；不得为 `fake`、`mock` / `mocked`、`stub`、`dummy`、`httptest`、`localhost`、`127.0.0.1`、`unit-test`、`local-model`、`dev-server`、`test-endpoint` 等本地假服务或单测证据。
- `c03_query_id` 必须指向 c03 检索证据，且必须匹配同文种候选素材表中 `gate_status=ready_for_model_run` 的 `c03_query_ref`；不能是 `pending`、本地路径或 `各类文件/`。
- 成功运行必须有正数 `first_token_ms`、`total_generation_ms`、`completion_chars` 和脱敏 `output_ref`；失败运行必须记录 `error_reason`。

`private-model-reviews.csv` 一旦填写，必须满足：

- `run_id` 指向已存在的模型运行记录。
- 四维评分为 1-5。
- `adoption_status` 仅允许 `直接用` / `小改` / `大改` / `弃用`，且 `counts_as_adopted` 与 PRD 口径一致。
- `meets_like_govdoc` 必须为布尔值，表示该输出是否达到“像公文”的最低判断。

`private-model-decisions.csv` 一旦声明 `pass` / `fail`，必须满足：

- `evidence_refs` 使用 `run:<run_id>;review:<review_record_id>` 格式。
- `model_profile` 必须使用 `<model_provider>/<model_name>/<model_backend>` 格式，与被引用运行记录三列拼接结果一致。
- 被引用运行与评分必须存在，且属于同一文种、同一模型画像。
- `run_count`、`adoption_rate`、`average_rubric_score` 必须能由引用记录反算得到。
- `adoption_rate` 为 0-1 比例，`average_rubric_score` 为四维 rubric 的 1-5 均分。
- 8.1 只有在 9 个高频文种都有 `pass` 或明确 `fail` 结论、且每个结论都能反查模型运行与人工评分时才能勾选。

## 8.3 机器闸门

`xinchuang-runtime-runs.csv` 一旦填写，必须满足：

- `cpu_arch` 必须为 `loongarch64` 或 `arm64`。
- `runtime_mode` 必须为 `target_host`，表示在目标 CPU / OS 机器上实际运行；交叉编译、Windows / x86_64 / amd64 本机运行、host-only 日志不得记为通过证据。
- `platform_id`、`os_name`、`os_version`、`kernel_version`、`go_version`、`binary_ref`、`platform_fingerprint_ref`、`evidence_ref` 均必填。
- `platform_fingerprint_ref` 必须能指向目标机架构、OS、内核与 Go runtime 指纹；`binary_ref`、`platform_fingerprint_ref`、`evidence_ref`、`notes` 不得使用 `cross-build`、`cross-compile`、`交叉编译`、`local-host`、`dev-machine`、`windows`、`x86_64`、`amd64`、`本机运行` 等仅证明非目标环境的证据。
- 成功运行必须满足 `postgres_connected`、`minio_connected`、`c01_connected`、`c03_connected`、`sse_stream_completed` 均为 `true`，并记录正数 `first_token_ms` 与 `total_generation_ms`。
- 失败运行必须记录 `error_reason`，不能伪装为通过。

`xinchuang-runtime-decisions.csv` 一旦填写，必须满足：

- `run_id` 指向已存在的运行记录。
- `end_to_end_pass=true` 的结论必须引用一条所有依赖均连通且 SSE 完成的运行记录。
- LoongArch64 通过结论必须来自 `cpu_arch=loongarch64` 的真实运行。
- ARM64 + 麒麟通过结论必须来自 `cpu_arch=arm64` 且 `os_name` 包含 `Kylin` 或 `麒麟` 的真实运行。
- 8.3 只有在 LoongArch64 与 ARM64 + 麒麟各有一条 `end_to_end_pass=true` 结论时才能勾选。
