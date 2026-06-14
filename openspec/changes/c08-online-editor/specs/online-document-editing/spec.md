## ADDED Requirements

### Requirement: OnlyOffice 社区版接入与回调鉴权
系统必须（MUST）以 OnlyOffice Document Server 社区版作为公文在线编辑/核稿界面，并经 JWT 回调与 Go 后端集成（口径遵 ADR-0001 D8）。系统必须对编辑器与后端之间的每一次配置下发（document config）与状态回调（callback）签发并校验 JWT；JWT 校验失败的回调请求必须拒绝且不得改动任何文档状态。系统必须仅经 OnlyOffice 公开 API/JS 集成、不得修改 OnlyOffice 源码（保持 AGPL v3 合规边界，遵 D8），且不得隐藏或去除编辑器自带的 OnlyOffice 品牌标识。

#### Scenario: 合法回调被接受并落库
- **WHEN** OnlyOffice 在用户完成编辑后向后端回调接口发送携带有效 JWT 的保存请求（status=2/MustSave）
- **THEN** 后端必须校验 JWT 通过、依回调中的文档 URL 拉取最新 docx 并写入对应成稿版本，且返回 OnlyOffice 约定的成功应答（error=0）

#### Scenario: JWT 缺失或非法的回调被拒绝
- **WHEN** 后端收到一条 JWT 缺失、签名错误或已过期的回调请求
- **THEN** 后端必须拒绝该请求、不得拉取或写入任何文档内容，并记录一条鉴权失败审计

#### Scenario: 不得改 OnlyOffice 源码且保留品牌
- **WHEN** 部署在线编辑组件
- **THEN** 系统必须以原版 OnlyOffice 社区版镜像经 API/JS 集成、不打入对其源码的修改，且编辑器界面必须保留 OnlyOffice 品牌标识

### Requirement: 文档读写经 MinIO 预签名 URL
系统必须（MUST）经 MinIO 预签名 URL 完成 OnlyOffice 对成稿文档的读取与写回，文档原件必须存放于生成稿桶（与范文桶双桶隔离）。预签名 URL 必须设置有限有效期，过期后必须重新签发；后端不得向前端或编辑器下发 MinIO 长期凭据。

#### Scenario: 打开文档下发读取用预签名 URL
- **WHEN** 文秘在写作工作台打开一篇 AI 初稿
- **THEN** 后端必须为该文档签发一个有限有效期的 MinIO 预签名读取 URL 并注入 OnlyOffice 文档配置，使编辑器能加载该 docx

#### Scenario: 保存经预签名 URL 写回生成稿桶
- **WHEN** OnlyOffice 回调触发文档保存
- **THEN** 后端必须将最新 docx 内容写入 MinIO 生成稿桶（而非范文桶），并以新的成稿版本登记其对象引用

#### Scenario: 预签名 URL 过期后重新签发
- **WHEN** 一个已签发的预签名读取 URL 已超过有效期且文秘再次打开同一文档
- **THEN** 后端必须重新签发新的预签名 URL，不得复用已过期 URL，也不得下发 MinIO 长期凭据

### Requirement: docx 保真编辑与实时协同
系统必须（MUST）支持文秘在浏览器内打开 AI 初稿并以 docx 原生保真方式进行编辑与核稿，对照机关口径逐处修订；编辑过程必须支持 OnlyOffice 提供的多人实时协同。本 change 仅做规范正文内容层面的编辑，GB/T 9704 红头版式精排不在范围内（后置）。

#### Scenario: 浏览器内编辑并保真保存
- **WHEN** 文秘在编辑器内修改初稿正文（如调整称谓、主送、正文结构、落款）并触发保存
- **THEN** 系统必须以 docx 保真方式持久化修改结果为新的成稿版本，且重新打开时内容与排版与保存时一致

#### Scenario: 多人协同编辑同一文档
- **WHEN** 两名具备编辑权限的用户同时打开同一篇成稿
- **THEN** 系统经 OnlyOffice 协同会话实时同步双方修改，保存时以协同会话最终 docx 为准写回单一新版本（MVP 不自建 CRDT / 逐字段合并，依赖 OnlyOffice 协同语义）

### Requirement: 权限判定调用 c04 不另立口径
系统对"能否打开/编辑/采纳/导出"某文档的判定必须（MUST）调用 c04 的 RBAC 与密级叠加判定，不得在本 change 另立一套权限或密级口径。本 change 显式依赖并由 c04 权限点登记中心登记的离散操作权限点为：`document.open`（打开文档）、`document.edit`（在线编辑）、`document.export`（导出）、`review.online`（在线核稿）、`adopt.decide`（采纳决策）；各动作入口必须以对应权限点经 c04 统一决策接口校验，不得绕过或在本 change 硬编码权限名。任一判定（RBAC 或密级）未通过时，系统必须拒绝下发文档配置或预签名 URL。

#### Scenario: 无编辑权限被拒绝打开
- **WHEN** 一名不具备该文档编辑权限（`document.open` / `document.edit` 的 RBAC 或密级判定不通过）的用户尝试打开它
- **THEN** 系统必须拒绝下发 OnlyOffice 文档配置与预签名 URL，并提示无权限，而不得返回文档内容

#### Scenario: 叠加判定二者其一不通过即拒绝
- **WHEN** 用户的 RBAC 操作权限通过但密级判定不通过（或反之）
- **THEN** 系统必须按 c04 的叠加判定结果拒绝该操作，不得因单层通过而放行

#### Scenario: 各动作以对应权限点经 c04 校验
- **WHEN** 用户发起打开 / 编辑 / 在线核稿 / 采纳决策 / 导出中的任一动作
- **THEN** 系统必须分别以 `document.open` / `document.edit` / `review.online` / `adopt.decide` / `document.export` 权限点调用 c04 统一决策接口完成校验，不得绕过或自定义权限口径

### Requirement: 公文语义结构化存储与版式解耦
系统必须（MUST）将成稿内容按公文语义结构（如标题、主送、正文结构、落款等要素）结构化存储于 PostgreSQL，并与具体版式表现解耦；成稿原件（docx）存于 MinIO 生成稿桶，二者经成稿版本记录关联。语义结构必须独立于 OnlyOffice 的内部表示，使编辑器后端可替换。

#### Scenario: 保存时同步落语义结构
- **WHEN** 一篇成稿被保存
- **THEN** 系统必须在 PostgreSQL 中登记该版本的公文语义结构（标题/主送/正文结构/落款等要素），并将其与 MinIO 中的 docx 原件经版本记录关联

#### Scenario: 语义结构不绑定具体编辑器
- **WHEN** 查询某篇成稿的语义结构
- **THEN** 返回的结构必须以与版式/编辑器无关的公文要素形式表达，不得依赖 OnlyOffice 私有的内部数据格式

### Requirement: 编辑器可替换适配层
在线编辑接入必须（MUST）做成可替换适配层：前端编辑组件与后端回调/导出接口对业务封装为稳定边界（遵 ADR-0001 §2「编辑器可替换」）。更换底层编辑器实现（如信创终局换 WPS）或接入 OFD 服务时，业务代码（成稿版本、语义结构、采纳决策等）不得改动。

#### Scenario: 业务接口不暴露 OnlyOffice 细节
- **WHEN** 业务侧调用打开/保存/导出等接口
- **THEN** 这些接口契约必须以公文域概念（文档、成稿版本、语义结构）表达，不得在契约层泄露 OnlyOffice 专有的参数或回调格式

#### Scenario: 替换编辑器实现不改业务代码
- **WHEN** 底层编辑器由 OnlyOffice 替换为另一实现（如 WPS）
- **THEN** 仅适配层需重写，成稿版本、语义结构持久化与采纳决策等业务逻辑不得改动
