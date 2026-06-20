# c05 高频文种边界确认（8.4）

## 结论

c05 仅交付高频文种初稿生成接口、结构契约、few-shot 提示编排与 SSE 流式契约。在线编辑 / 核稿、采纳决策、采纳稿回流信号产出、OnlyOffice 集成与 OnlyOffice 信创认证均不由 c05 承担。

本登记仅确认边界，不替代 7.3 的真实模型校准，不替代 8.1-8.3 的国产模型 / c01 后端切换 / 国产 CPU 与操作系统 PoC。

## 权威归属

| 事项 | 权威归属 | c05 结论 | 证据 |
| --- | --- | --- | --- |
| 高频文种初稿生成与 SSE 流式契约 | c05 | c05 负责消费 c06 上下文、检索 c03 样例、拼装提示并经 c01 生成初稿。 | `openspec/changes/c05-high-freq-doctypes/specs/draft-generation-streaming/spec.md`、`internal/draft/` |
| 在线编辑 / 核稿 / 导出 | c08 | c05 不集成 OnlyOffice，不实现打开、编辑、核稿、导出动作。 | `openspec/changes/c08-online-editor/design.md` D-1/D-2/D-5、`openspec/changes/c08-online-editor/tasks.md` |
| 采纳决策 | c08 | c05 不登记采纳决策，不统计真实采纳率；c05 的 7.1/7.2 仅保留评测与灰度打标口径。 | `openspec/changes/c08-online-editor/design.md` D-6、`openspec/changes/c08-online-editor/specs/review-adoption-decision/spec.md` |
| 采纳稿回流信号产出 | c08 | c08 对计入采纳的成稿产出回流信号，字段为成稿引用、采纳结论、密级、文种。c05 不产出该信号。 | `openspec/changes/c08-online-editor/tasks.md` 7.3 |
| 采纳稿回流入库与对账 | c03 | c03 单点消费 c08 回流信号并经脱敏、抽取、PostgreSQL / MinIO / Milvus outbox 管线入库。c05 不直调 c03 入库 API，不写 Milvus，不写 `corpus_outbox_events`。 | `openspec/specs/corpus-ingestion/spec.md`、`backend/migrations/000003_corpus_rag_retrieval.sql` |
| OnlyOffice 商业授权与信创认证 | c08 / 商务 / 法务 / 信创交付阶段 | c05 不解决 OnlyOffice 授权、去品牌、SLA、统信 / 龙芯认证。当前权威口径为麒麟有企业版认证，统信 / 龙芯待书面确认；正式政企交付前由商务 / 法务 / 信创阶段承接。 | `docs/adr/0001-tech-stack-and-architecture.md` D8、§4-§6；`docs/prds/intelligent-gov-document-agent.prd.md` Risks |

## c05 红线

- 不新增在线编辑器运行时代码、OnlyOffice config、OnlyOffice callback 或 `DocsAPI.DocEditor` 集成。
- 不新增 `document.open` / `document.edit` / `document.export` / `review.online` / `adopt.decide` 入口。
- 不新增采纳决策表、采纳决策登记接口或真实采纳率统计入口。
- 不新增回流入库 API、`corpus_outbox_events` 写入、`corpus_adoption_feedback` 写入、`adoption_ingest` 事件生产或 Milvus 写入路径。
- 不把 OnlyOffice 统信 / 龙芯认证缺口改写为 c05 交付项。

## 验证

- `internal/draft/boundary_registry_test.go` 扫描 c05 生产代码，防止引入在线编辑、采纳决策、回流入库或 Milvus 写入路径。
- 同一测试校验本登记明确写出 c08 / c03 / 商务归属，并引用 c08 D-6、c03 corpus-ingestion、ADR-0001 D8 的权威口径。
