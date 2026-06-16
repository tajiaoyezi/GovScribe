# hybrid-retrieval

## Purpose

定义范文知识库读取侧能力，包括 Milvus BM25 + 稠密向量混合检索、文号与单位名精确召回、rerank 精排、密级防越权、文种过滤和检索操作层 RBAC。

## Requirements

### Requirement: 范文混合检索（BM25 + 稠密向量 RRF 融合）

系统必须（MUST）基于 Milvus 2.6.x 原生混合检索对外提供"给定写作意图召回同类范文"的能力。检索必须以 `HybridSearch` 下发两路 `NewAnnRequest`——稠密向量路（`entity.FloatVector`，召回内容相近的范文）与 BM25 稀疏路（`entity.Text`，召回关键词相关的范文）——并以 `NewRRFReranker()`（RRF）融合排序。检索查询文本必须经 vector-embedding-service 生成稠密向量；BM25 稀疏路由 Milvus 内置 `chinese` analyzer 承担。该能力供 c05 / c07 生成编排侧（c05 必选、c07 可选；其生成最终经 c01 窄抽象接口出站）取范文样例做 RAG 提示拼装。

#### Scenario: 写作意图召回同类范文

- **WHEN** c05 / c07 生成编排侧以"为某文种起草"的写作意图发起范文检索
- **THEN** 系统将意图文本经 embedding 生成稠密向量，`HybridSearch` 下发稠密 + BM25 两路 `NewAnnRequest`，经 RRF 融合后返回 TopK 同类范文及其元数据

#### Scenario: 两路任一为空仍返回融合结果

- **WHEN** 稠密路与 BM25 路其中一路命中为空、另一路有命中
- **THEN** 系统仍以 RRF 融合返回另一路的命中结果，不因单路为空而整体返回空

### Requirement: 文号与单位名精确召回

系统必须（MUST）支持按文号、发文单位名对范文做精确召回。精确召回必须命中入库时抽取的标量 VARCHAR（不切词）字段，使用等值匹配，且不得经 jieba / BM25 / `TEXT_MATCH` 切分（`〔2024〕X号` 经同一 analyzer 会被切碎导致漏召）。精确召回可与语义召回并行执行，结果可与混合检索结果合并。

#### Scenario: 按文号精确命中

- **WHEN** 用户以文号 `〔2024〕12号` 检索范文
- **THEN** 系统对 Milvus 标量 VARCHAR 字段做等值匹配，精确返回该文号对应的范文，不经分词召回近似项

#### Scenario: 按单位名精确命中

- **WHEN** 用户以发文单位名"XX市人民政府办公厅"检索范文
- **THEN** 系统对不切词标量字段做等值匹配返回该单位的范文，而非按 BM25 分词的模糊相关结果

### Requirement: 结果 rerank 精排

系统应当（SHALL）支持对混合检索的候选结果调用 vector-embedding-service 的 rerank 能力做精排，以查询文本与候选范文片段的相关性重排序后再返回。rerank 必须为可选环节：当 rerank 服务不可用时，系统必须降级返回未精排的 RRF 融合结果而非整体失败。

#### Scenario: rerank 提升相关性排序

- **WHEN** 混合检索返回候选集且 rerank 服务可用
- **THEN** 系统以查询文本与各候选片段调用 `/v1/rerank` 重排序，按相关性得分降序返回精排结果

#### Scenario: rerank 不可用时降级

- **WHEN** rerank 服务超时或不可用
- **THEN** 系统降级返回未经 rerank 的 RRF 融合结果，并记录降级，不向调用方返回检索失败

### Requirement: 密级防越权召回（Postgres 预过滤为权威 ACL）

检索必须（MUST）以 Postgres 预过滤为权威 ACL，确保不返回当前用户密级 / 权限不可见的范文；Milvus 密级 partition 与文种标量过滤仅作性能与纵深防护，不得作为唯一的访问控制依据。所有密级 / 文种过滤条件必须由后端注入，前端不可传递任意过滤表达式。查询层必须强制 `is_deleted==false` 兜底过滤已软删的范文。

#### Scenario: 越级范文被权威 ACL 拦截

- **WHEN** 某用户发起检索且候选集中含其密级不可见的范文
- **THEN** 系统以 Postgres 预过滤为权威 ACL 将不可见范文剔除，最终结果不含越级范文，即便 Milvus partition 过滤被绕过也不返回

#### Scenario: 前端不可注入过滤表达式

- **WHEN** 检索请求中携带前端自定义的过滤表达式
- **THEN** 系统必须忽略该表达式，仅采用后端按用户密级 / 权限注入的过滤条件执行检索

#### Scenario: 软删范文不被召回

- **WHEN** 一篇范文已在 Postgres 软删（`is_deleted=true`）但 Milvus 删除尚未生效（可见性延迟）
- **THEN** 查询层凭 `is_deleted==false` 兜底过滤，该范文不出现在检索结果中

### Requirement: 文种过滤召回

系统必须（MUST）支持按文种限定检索范围，使 c05 / c07 生成编排侧（c05 必选、c07 可选；其生成最终经 c01 窄抽象接口出站）仅取目标文种（对齐 PRD「文种覆盖矩阵」5 档能力阶梯口径）的范文样例。文种过滤通过 Milvus 标量字段过滤实现，并受 Postgres 权威 ACL 约束。当目标文种范文不足（参照稀缺线）时，系统应当返回可得结果并向消费方（c05 / c07）标识样例不足；是否走 c07 通用兜底以 c06 路由判定为单一权威，本 change 不做文种降级决策。

#### Scenario: 按目标文种召回样例

- **WHEN** c05 / c07 生成编排侧为"通知"文种请求范文样例
- **THEN** 系统以文种标量过滤限定召回范围，仅返回"通知"文种的范文

#### Scenario: 目标文种样例不足

- **WHEN** 目标文种在库内范文数量低于可用阈值
- **THEN** 系统返回现有命中并向消费方（c05 / c07）标识"样例不足"，是否走 c07 通用兜底以 c06 路由判定为单一权威，本 change 不做文种降级决策

### Requirement: 范文检索操作层 RBAC 鉴权

发起范文检索前系统必须（MUST）消费 c04 `rbac-authorization` 的授权决策（`template.search` 权限点），未授予即拒绝（fail-closed），本 change 不自建 RBAC 判定。该操作层 RBAC 与既有密级数据 ACL 预过滤（Postgres 预过滤为权威 ACL）两层正交叠加、不可相互替代（操作层 RBAC ⊥ 数据层密级 ACL）：RBAC 判定"能否发起检索"，密级 ACL 预过滤判定"能召回哪些范文"，任一层拒绝即整体拒绝。

#### Scenario: 未授权角色不可发起范文检索

- **WHEN** 某角色未被授予 `template.search` 权限点而发起范文检索
- **THEN** 系统经 c04 `rbac-authorization` 判定拒绝该检索请求（fail-closed），不进入混合检索流程，本 change 不自建 RBAC 判定

#### Scenario: 已授权检索仍受密级 ACL 预过滤约束

- **WHEN** 某角色已被授予 `template.search` 权限点并发起范文检索
- **THEN** 系统放行检索操作，但召回结果仍经密级数据 ACL 预过滤（Postgres 预过滤为权威 ACL）剔除越级范文，两层正交叠加、不可相互替代
