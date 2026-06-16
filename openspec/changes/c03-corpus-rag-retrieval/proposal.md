## Why

GovScribe 公网线靠"范文知识库 + RAG + 提示工程"提质，不微调（PRD「公网提质路线」、ADR-0001 D4）。要让初稿"像公文、可直接采纳"，写作时必须能从范文库稳定检索到同类优质样例喂给模型；这既要语义召回（找"内容相近"的范文），又要精确召回（按文号、发文单位等关键词命中特定公文），且全程不得越过密级边界把不该看的范文检索出来。

当前项目尚无任何检索能力：没有范文入库管线、没有向量库、没有 embedding/rerank 服务接入。c02 已具备脱敏与词条抽取能力、可对范文脱敏入库，c05 / c07 生成编排侧（c05 必选、c07 可选）已具备生成编排、其生成最终经 c01 窄抽象接口出站；本 change 负责补齐"建库 + 检索"这块，让 RAG 提质路线真正跑通，否则"库空 → 检索不到样例 → 退回通用模型不像公文"，价值假设无从验证（PRD Open Questions）。

## What Changes

- 新增**范文知识库建库管线**：支持客户存量公文**初次批量建库**与日常**补充入口**；范文经 c02 脱敏后入库，**采纳稿回流**由 c08（采纳决策持有方）对计入采纳的成稿产出回流信号、写入 c08 与本 change 约定的同一 outbox / 事件表，本 change 单点消费后自动沉淀为新范文样例（ADR-0001 D5）。
- 引入 **Milvus 2.6.x** 作为向量库，使用其**原生 BM25（稀疏）+ 稠密向量混合检索**：`HybridSearch` 下发两路 `NewAnnRequest`（稠密 `FloatVector` + BM25 `Text`），以 `RRF` 融合排序（ADR-0001 D4）。Go SDK 锁 `milvus/client/v2`，与 server 大版本对齐 **2.6.x**，不上 3.0-beta。
- **文号 / 单位名精确召回走结构化标量字段**：入库时复用 c02 脱敏网关的正则 + AC 字典能力，把文号（如 `〔2024〕X号`）、发文单位名抽取为 Milvus 标量 VARCHAR（不切词）字段做精确等值匹配，**不依赖 jieba / BM25 / `TEXT_MATCH`**（三者同一 analyzer 会把文号切碎，ADR-0001 §2、D4）。
- **embedding / rerank 服务化接入**：经 OpenAI 兼容端点调用外置 embedding（优先中文模型）与 rerank 服务，Go 经 HTTP 调用。embedding 显式设 `EncodingFormat="float"` 规避 openai-go base64 反序列化缺陷；rerank 走 Cohere/Jina 风格 `/v1/rerank`，**自写薄 client**（ADR-0001 D6）。
- **三库切分**：Postgres 为权威源与访问控制判定，Milvus 为**可重建的派生混合索引**（密级 = partition key、文种 = 标量过滤），MinIO 存范文原件（业务桶与 Milvus 后端桶物理隔离）（ADR-0001 §2、D5）。
- **密级防越权（保密红线相关）**：以 **Postgres 预过滤为权威 ACL**，Milvus partition/标量过滤仅作性能与纵深，过滤条件**由后端注入、前端不可传任意表达式**；partition key 不可变，降密走 delete + reinsert；查询层强制 `is_deleted==false` 兜底（ADR-0001 §2）。
- **跨库一致性**：无跨库事务 → 关系库先落地、**outbox 异步管线建索引 + 软删 + 定期对账 job**，主键用全局 `chunk_id`，不追求强一致（ADR-0001 §2）。

## Capabilities

### New Capabilities

- `corpus-ingestion`: 范文知识库的写入侧。覆盖客户存量公文初次批量建库、日常补充入口、采纳稿回流入库（回流由 c08 产信号并经约定 outbox / 事件表交付，本 change 单点消费入库 + 对账）；范文经 c02 脱敏后落地，并由脱敏网关的正则 + AC 字典抽取文号/单位名等结构化标量字段；正文敏感实体脱敏入库，文号/单位名等标识字段默认明文做精确等值召回；三库分工写入（Postgres 权威源、MinIO 原件、Milvus 派生索引），以 outbox + 软删 + 定期对账保障跨库最终一致与可全量重建。
- `hybrid-retrieval`: 范文知识库的读取侧。基于 Milvus 原生 BM25 + 稠密向量混合检索（HybridSearch + RRF）对外提供"给定写作意图召回同类范文"的检索能力；语义召回与文号/单位名精确召回（标量字段等值）并行，结果可经 rerank 精排；以 Postgres 预过滤为权威 ACL、Milvus 密级 partition 与文种标量过滤为纵深，杜绝跨密级越权召回。
- `vector-embedding-service`: embedding / rerank 模型的服务化接入抽象。以 OpenAI 兼容端点统一对接外置中文 embedding 与 rerank 服务，封装窄抽象（embedding 锁 `EncodingFormat="float"`、rerank 自写 `/v1/rerank` client），供 `corpus-ingestion` 建索引与 `hybrid-retrieval` 查询/精排复用，并为信创阶段切换国产推理（如昇腾 mis-tei）预留替换点。

### Modified Capabilities

无（本 change 引入的均为新 capability；尚无 `openspec/specs/` 既有规格被修改）。

## Impact

- **新增依赖 / 系统**：Milvus 2.6.x（独立进程，外挂 etcd + MinIO 后端，无法嵌进 Go 单二进制——单二进制只对 Go 应用层成立，ADR-0001 D4）；外置 embedding 服务与 rerank 服务（OpenAI 兼容端点）；MinIO 范文桶。
- **新增 Go 依赖**：`github.com/milvus-io/milvus/client/v2@v2.6.5`（import `milvusclient`/`entity`/`index`，旧 `milvus-sdk-go` 已废弃，module `GoVersion` 为 1.25.8）；`github.com/openai/openai-go/v3`（embedding 调用，导入路径必须带 `/v3`）；自写 rerank client（约 20 行）。要求 Go 1.25.8。
- **新增后端模块**：范文建库/补充/回流的入库管线、outbox + 对账 job、混合检索查询服务、embedding/rerank 接入层。
- **新增 / 扩展接口**：范文批量导入与补充入库 API、范文混合检索 API（供 c05 / c07 生成编排侧取样例，c05 必选、c07 可选；其生成最终经 c01 窄抽象接口出站）；采纳稿回流不走入库 API，而由本 change 单点消费 c08 与本 change 约定的同一 outbox / 事件表入库；内部 embedding/rerank 服务调用契约。
- **Postgres schema 扩展**：范文文档元数据、`chunk_id` 主键、密级/文种/文号/单位名字段、采纳稿回流记录、outbox 表、软删与审计双时间戳（与 c04 权威 ACL 表对接，不在本 change 重定义 ACL 判定）。
- **复用 c02**：文号/单位名结构化抽取与范文脱敏入库复用 c02 的脱敏网关正则 + AC 字典词条来源，本 change 不实现脱敏识别算法本身。
- **依赖 c04（消费，不改其契约）**：范文检索入口消费 c04 `rbac-authorization`（`template.search` 权限点）、范文入库入口消费 c04 `rbac-authorization`（`template.ingest` 权限点）做操作层 RBAC 校验，未授予即拒绝（fail-closed），本 change 不自建 RBAC 判定；该操作层 RBAC 与既有密级数据 ACL 预过滤（Postgres 预过滤为权威 ACL）两层正交叠加、不可相互替代。
- **被 c05 / c07 生成编排侧依赖**：c05 / c07 生成编排侧（c05 必选、c07 可选）从本 change 的检索能力取范文样例做 RAG 提示拼装，其生成最终经 c01 窄抽象接口出站；本 change 不涉及模型生成正文（c05）。
- **保密红线 / 密级路由 / 脱敏**：触及——密级防越权（Postgres 预过滤权威 ACL + Milvus partition 纵深）、范文脱敏入库（涉密恒走私有、永不降级，复用 c02）。映射表不出域口径由 c02 保证，本 change 仅消费其抽取产物。
- **MVP 范围**：在 MVP 内。向量库 MVP 可临时用境内数据驻留的托管服务，信创/私有化阶段全本地离线（Milvus/embedding/rerank/MinIO 均私有化）（ADR-0001 §2 部署路径、D4 PoC 清单）。
