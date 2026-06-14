## ADDED Requirements

### Requirement: OpenAI 兼容 embedding 服务接入

系统必须（MUST）通过 OpenAI 兼容端点接入外置 embedding 服务，优先使用中文 embedding 模型（如 BGE 系），供 corpus-ingestion 建索引与 hybrid-retrieval 查询复用。Go 侧调用必须使用 `github.com/openai/openai-go/v3`（导入路径带 `/v3`），且调用 embedding 时必须显式设置 `EncodingFormat="float"`，以规避 openai-go 对 base64 响应反序列化的未修复缺陷。建库侧与查询侧必须使用同一 embedding 模型 / 维度，保证向量空间一致。

#### Scenario: 文本生成 float 向量
- **WHEN** 调用方传入一段文本请求 embedding
- **THEN** 系统经 OpenAI 兼容端点请求并显式设 `EncodingFormat="float"`，返回与库内向量同维度的 float 稠密向量

#### Scenario: 建库与查询同模型同维度
- **WHEN** corpus-ingestion 建索引与 hybrid-retrieval 查询分别请求 embedding
- **THEN** 两侧必须使用同一 embedding 模型与向量维度，使查询向量可在同一向量空间与库内向量比对

### Requirement: rerank 服务接入（自写薄 client）

系统必须（MUST）以自写薄 client 接入 rerank 服务，走 Cohere / Jina 风格 `/v1/rerank` 接口（OpenAI 规范无 rerank）。该 client 必须接收查询文本与候选文档列表、返回带相关性得分的重排序结果，供 hybrid-retrieval 精排复用。rerank 调用必须有超时控制，超时 / 连接失败 / 5xx / 响应格式异常均视为不可用，由调用方按既定策略降级。

#### Scenario: 候选文档按相关性重排
- **WHEN** 调用方传入查询文本与候选范文片段列表
- **THEN** 系统经 `/v1/rerank` 返回各候选的相关性得分与重排序后的顺序

#### Scenario: rerank 不可用判定
- **WHEN** rerank 请求超时、连接失败、返回 5xx 或响应格式异常
- **THEN** 系统将本次调用判为 rerank 不可用并向调用方返回明确的失败信号，由调用方决定降级

### Requirement: embedding / rerank 窄抽象与替换点

系统必须（MUST）将 embedding 与 rerank 封装为窄抽象接口，使上层（corpus-ingestion / hybrid-retrieval）不直接耦合具体供应商端点。该抽象必须为信创阶段切换国产推理（如昇腾 mis-tei）预留替换点：更换底层推理服务时，上层业务代码不需改动。具体端点地址 / 模型名 / 鉴权配置必须可经配置注入，不得硬编码。

#### Scenario: 切换推理服务不改业务代码
- **WHEN** 运维将底层 embedding / rerank 端点从公网托管服务切换为信创私有化推理（如 mis-tei）
- **THEN** 仅经配置更新端点 / 模型名，corpus-ingestion 与 hybrid-retrieval 的调用代码无需改动即可继续工作

#### Scenario: 端点配置经注入而非硬编码
- **WHEN** 系统初始化 embedding / rerank client
- **THEN** 端点地址、模型名与鉴权信息必须来自配置注入，代码中不得硬编码具体供应商地址
