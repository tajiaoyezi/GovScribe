# ADR-0001：GovScribe（智能公文 Agent）— 技术栈与架构决策

- **状态**：Accepted（2026-06-13）
- **关联**：[PRD](../prds/intelligent-gov-document-agent.prd.md)（需求 + 摘要级技术方向）；本 ADR 承载 PRD 不放的**深层细节、已核验版本/许可、信创 PoC 清单、待确认项**。
- **说明**：以下结论来自三轮带对抗性核验的调研（Go 技术栈 / Milvus / 在线 Office），核验日期 2026-06。**版本号有时效性，落地前以官方 releases / `go list -m -versions` 实时复核。**

---

## 1. 决策清单（Decisions）

### D1 — 应用层 / 后端语言 = Golang
- **决策**：后端用 Go，应用层目标可编译为**单二进制**（利信创/气隙分发）。供应商接入用**窄抽象接口**封装。
- **理由**：脱敏网关/密级路由/流式透传是 Go 主场；单二进制对私有化部署是加分项。
- **边界**：重 ML 算子（embedding/rerank/中文 NER）**不进 Go 进程**，一律服务化外置（见 D6/D7）。Go 框架可选 Eino（字节，pre-1.0 需锁版本）或"官方 SDK + 手写薄编排"（公文 RAG 不复杂，后者更合简单优先原则）。

### D2 — 前端 = React + TypeScript + Ant Design
- **决策**：React + TS + antd，中后台管理 + 写作工作台；初稿**流式输出走 SSE**。

### D3 — 模型接入 = LiteLLM Proxy（MVP）→ Go 直连官方 SDK（信创）
- **决策**：MVP 用 **LiteLLM Proxy** 统一 OpenAI/Anthropic/私有兼容端点（`base_url/api_key/model` 可配可切）；**信创/私有化阶段切换为 Go 直连官方 SDK**，业务代码不动（靠 D1 的窄抽象层）。
- **已核验**：
  - `github.com/openai/openai-go/v3`（**导入路径必须带 `/v3`**，否则拉到过期的 v1.12；Apache-2.0，GA），支持 `option.WithBaseURL`。
  - `github.com/anthropics/anthropic-sdk-go`（MIT，v1.50.x，需 **Go 1.24+**），支持 `WithBaseURL`、流式、ToolRunner。
  - LiteLLM Proxy 是 **Python + Postgres，非单二进制**；运行时热切换模型需开 `store_model_in_db` 并经 `/model/new` API；多 worker 有 ~30s 传播延迟。
  - **Anthropic 经 LiteLLM 的 OpenAI 格式流式**有字段映射损耗与已知丢失 bug（#27946 open）；重度依赖 thinking 时走原生 `/v1/messages` 或直连 anthropic-sdk-go，Go 端用宽松 JSON 接收非标字段。

### D4 — 向量库 = Milvus 2.6.x（原生 BM25 + 稠密混合检索）
- **决策**：选 Milvus，用其**原生混合检索**（稠密向量 + BM25 稀疏），一个引擎覆盖"语义召回 + 精确词召回"。
- **已核验**：
  - Go SDK 用**新客户端** `github.com/milvus-io/milvus/client/v2`（代码 import `.../milvusclient`、`.../entity`、`.../index`）；旧 `milvus-sdk-go` **已废弃**（2025-03）。
  - **SDK 与 server 大版本必须对齐**，锁 **2.6.x**（server 如 2.6.18 / SDK 如 v2.6.5）。**不上 3.0-beta**。需 Go 1.24+。
  - 混合检索：`HybridSearch` + 两个 `NewAnnRequest`（稠密 `entity.FloatVector` + BM25 稀疏 `entity.Text`）+ `NewRRFReranker()` 起步。
  - 中文分词：内置 `chinese` analyzer = jieba + cnalphanumonly（GA）。
  - **jieba 文件式大词典（FileResource）是 Milvus 3.0（beta）才有；2.5/2.6 只能内联进 schema** → 海量单位名录词典走第 2 节的"结构化字段"方案，不靠 jieba 大词典。
- **后果/代价**：Milvus **无法嵌进 Go**（Lite 仅 Python、FLAT-only、不可生产）→ 始终是独立进程 + etcd + MinIO（2.6 用内置 Woodpecker 去掉 Pulsar/Kafka）。**"单二进制"只对 Go 应用层成立。**

### D5 — 关系库 = PostgreSQL；对象存储 = MinIO（双桶）
- **决策**：PostgreSQL 作**权威数据源 + 访问控制判定**（用户、模型配置、审计日志、脱敏字典、文档元数据、采纳稿回流）；MinIO 存范文/生成稿 + Milvus 后端，**业务桶与 Milvus 桶物理隔离**。

### D6 — embedding / rerank = TEI / Xinference 服务化
- **决策**：embedding 与 rerank 模型**服务化**（OpenAI 兼容端点），优先中文 embedding（BGE 系等）；Go 经 HTTP 调用。
- **已核验坑**：`openai-go` 接 embedding 时**显式设 `EncodingFormat="float"`**（openai-go 对 base64 响应反序列化有未修复缺陷 #241）；rerank 走 Cohere/Jina 风格 `/v1/rerank`（OpenAI 规范无 rerank，需自写 ~20 行 client）。信创：昇腾 reranker 不走 vLLM-Ascend，用华为 mis-tei。

### D7 — 中文 NER 脱敏 = Go 网关（正则 + AC 字典）+ 外置 NER 服务
- **决策**：脱敏网关三层在 Go 侧落地——正则 + 客户可维护字典（Aho-Corasick 多模匹配）+ **中文 NER 兜底（外置 Python 微服务，HanLP / PaddleNLP PP-UIE / GLiNER 实测后选）**。
- **铁律**：**映射表生成/回填/diff/审计全部留在 Go 网关内**；外置 NER 服务无状态、只收文本片段、只回实体 span，**绝不接触映射表与原文落盘**。流式回填用尾部缓冲（tail-buffer）处理跨 chunk 截断的占位符。
- **已核验**：HanLP 最新 v2.1.3（非 2024-12）；Aho-Corasick 用 `petar-dambovaliev/aho-corasick`（无 v1 tag，**锁 commit**）或保守 `cloudflare/ahocorasick`，做成可替换接口；纯 Go 分词 `sugarme/tokenizer` 有多字节 offset bug（#82），若用于 NER offset 切割须回归测试或打补丁。
- **失败模式（fail-closed 为底线 + 允许降级，已决策）**：正则 / AC 字典跑在 Go 进程内、**不随 NER 宕机失效**；只有外置 NER 兜底会"挂"，而它恰恰负责抓字典未覆盖的新实体 → **NER 不可用时绝不静默发原文**。判定"不可用"要宽：**超时 / 连接失败 / 5xx / 响应格式异常均算**"脱敏未完成"，不能只判进程死活。处置优先级：① 自动转私有 / 本地模型；② 无私有可用时**降级发送**（仅 regex + AC 字典、跳过 NER），为**按密级可配开关**——**MVP 公网阶段（无私有模型）降级即主回退路径**；③ 否则阻断 + 报错。**涉密恒走私有、永不降级**。用**熔断器（circuit breaker）**避免逐请求干等超时、拖死写作流；fail-open 永远是客户为某密级显式开启的结果，不是默认值；每次降级 / 阻断 / 转私有均入审计（与"脱敏前后 diff + 命中项"同一套）。

### D8 — 在线编辑 = OnlyOffice Document Server 社区版（免费，MVP）
- **决策**：MVP 用 **OnlyOffice 社区版（免费）**做公文在线编辑/核稿界面；docx 原生保真 + 实时协同；与 Go 后端经 **JWT 回调**集成，文档读写走 **MinIO 预签名 URL**。
- **已核验约束**：
  - 社区版 **AGPL v3 + 禁止去品牌**（编辑器带 OnlyOffice 标识）；**只要不改其源码、仅经 API/JS 集成，一般不传染本应用**（商用前法务确认）。
  - **正式政企交付大概率仍需商业授权**（去品牌 / 合规 / SLA / 集群）——成本是延后非消除。
  - 信创认证：**麒麟有 2022 企业版一手认证；统信 / 龙芯待书面确认**（龙芯非 ARM64，官方 arm64 镜像不覆盖）。
  - docx 导出（非编辑场景）若需纯 Go：用 **`mmonterroca/docxgo`（MIT）**；**排除 `unioffice`、`fumiama/go-docx`（AGPL，污染闭源单二进制）**。轻量富文本备选 TipTap（MIT）。

### D9 — OFD = 暂不支持
- **决策**：**当前不支持 OFD**，后续视客户需求再评估。
- **背景**：OFD 是党政电子公文国家标准（**GB/T，推荐性，非法律强制**；强制力来自上级发文/本地系统，各地不一——**勿对客户表述为"全国强制"**）。
- **若未来要做**：OFD 生态是 Java（ofdrw，Apache-2.0，**无 docx→OFD，无 HTML→OFD**，只能 docx→PDF→OFD 且有字体/版式漂移）或商业 SDK（数科/福昕，信创已适配）；**Go 无成熟原生 OFD 库**，届时定位为**可旁挂独立服务**。

### D10 — 身份认证 / 权限 / 租户
- **决策**：MVP **单租户私有部署**（每客户一套，数据按部门 / 用户 / 密级隔离，**不引入多租户复杂度**，与私有化交付方向一致）；权限用 **RBAC 落 Postgres**。
- **角色**：**系统管理员**（配模型接入 / 维护脱敏库 / 看审计 / 管用户）、**文秘（核心）**（起草 / 检索范文 / 在线核稿 / 决定采纳 / 补范文入库）、**业务兼职用户**（文种引导 + 通用兜底起草，权限较窄）、**审计员**（只读审计与脱敏留痕，职责分离，可选）。
- **认证**：MVP **本地账号密码**；认证做成**可插拔适配层**，信创 / 政企终局接客户统一身份认证（CAS / OIDC / LDAP / 国产 IAM），业务代码不动（与 D1 窄抽象同思路）。
- **边界**：**RBAC 与密级是两层正交控制**——RBAC 管"能做什么操作"，密级（§2 密级防越权，Postgres 预过滤为权威）管"能看 / 能外发什么数据"；二者叠加判定，不可相互替代。

---

## 2. 架构方向

- **三库切分**：Postgres（权威源 + ACL）/ Milvus（可重建的派生混合索引，**密级=partition key、文种=标量过滤**）/ MinIO（双桶）。Milvus 是派生索引，必要时可从 Postgres + MinIO 全量重建。
- **跨库一致性**：无跨库事务 → **outbox 异步管线 + 软删 + 定期对账 job**；主键用全局 `chunk_id`。
- **密级防越权**：**Postgres 预过滤为权威 ACL**；Milvus 标量过滤/partition 仅为性能与纵深，过滤条件由后端注入，前端不可传任意表达式。partition key 不可变 → **降密走 delete + reinsert**。删除有可见性延迟 → 查询层强制 `is_deleted==false` 兜底 + 审计双时间戳。
- **文号 / 单位名精确召回**：**入库时由脱敏网关正则 + AC 字典抽取为结构化标量字段做精确匹配，不依赖 jieba/BM25 切分**。注意：`〔2024〕X号` 经 jieba+cnalphanumonly 会被切碎（括号删、数字与"号"分开），**`TEXT_MATCH` 走同一 analyzer 同样切碎** → 必须用标量 VARCHAR 精确等值 / keyword（不切词）字段。与脱敏识别复用同一套词条来源。
- **编辑器可替换**：在线编辑接入做成可替换适配层（前端组件 + 后端回调/导出接口），内容按公文语义结构化存储、与版式解耦，导出抽象 `export(docx)` 预留 `export(ofd)`，便于信创终局换 WPS / 接 OFD 服务时业务不动。
- **部署路径**：MVP 公网快速验证 → 信创/私有化全本地离线（LLM/embedding/rerank/向量库/对象存储/编辑器均私有化，脱敏映射表不出域）。

---

## 3. 版本与许可锚点（落地前实时复核）

| 组件 | 选定 | 许可 | 关键备注 |
|---|---|---|---|
| Go | 1.24+ | — | Milvus 新客户端与 anthropic-sdk-go 均要求 |
| Milvus server | 2.6.x（如 2.6.18） | Apache-2.0 | 不上 3.0-beta |
| Milvus Go 客户端 | `milvus/client/v2`（import `milvusclient`） | Apache-2.0 | 与 server 大版本对齐；旧 `milvus-sdk-go` 废弃 |
| openai-go | v3.x | **Apache-2.0** | **导入路径必须 `/v3`** |
| anthropic-sdk-go | v1.50.x | MIT | `WithBaseURL`、流式、ToolRunner |
| LiteLLM Proxy | MVP 用 | — | Python+Postgres，非单二进制；信创阶段退场 |
| docx 导出（纯 Go） | `mmonterroca/docxgo` | **MIT** | 排除 unioffice / fumiama-go-docx（AGPL） |
| 在线编辑 | OnlyOffice DS 社区版 | **AGPL v3** | MVP 免费、带品牌；正式交付评估商业授权 |
| 中文 NER | HanLP v2.1.x / PP-UIE / GLiNER | 各异 | 外置 Python 服务，实测公文域召回后选 |
| Aho-Corasick | petar-dambovaliev（锁 commit）/ cloudflare | MIT | 做成可替换接口 |

---

## 4. 信创 / 私有化必做 PoC 清单（开工前/交付前）

- [ ] **鲲鹏 ARM64 + 麒麟**：Milvus / OnlyOffice 的 ARM64 镜像在 **64KB 页大小 / jemalloc** 下的可用性（可能需重编镜像）；ARM CI 非一等公民，锁已验证 tag。
- [ ] **龙芯 LoongArch64（高风险，必须 PoC，勿向客户承诺）**：Milvus 无官方镜像（knowhere C++ SIMD 风险）、OnlyOffice 无官方（仅社区 fork 旧版）、LiteLLM/Prisma 无 loong64 引擎。
- [ ] **Milvus BM25 中文**：用真实公文跑 `run_analyzer`，验证文号/单位名分词与召回；确认走结构化字段兜底。
- [ ] **Postgres 国产化**（若客户要求 openGauss / 人大金仓）：与应用的兼容性 PoC（注意 Milvus 自带存储，向量侧不依赖 PG 扩展）。
- [ ] **OnlyOffice**：统信 UOS / 龙芯官方信创认证书面确认；**AGPL 合规法务确认**（仅 API 集成、不改源码）。
- [ ] **气隙**：所有组件离线镜像 + Helm 包预打包；**公文专用字体（仿宋_GB2312、方正小标宋_GBK 等）授权与分发确认**。
- [ ] **embedding/rerank 国产推理**：昇腾走 mis-tei（rerank 不走 vLLM-Ascend）/ 鲲鹏 CPU 走 llama.cpp GGUF 兜底，锁版本并自测吞吐。

---

## 5. 待商务 / 法务 / 客户确认项

- OnlyOffice **Developer/Enterprise 商业授权报价**（正式政企交付：去品牌 + 合规 + SLA + 信创认证书）。
- OnlyOffice 统信 / 龙芯**官方兼容认证证书**。
- **密级策略**（哪些可脱敏后上公网、哪些强制私有化）——客户合规部门政策决定（PRD Open Question）。
- **指标阈值**（采纳率 / 提效 / 速度）——待首版实测后与客户共同定档（PRD）。
- **范文数据盘点**（9 个高频文种每种可提供多少篇优质存量范文、格式、到位时间、清洗/脱敏入库责任方）——开工前确认；RAG 提质的效果与 UAT 可验证性取决于此，库空/过少则退回通用模型"不像公文"（PRD Open Question 及「文种覆盖矩阵」）。

---

## 6. 被核验纠正的认知（避免重蹈）

- `openai-go` 裸导入路径会拉到过期 v1.12，**必须 `/v3`**。
- Milvus `TEXT_MATCH` 与 jieba 同 analyzer，**同样切碎文号** → 精确召回必须靠结构化标量字段，不是 TEXT_MATCH。
- OnlyOffice **麒麟认证是 2022 企业版**（非社区版）；统信/龙芯无一手认证证据。
- HanLP 最新 **v2.1.3**（非 2024-12）。
- ofdrw **只有 OFD→HTML，没有 HTML→OFD**（OFD 已暂不支持，备查）。
- LiteLLM "立即生效"的热切换对**在途流不生效**、多 worker 有传播延迟、Redis 缓存有 TTL 陈旧期。
