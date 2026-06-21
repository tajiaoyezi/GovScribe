# c05 高频文种 7.3 校准证据协议

建立日期：2026-06-20

## 目标

本协议用于把 7.3 的“真实范文与目标模型实测校准”落成可审计证据链。7.3 只有在能给出校准后的 TopK、提示总长上限、契约措辞版本，以及采纳率和速度依据时，才能标记完成。

## 边界

- 原始公文目录 `各类文件/` 只作为本地候选素材来源，不提交 git，不在 c05 运行时直接读取；证据 CSV 不得记录裸目录名 `各类文件`、本地路径或原始文件名。
- few-shot 样例只能来自 c03 `corpus-rag-retrieval` 的脱敏后检索结果；本协议不得绕过 c03 从本地原文目录注入样例。
- 清洗、脱敏和入库归属 c02 / c03 相关流程；c05 只消费 c03 可检索样例与目标模型运行证据，不持有脱敏前原文。
- 文种 / 子类是否属于 c05 深做档仍由 c06 单一权威判定；本协议不写入能力档、不判定标黄、不决定移交 c07。
- 在线核稿与采纳决策权威归 c08；本协议只定义 7.3 校准时需要采集的采纳标签字段。

## 证据链

1. **候选素材登记**：记录各文种本地候选公文包数与可抽取数，不记录标题清单或正文。
2. **脱敏与 c03 入库**：原文经清洗 / 脱敏后入 c03，并产生可追溯的 c03 检索引用。
3. **提示变体设计**：为目标文种设置 TopK、提示总长上限、结构契约版本与措辞版本。
4. **目标模型实测**：用 c06 场景上下文 + c03 检索样例 + c05 编排路径发起模型生成，记录模型、参数、耗时与输出引用。
5. **人工评分与采纳标签**：按 7.1 rubric 四维评分，并记录直接用 / 小改 / 大改 / 弃用。
6. **校准决策**：按文种汇总采纳率与速度数据，形成 TopK / 提示总长 / 契约措辞的定档结论。

## 记录文件

- `docs/other/c05-high-freq-doctype-calibration-candidates.csv`：候选素材与 c03 入库状态；只记录聚合数量、抽象批次、c03 gate 与受控 candidate gate code，不记录自由文本说明。
- `docs/other/c05-high-freq-doctype-corpus-intake-readiness.csv`：原始候选素材到清洗 / 脱敏 / c03 入库的准备清单；只记录抽象批次、聚合数量、缺口、责任状态、下一步 gate 与受控 readiness code，不记录本地路径、原始文件名、正文标题或自由文本说明。
- `docs/other/c05-high-freq-doctype-calibration-variants.csv`：校准候选提示变体矩阵。
- `docs/other/c05-high-freq-doctype-calibration-runs.csv`：目标模型运行记录。
- `docs/other/c05-high-freq-doctype-calibration-reviews.csv`：人工评分与采纳标签。
- `docs/other/c05-high-freq-doctype-calibration-decisions.csv`：每个文种的校准结论。

## 字段约束

### 候选素材

- `doctype` 必须属于 c05 9 个高频文种。
- `raw_package_count` 与 `readable_package_count` 只登记数量，不登记原文标题或正文。
- `readable_package_count` 不得大于 `raw_package_count`；二者为 0 时，`gate_status` 只能为 `pending_corpus` 或 `insufficient`。
- `desensitized_batch_ref` 只记录脱敏后样本批次引用；未完成脱敏前填 `pending`，完成后必须使用 `sanitized-batch:<batch-id>` 格式，不得记录本地路径、原始目录名、文件名或正文标题。
- `c03_query_ref` 只记录 c03 可检索样例的查询 / 批次引用；未完成 c03 入库前填 `pending`，不得填本地原文路径或裸原始目录名。
- `c03_retrievable_count` 只在 c03 入库并可检索后填写；未完成时填 `pending`。
- `gate_status` 取值：`pending_corpus` / `pending_desensitization` / `pending_c03` / `ready_for_model_run` / `insufficient`.
- `pending_c03` 必须已有非 `pending` 的 `desensitized_batch_ref`，且 `c03_query_ref` / `c03_retrievable_count` 仍为 `pending`，表示已脱敏但尚未完成 c03 可检索验证。
- `ready_for_model_run` 必须同时具备非 `pending` 的 `desensitized_batch_ref`、非 `pending` 的 `c03_query_ref` 与正数 `c03_retrievable_count`；只完成本地候选素材盘点或清洗脱敏前，不得把该文种标为可跑目标模型。
- `candidate_gate_code` 只允许 `missing_corpus` / `awaiting_desensitization` / `awaiting_c03_retrieval` / `ready_for_model_run` / `low_sample_count` 五类受控代码，并必须与 `gate_status`、`raw_package_count`、`readable_package_count` 自洽；不得使用自由文本记录原始标题、正文片段或文件名。

### 语料入库准备清单

- `doctype` 必须覆盖 c05 9 个高频文种，且每个文种只记录聚合数量与抽象来源分组。
- `candidate_batch` 与 `source_group_ref` 只能使用抽象批次 / 分组引用，不得记录本地原始目录、原始文件名、正文标题或 Office / PDF 原文扩展名。
- `candidate_batch`、`raw_package_count` 与 `readable_package_count` 必须与同文种的候选素材表记录完全一致，不得在准备清单中单独抬高或改写候选数量。
- `readable_gap_to_100` 必须按 `max(0, 100 - readable_package_count)` 计算，便于实施侧判断距离 `<100` 稀缺线的缺口。
- `intake_stage` 取值：`ready_for_desensitization` / `needs_more_corpus` / `missing_corpus`。
- `next_c03_gate` 取值：`pending_desensitization` / `pending_corpus` / `insufficient`，必须与候选数量和 `intake_stage` 自洽。
- `ready_for_desensitization` 表示已有可抽取候选包，下一步先由清洗 / 脱敏责任方产出脱敏批次，再进入 c03 入库验证；它不代表已经可跑目标模型。
- `needs_more_corpus` 表示有少量候选但不足以支撑稳定校准，仍需补充素材。
- `missing_corpus` 表示当前批次未覆盖该文种，必须先补齐原文或确认已有脱敏入库批次。
- `desensitization_owner` 与 `c03_ingestion_owner` 必须显式登记责任状态；未定责时填“待实施侧确认”，不得留空。
- `readiness_code` 只允许 `has_readable_candidates` / `low_sample_count` / `missing_corpus` 三类受控代码，不得使用自由文本记录原始标题、正文片段或文件名。

### 提示变体矩阵

- `variant_id` 必须唯一；模型运行表中的 `prompt_variant_id` 必须引用这里已登记的变体。
- `doctype` 必须属于 c05 9 个高频文种；`subtype` 必须与后续模型运行记录一致。
- `topk`、`prompt_total_chars`、`prompt_token_estimate` 必须为正整数。
- `contract_version` 与 `wording_version` 必须记录结构契约版本与措辞版本；不得只写“默认值”替代可追溯版本。
- `comparison_group` 用于把同一文种 / 子类下可比较的 TopK、提示总长或契约措辞候选归组。
- `comparison_axis` 取值：`baseline` / `topk` / `prompt_total_chars` / `contract_wording` / `combined`。
- `variant_status` 取值：`planned` / `ready_for_run` / `retired`；标为 `ready_for_run` 前，该文种必须已有 `ready_for_model_run` 的 c03 候选素材记录。

### 模型运行

- `run_id` 必须唯一，并能追溯到模型输出对象或日志。
- `c03_query_id` 必须指向 c03 检索结果，且必须匹配同文种候选素材表中 `gate_status=ready_for_model_run` 的 `c03_query_ref`；不得填本地原文路径。
- `prompt_variant_id` 必须引用 `calibration-variants.csv` 中已登记的变体，且文种、子类、TopK、提示总长、token 估算与契约版本必须一致。
- `model_endpoint_evidence_ref` 必须指向真实目标模型端点、部署清单或网关证明引用；不得为 `fake`、`mock` / `mocked`、`stub`、`dummy`、`httptest`、`localhost`、`127.0.0.1`、`unit-test`、`local-model`、`dev-server`、`test-endpoint` 等本地假服务或单测证据。
- `content_security_level` 必须来自 c06 上下文，取值为 `非密` / `敏感` / `涉密`。
- `first_token_ms` 与 `total_generation_ms` 必须来自真实运行计时；不得用估算值。
- `output_ref` 只记录脱敏后输出引用，不记录完整正文。

### 人工评分

- 四维分数取 1-5：文种规范、结构完整、行文关系、机关口径。
- `adoption_status` 只允许：`直接用` / `小改` / `大改` / `弃用`。
- `counts_as_adopted` 必须遵循 PRD 口径：直接用 / 小改为 `true`，大改 / 弃用为 `false`。

### 校准决策

- `selected_topk` 与 `selected_prompt_total_chars` 必须来自对应模型运行记录，不得只引用默认值。
- `adoption_rate` 必须由人工评分记录计算，不能引用 7.1 种子样稿分数。
- `median_first_token_ms` 与 `p95_total_generation_ms` 必须由目标模型运行记录计算。
- 若某文种候选不足、未完成 c03 入库或无人工采纳标签，`pass_fail` 必须填 `blocked` 或 `insufficient_evidence`。
- `evidence_refs` 对 `pass` / `fail` 决策必须使用分号分隔的 `run:<run_id>;review:<review_record_id>;variant:<variant_id>` 引用格式；其中 `variant:<variant_id>` 保留提示变体表中的完整 `variant_id`（例如 `variant:notice-topk3`），不得剥掉 `variant:` 前缀；被引用的运行记录、人工评分记录与提示变体必须真实存在，且评分记录必须引用同一组运行记录。
- `pass` / `fail` 决策必须至少引用两个同一 `comparison_group` 的 `variant:<variant_id>`，且至少包含一个非 `baseline` 对比轴（`topk` / `prompt_total_chars` / `contract_wording` / `combined`），避免只拿单个默认提示变体伪装成校准。
- 每个被引用的提示变体都必须被 `evidence_refs` 中的已完成模型运行覆盖；被选定的 `selected_topk`、`selected_prompt_total_chars`、`selected_contract_version` 必须匹配其中一个被引用变体。
- `run_count`、`adoption_rate`、`median_first_token_ms`、`p95_total_generation_ms` 的聚合口径只统计匹配 `selected_topk` / `selected_prompt_total_chars` / `selected_contract_version` 的已选变体运行；其它被引用运行仅用于证明对比变体已实测。

## 机器校验闸门

`internal/draft/calibration_artifacts_test.go` 对上述 CSV 证据链设置机器闸门：

- 候选素材表必须覆盖 c05 9 个高频文种，且不得记录本地原文路径、裸 `各类文件` 目录名、文件名或 Office/PDF 原文扩展名；`candidate_gate_code` 必须为受控代码并与 `gate_status` / 数量自洽，不得用自由文本说明替代。
- 语料入库准备清单必须覆盖 c05 9 个高频文种，且不得记录本地原文路径、裸 `各类文件` 目录名、文件名或 Office/PDF 原文扩展名；其 `readable_gap_to_100`、`intake_stage` 与 `next_c03_gate` 必须自洽，避免把原始候选目录误当作已脱敏 / 已入 c03 的模型运行前置证据。
- 候选素材表的 `gate_status` 必须与 `raw_package_count` / `readable_package_count` / `desensitized_batch_ref` / `c03_query_ref` / `c03_retrievable_count` 自洽；未取得脱敏批次、c03 查询引用与正数 c03 可检索样例前不能进入 `ready_for_model_run`。
- 提示变体表一旦填写，必须有唯一 `variant_id`、c05 文种、子类、正数 TopK、提示总长、token 估算、契约版本、措辞版本、对比组、对比轴与变体状态；`ready_for_run` 状态必须已有同文种 c03 可检索候选。
- 模型运行记录一旦填写，必须有唯一 `run_id`、真实日期、c03 检索引用、TopK、提示长度、模型信息、真实目标模型端点 / 部署证据、内容密级、首包耗时、总耗时、输出长度与流完成状态；不能用 `pending` 或 `各类文件/` 作为 c03 证据，不能用本地假服务或单测端点替代目标模型。
- 模型运行记录的 `c03_query_id` 必须能回查到同文种 `ready_for_model_run` 候选行，避免绕过脱敏批次与 c03 可检索数量门禁直接登记目标模型运行；`prompt_variant_id` 必须能回查到同文种 / 子类且参数一致的提示变体，避免运行记录临时写入未经登记的 TopK、提示总长或契约措辞版本。
- 人工评分记录一旦填写，必须引用已存在的 `run_id`，四维评分必须为 1-5，采纳标签与 `counts_as_adopted` 必须符合 PRD 口径（直接用 / 小改计入采纳，大改 / 弃用不计入）。
- 校准决策一旦声明 `pass` 或 `fail`，必须给出 TopK、提示总长、契约版本、运行次数、采纳率、首包中位数、总耗时 P95 与证据引用；这些聚合字段必须由 `evidence_refs` 中匹配已选变体的 `calibration-runs.csv` 与 `calibration-reviews.csv` 记录反算得到，不能只填手工聚合值。
- `evidence_refs` 的每个 `run:<run_id>` 必须指向同文种、已完成且其 `prompt_variant_id` 已列入 `variant:<variant_id>` 引用集合的目标模型运行；每个被引用运行都必须有对应的 `review:<review_record_id>` 人工评分，且评分记录必须指向同一组运行记录。
- `evidence_refs` 的每个 `variant:<variant_id>` 必须指向同文种 / 子类、同一对比组的提示变体；至少两个变体必须被引用运行实际覆盖，并且被选定的 TopK / 提示总长 / 契约版本必须来自其中一个变体。
- 采纳率按引用评分记录中 `counts_as_adopted=true` 的占比计算，可保留两位小数；首包耗时中位数按引用运行排序后的常规中位数取整，P95 总耗时按 nearest-rank 口径计算。
- `tasks.md` 中 7.3 只有在 9 个高频文种都存在 `pass` 校准决策时才能勾选；否则测试会失败。

## 7.3 完成门槛

7.3 完成前必须同时满足：

- 覆盖 c05 9 个高频文种；未覆盖文种必须明确不能完成 7.3，不能以其它文种外推。
- 每个文种至少有 c03 可检索的脱敏样例、目标模型运行记录、人工评分记录和速度记录。
- 至少比较两个 TopK 候选值或给出不比较的实测理由；不能直接把 `DefaultFewShotTopK` 当作校准结论。
- 至少记录一个提示总长候选上限及其真实运行效果；不能只记录提示字符数默认估算。
- 校准结论必须能追溯到 `calibration-runs.csv` 与 `calibration-reviews.csv` 的记录。

当前状态：2026-06-20 本地候选素材只解决了部分文种的候选来源问题，尚未完成脱敏、c03 入库、目标模型实测、人工采纳标签与速度证据，因此 7.3 仍不得勾选完成。
