# c05 高频文种 7.3 校准证据协议

建立日期：2026-06-20

## 目标

本协议用于把 7.3 的“真实范文与目标模型实测校准”落成可审计证据链。7.3 只有在能给出校准后的 TopK、提示总长上限、契约措辞版本，以及采纳率和速度依据时，才能标记完成。

## 边界

- 原始公文目录 `各类文件/` 只作为本地候选素材来源，不提交 git，不在 c05 运行时直接读取。
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

- `docs/other/c05-high-freq-doctype-calibration-candidates.csv`：候选素材与 c03 入库状态。
- `docs/other/c05-high-freq-doctype-calibration-runs.csv`：目标模型运行记录。
- `docs/other/c05-high-freq-doctype-calibration-reviews.csv`：人工评分与采纳标签。
- `docs/other/c05-high-freq-doctype-calibration-decisions.csv`：每个文种的校准结论。

## 字段约束

### 候选素材

- `doctype` 必须属于 c05 9 个高频文种。
- `raw_package_count` 与 `readable_package_count` 只登记数量，不登记原文标题或正文。
- `c03_retrievable_count` 只在 c03 入库并可检索后填写；未完成时填 `pending`。
- `gate_status` 取值：`pending_corpus` / `pending_desensitization` / `pending_c03` / `ready_for_model_run` / `insufficient`.

### 模型运行

- `run_id` 必须唯一，并能追溯到模型输出对象或日志。
- `c03_query_id` 必须指向 c03 检索结果；不得填本地原文路径。
- `prompt_variant_id` 必须能说明 TopK、提示总长上限与契约措辞版本。
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

## 7.3 完成门槛

7.3 完成前必须同时满足：

- 覆盖 c05 9 个高频文种；未覆盖文种必须明确不能完成 7.3，不能以其它文种外推。
- 每个文种至少有 c03 可检索的脱敏样例、目标模型运行记录、人工评分记录和速度记录。
- 至少比较两个 TopK 候选值或给出不比较的实测理由；不能直接把 `DefaultFewShotTopK` 当作校准结论。
- 至少记录一个提示总长候选上限及其真实运行效果；不能只记录提示字符数默认估算。
- 校准结论必须能追溯到 `calibration-runs.csv` 与 `calibration-reviews.csv` 的记录。

当前状态：2026-06-20 本地候选素材只解决了部分文种的候选来源问题，尚未完成脱敏、c03 入库、目标模型实测、人工采纳标签与速度证据，因此 7.3 仍不得勾选完成。
