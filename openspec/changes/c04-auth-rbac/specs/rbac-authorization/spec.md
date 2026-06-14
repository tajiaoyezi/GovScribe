## ADDED Requirements

### Requirement: 角色定义与 Postgres 权威落地
系统应当（SHALL）固化四类角色并以 PostgreSQL 为唯一权威源存储角色、操作权限点及角色—权限映射，运行期的授权判定必须（MUST）以 Postgres 中的映射为准（口径见 ADR-0001 D10）。四类角色及其操作授权边界必须为：
- **系统管理员**：配置模型接入、维护脱敏库、查看审计、管理用户 / 角色；
- **文秘（核心）**：起草、检索范文、在线核稿、决定采纳、补范文入库；
- **业务兼职用户**：文种引导 + 通用兜底起草，权限较窄（不含模型接入配置、脱敏库维护、用户管理、审计查看）；
- **审计员（可选）**：只读审计与脱敏留痕，不得执行任何写操作或业务起草操作（职责分离）。

#### Scenario: 角色—权限映射以 Postgres 为权威
- **WHEN** 需要判定某操作是否被某角色授权
- **THEN** 系统必须查询 PostgreSQL 中的角色—权限映射作出判定，不得使用与 Postgres 不一致的硬编码副本

#### Scenario: 审计员只读不可写
- **WHEN** 审计员发起任何写操作或公文起草 / 采纳类操作
- **THEN** 系统必须拒绝该操作，仅允许其只读查看审计与脱敏留痕

#### Scenario: 业务兼职用户权限收窄
- **WHEN** 业务兼职用户请求模型接入配置、脱敏库维护、用户管理或审计查看等操作
- **THEN** 系统必须拒绝，仅放行文种引导与通用兜底起草相关操作

### Requirement: 基于操作权限点的授权判定
系统应当（SHALL）以离散的"操作权限点"描述受控操作，并对外提供"某 principal 是否可执行某操作"的授权判定能力供其它 change 统一调用。判定必须（MUST）依据 principal 当前所属角色与角色—权限映射；映射中未授予的操作必须默认拒绝（fail-closed，不授权即拒绝）。授权判定只回答"能否做该操作"，不得替代密级层对"能看 / 能外发什么数据"的判定（两层正交，见 access-control-composition）。

#### Scenario: 已授权操作放行
- **WHEN** principal 所属角色在映射中被授予某操作权限点，且发起该操作
- **THEN** RBAC 判定必须返回允许（最终放行还需通过密级叠加判定）

#### Scenario: 未授权操作默认拒绝
- **WHEN** principal 所属角色未被授予某操作权限点
- **THEN** RBAC 判定必须返回拒绝（默认拒绝），且对未显式声明的新操作同样默认拒绝

#### Scenario: 角色变更后判定即时生效
- **WHEN** 系统管理员变更某 principal 的角色后，该 principal 发起一项受新角色影响的操作
- **THEN** 授权判定必须依据更新后的角色—权限映射作出，不得沿用变更前的授权结果

### Requirement: 系统管理员用户 / 角色管理
系统应当（SHALL）为系统管理员提供用户与角色管理能力：创建账号、停用账号、为账号分配 / 变更角色、重置密码。这些管理操作必须（MUST）仅对系统管理员角色开放；任何此类变更（创建 / 停用 / 角色分配 / 密码重置）必须写入审计留痕，记录操作者、被操作账号、变更内容与时间。MVP 为单租户，用户与角色数据按本部署内的部门 / 用户 / 密级隔离，必须不引入多租户结构。

#### Scenario: 系统管理员创建并停用账号
- **WHEN** 系统管理员创建一个新账号并随后将其停用
- **THEN** 系统应当完成账号创建与停用，且两次变更均产生审计留痕（停用后该账号不得再通过认证，见 local-identity-auth）

#### Scenario: 分配角色后授权随之变化
- **WHEN** 系统管理员将某账号的角色由业务兼职用户改为文秘
- **THEN** 该账号后续的 RBAC 判定必须按文秘角色的权限映射进行，且该角色变更产生审计留痕

#### Scenario: 非管理员无法执行用户管理
- **WHEN** 文秘、业务兼职用户或审计员发起创建账号 / 停用账号 / 分配角色 / 重置密码等管理操作
- **THEN** 系统必须拒绝该操作

#### Scenario: 重置密码留痕
- **WHEN** 系统管理员重置某账号密码
- **THEN** 系统必须更新该账号的加盐哈希密码并写入审计留痕，留痕中不得包含新密码原文或哈希

### Requirement: 权限点登记中心收口登记离散操作权限点
系统应当（SHALL）作为「权限点登记中心」，收口登记由各业务 change 声明、本 change 落 Postgres 为权威的离散操作权限点及其默认角色—权限映射；各消费方 change（c08 / c02 / c01 / c03 / c05 / c07 等）声明的命名权限点必须（MUST）由本登记中心统一登记，不得在各 change 自立口径或硬编码副本。本 change 收口登记的命名权限点至少包含：`document.open` / `document.edit` / `document.export`（文档打开 / 编辑 / 导出，供 c08）、`review.online`（在线核稿，供 c08）、`adopt.decide`（采纳决策，供 c08）、`audit.read`（审计只读，覆盖本 change「账号安全审计表」与 c02「脱敏审计表」）、`dict.manage`（脱敏库维护，供 c02）、`model.config`（模型配置，供 c01）、`template.search` / `template.ingest`（范文检索 / 入库，供 c03）、`draft.create`（起草，供 c05 / c07）、`user.manage`（用户 / 角色管理，本 change 自有）。默认角色—权限映射必须（MUST）为：
- **系统管理员**：`model.config`、`dict.manage`、`audit.read`、`user.manage`；
- **文秘（核心）**：`document.open`、`document.edit`、`document.export`、`review.online`、`adopt.decide`、`template.search`、`template.ingest`、`draft.create`；
- **业务兼职用户**：`document.open`、`document.edit`、`document.export`、`draft.create`、`template.search`（收窄面，不含 `model.config` / `dict.manage` / `user.manage` / `audit.read` / `review.online` / `adopt.decide` / `template.ingest`）；
- **审计员（可选）**：仅 `audit.read`（只读，无任何写 / 业务起草权限点）。

#### Scenario: 命名权限点经登记中心收口登记
- **WHEN** c08 / c02 / c01 / c03 / c05 / c07 等消费方 change 声明其受控操作所需的命名权限点（如 `document.open` / `dict.manage` / `model.config` / `template.search` / `draft.create` 等）
- **THEN** 系统必须由本登记中心将其统一登记到 PostgreSQL 权限点表为权威，消费方据已登记权限点经统一判定点判定，不得另立口径或使用与库不一致的硬编码副本

#### Scenario: 默认角色—权限映射按登记口径落地
- **WHEN** 初始化角色—权限映射种子数据
- **THEN** 系统必须按上述默认映射落 Postgres：系统管理员映射 `model.config` / `dict.manage` / `audit.read` / `user.manage`；文秘映射 `document.open` / `document.edit` / `document.export` / `review.online` / `adopt.decide` / `template.search` / `template.ingest` / `draft.create`；业务兼职用户映射 `document.open` / `document.edit` / `document.export` / `draft.create` / `template.search`；审计员仅映射 `audit.read`，且业务兼职用户不得含 `model.config` / `dict.manage` / `user.manage` / `audit.read` / `review.online` / `adopt.decide` / `template.ingest`
