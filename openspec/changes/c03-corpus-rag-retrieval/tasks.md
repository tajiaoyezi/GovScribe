## 1. 基础设施就位（Milvus / MinIO / embedding / rerank）

- [ ] 1.1 部署 Milvus 2.6.x（server 锁 2.6.x，如 2.6.18；独立进程 + etcd + 内置 MinIO 后端，内置 Woodpecker 去 Pulsar/Kafka），确认无法嵌进 Go 单二进制为预期形态（ADR-0001 D4）
- [ ] 1.2 创建 MinIO 业务范文桶，并校验其与 Milvus 自身后端桶为两个物理隔离的桶（不得共用同一桶）
- [ ] 1.3 接入外置 embedding 服务（OpenAI 兼容端点，优先中文模型 BGE 系），冒烟验证可返回 float 稠密向量
- [ ] 1.4 接入外置 rerank 服务（Cohere/Jina 风格 `/v1/rerank` 端点），冒烟验证可返回候选相关性得分
- [ ] 1.5 锁定依赖版本并落 `go.mod`：`github.com/milvus-io/milvus/client/v2`（import `milvusclient`/`entity`/`index`，与 server 大版本对齐 2.6.x）、`github.com/openai/openai-go/v3`（导入路径必须带 `/v3`）、Go 1.24+；上线前以 `go list -m -versions` 复核实际可用版本（ADR-0001 §3）

## 2. vector-embedding-service 窄抽象

- [ ] 2.1 定义 embedding/rerank 的 Go 窄抽象接口（端点地址 / 模型名 / 维度 / 鉴权全部经配置注入，代码内不硬编码供应商地址），供 corpus-ingestion 与 hybrid-retrieval 共用
- [ ] 2.2 实现 embedding client：经 OpenAI 兼容端点请求，调用时显式设 `EncodingFormat="float"` 规避 openai-go base64 反序列化缺陷（#241）；单元测试断言请求体含 float 编码、返回向量维度与配置一致（ADR-0001 D6）
- [ ] 2.3 实现 rerank 自写薄 client：接收查询文本 + 候选片段列表，调用 `/v1/rerank` 返回带相关性得分的重排序结果；统一超时控制，超时 / 连接失败 / 5xx / 响应格式异常一律判为"不可用"并向调用方返回明确失败信号（ADR-0001 D6）
- [ ] 2.4 强制"建库侧与查询侧同 embedding 模型 / 同维度"约束：模型名与维度由统一配置注入，提供启动期一致性校验，维度不匹配时拒绝启动
- [ ] 2.5 为信创阶段切换国产推理预留替换点（昇腾 mis-tei / 鲲鹏 CPU GGUF），验证仅改配置即可切换端点 / 模型名而上层调用代码不动（替换点存在性测试，不在本 change 落地国产推理本体）

## 3. Postgres 权威源 schema

- [ ] 3.1 设计范文文档元数据表与 chunk 表，`chunk_id` 为全局主键；含密级、文种、文号、单位名列，软删 `is_deleted` 与审计双时间戳；密级 / 文种 / 文号 / 单位名列预留与 c04 权威 ACL 表对接的字段，不在本 change 重定义 ACL 判定
- [ ] 3.2 设计采纳稿回流记录表，记录消费自 c08 outbox / 事件表的回流信号来源（成稿引用 / 采纳结论 / 密级 / 文种），关联原生成任务 / 采纳动作以便审计对账
- [ ] 3.3 设计 outbox 事件表（承载建索引 / 软删同步事件），支持失败重试且事件不丢失
- [ ] 3.4 编写迁移脚本并验证表结构与索引（密级 / 文种 / 文号 / 单位名 / `is_deleted` 上的查询路径）

## 4. Milvus 派生索引 schema 与混合检索原语

- [ ] 4.1 设计 Milvus collection schema：稠密 `FloatVector` 字段（维度对齐 embedding 模型）、BM25 稀疏字段（`entity.Text`，内置 `chinese` analyzer 分词）、密级 = partition key、文种 = 标量过滤字段、文号 / 单位名 = 不切词标量 VARCHAR（keyword）字段
- [ ] 4.2 用真实公文跑 Milvus `run_analyzer` 验证 BM25 中文分词，确认 `〔2024〕X号` / 单位名在 analyzer 下会被切碎，从而确立文号 / 单位名必须走结构化标量字段兜底（ADR-0001 §4 PoC，开工前必做）
- [ ] 4.3 实现 `HybridSearch` 双路下发：稠密路 `NewAnnRequest`（`FloatVector`）+ BM25 稀疏路 `NewAnnRequest`（`Text`），以 `NewRRFReranker()` 融合排序；验证单路命中为空时仍以 RRF 返回另一路结果，不整体返回空
- [ ] 4.4 实现文号 / 单位名标量字段等值精确召回，确认不经 jieba / BM25 / `TEXT_MATCH` 切分

## 5. corpus-ingestion 写入侧

- [ ] 5.1 实现统一入库管线骨架：逐篇经 c02 脱敏网关脱敏（fail-closed，脱敏未完成则该篇不写任何库并记入失败清单）→ 复用 c02 正则 + AC 字典抽取文号 / 单位名结构化字段 → 三库写入（Postgres 先落地）
- [ ] 5.2 实现初次批量建库入口：按文种成批导入，支持断点续传、幂等重试、单篇失败不阻断整批，产出逐篇结果清单（成功 / 脱敏失败 / 入库失败及原因）
- [ ] 5.3 实现日常补充入库入口（单篇 / 小批量），复用与初次建库相同的脱敏 / 抽取 / 三库写入路径
- [ ] 5.4 实现采纳稿回流单点消费：消费 c08 与本 change 约定的同一 outbox / 事件表中的回流信号（{ 成稿引用、采纳结论、密级、文种 }，仅"直接用 / 小改"计入采纳），经同一脱敏 + 抽取 + 三库管线沉淀为新范文 + 对账，在 Postgres 回流记录关联原生成任务 / 采纳动作；不提供供 c05 / c07 生成编排侧直调的回流入库 API
- [ ] 5.5 实现缺密级强制拒绝：建库 / 补充 / 回流任一入口缺密级标签的范文一律拒绝入库并标注原因，不得按非密默认放行（回流密级由 c08 信号提供，信号缺密级时阻断）
- [ ] 5.6 实现文号 / 单位名抽取写入与脱敏边界：正文人名 / 证件号 / 金额等敏感实体经 c02 脱敏占位后入库；文号 / 单位名等标识字段默认明文（不切词）写入 Milvus 标量 VARCHAR 字段与 Postgres 对应列做精确等值召回；客户密级策略判定标识字段亦敏感时降级为明文仅落 Postgres、Milvus 标量存确定性哈希 / 占位、精确召回先 Postgres 命中再回 `chunk_id`；本 change 仅消费 c02 抽取产物，不实现脱敏识别算法本身
- [ ] 5.7 在范文入库（初次批量建库 / 日常补充）人工入口接入操作层 RBAC：发起入库前消费 c04 `rbac-authorization` 的授权决策（`template.ingest` 权限点），未授予即拒绝（fail-closed），本 change 不自建 RBAC 判定，且与既有密级数据 ACL 预过滤两层正交叠加、不可相互替代（操作层 RBAC ⊥ 数据层密级 ACL）；采纳稿回流走 c08 约定 outbox / 事件表单点交付、不经此人工入口 → 验证：未授予 `template.ingest` 的角色经入库入口被拒、不进入脱敏 / 抽取 / 三库写入管线；已授予角色仍受缺密级拒绝约束

## 6. 跨库最终一致性（outbox + 软删 + 对账）

- [ ] 6.1 实现 outbox 异步消费管线：消费 Postgres 落地后写入的 outbox 事件，驱动 Milvus 建索引；建索引失败事件可重试且不丢失，最终 `chunk_id` 在 Milvus 可见
- [ ] 6.2 实现软删同步：删除范文走 Postgres `is_deleted=true` 并经 outbox 同步 Milvus，不做物理直删以免索引漂移
- [ ] 6.3 实现定期对账 job：比对 Postgres 与 Milvus 的 `chunk_id` 集合（含密级 / 文种 / 软删状态），检出缺失 / 多余 / 不一致项，重新触发建索引或告警
- [ ] 6.4 实现 Milvus 派生索引全量重建：以 Postgres 元数据 + MinIO 原件为权威源重生成全部 chunk 向量 + 标量字段写回 Milvus，验证重建结果与权威源在密级 partition / 文种 / 文号 / 单位名上一致

## 7. hybrid-retrieval 读取侧

- [ ] 7.1 实现"给定写作意图召回同类范文"检索：意图文本经 vector-embedding-service 生成稠密向量，`HybridSearch` 双路 + RRF 返回 TopK 范文及元数据，供 c05 / c07 生成编排侧（c05 必选、c07 可选；其生成最终经 c01 窄抽象接口出站）取样例
- [ ] 7.2 实现文号 / 单位名精确召回查询：对标量 VARCHAR 字段等值匹配，可与语义召回并行并合并结果
- [ ] 7.3 实现 rerank 精排（可选环节）：候选集经 vector-embedding-service rerank 重排序；rerank 不可用（超时 / 5xx / 格式异常）时降级返回未精排的 RRF 融合结果、记录降级，不向调用方返回检索失败
- [ ] 7.4 实现文种过滤召回：经 Milvus 文种标量过滤限定召回范围；目标文种范文低于可用阈值（稀缺线）时返回现有命中并向消费方（c05 / c07）标识"样例不足"，是否走 c07 通用兜底以 c06 路由判定为单一权威，本 change 不做文种降级决策（阈值取值见 PoC / 待确认项）
- [ ] 7.5 在范文检索入口接入操作层 RBAC：发起范文检索前消费 c04 `rbac-authorization` 的授权决策（`template.search` 权限点），未授予即拒绝（fail-closed），本 change 不自建 RBAC 判定，且与既有密级数据 ACL 预过滤两层正交叠加、不可相互替代（操作层 RBAC ⊥ 数据层密级 ACL） → 验证：未授予 `template.search` 的角色经检索入口被拒、不进入混合检索流程；已授予角色的召回结果仍经密级 ACL 预过滤剔除越级范文

## 8. 密级防越权召回（保密红线相关）

- [ ] 8.1 实现 Postgres 预过滤为唯一权威 ACL：检索结果以 c04 在 Postgres 的密级 / 权限判定预过滤，确保不返回当前用户不可见的范文，即便 Milvus partition 过滤被绕过也不返回越级范文
- [ ] 8.2 实现后端注入过滤条件：密级 / 文种过滤一律由后端按用户密级 / 权限注入；请求中携带的前端自定义过滤表达式一律忽略
- [ ] 8.3 实现查询层 `is_deleted==false` 强制兜底：覆盖软删同步可见性延迟，已软删范文不出现在检索结果
- [ ] 8.4 实现降密路径：partition key 不可变，降密走 delete + reinsert 而非就地改密级
- [ ] 8.5 越权拦截测试：构造跨密级候选、绕过 Milvus partition、前端注入表达式、软删未同步四类场景，验证均不返回越级 / 应隐藏范文

## 9. 对接 c05 / c07 生成编排侧

- [ ] 9.1 暴露范文混合检索 API 供 c05 / c07 生成编排侧（c05 必选、c07 可选；其生成最终经 c01 窄抽象接口出站）取范文样例做 RAG 提示拼装：本 change 只交付样例 + 元数据，不做提示模板与正文生成
- [ ] 9.2 联通"样例不足"标识与消费方处置：向消费方（c05 / c07）返回"样例不足"标识；是否走 c07 通用兜底以 c06 路由判定为单一权威，样例不足时仍带结构契约生成并标记由 c05 处置，c03 不做文种降级决策

## 10. 待 PoC（信创 / 私有化高风险，开工前 / 交付前，勿向客户承诺）

- [ ] 10.1 龙芯 LoongArch64（高风险，必须 PoC）：Milvus 无官方镜像（knowhere C++ SIMD 风险）、依赖链 loong64 缺失，验证向量库与 BM25 中文分词在龙芯上的可用性（ADR-0001 §4，勿向客户承诺）
- [ ] 10.2 鲲鹏 ARM64 + 麒麟：Milvus 的 ARM64 镜像在 64KB 页大小 / jemalloc 下的可用性（可能需重编镜像），锁已验证 tag（ADR-0001 §4）
- [ ] 10.3 国产 embedding / rerank 推理效果与吞吐摸底：昇腾走 mis-tei（rerank 不走 vLLM-Ascend）/ 鲲鹏 CPU 走 llama.cpp GGUF 兜底，锁版本并自测吞吐与召回质量（ADR-0001 §4、D6）
- [ ] 10.4 中文 embedding 模型选型与公文 chunk 切分粒度（整篇 / 段落 / 滑窗）PoC：评测召回质量与向量维度，确定建库 / 查询统一模型与维度（ADR-0001 §4）
- [ ] 10.5 Postgres 国产化（若客户要求 openGauss / 人大金仓）与应用兼容性 PoC（向量侧不依赖 PG 扩展，注意权威源 + ACL 表的兼容）（ADR-0001 §4）
