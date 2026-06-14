# 仓库指南

## 项目结构与模块组织

本仓库当前是 spec-first 项目。产品需求位于 `docs/prds/`，架构决策位于 `docs/adr/`，支撑材料位于 `docs/other/`，OpenSpec 工作区位于 `openspec/`。进行中的变更使用 `openspec/changes/cNN-kebab-case/` 目录，目录内包含 `proposal.md`、`design.md`、`tasks.md`，能力增量规格放在 `specs/<capability>/spec.md`。

当前尚未引入应用源码目录。根据 ADR-0001，后续实现应将后端 Go 代码放在边界清晰的 `internal/...` 包中，将前端 React/TypeScript 代码放在应用级 frontend 目录中，并将部署、配置文件与产品规格文档分开维护。

## 构建、测试与开发命令

仓库目前没有已提交的构建清单，例如 `go.mod`、`package.json` 或 `Makefile`。当前常用命令如下：

- `rg --files` - 快速查看仓库文件范围。
- `git status --short` - 编辑前检查本地变更。
- `git log --oneline -10` - 查看近期提交风格。

添加可运行代码后，应在本文件和相关 README 中记录准确命令。预期基线包括 Go 侧的 `go test ./...`，以及前端 `package.json` 中定义的 package-manager scripts。

## 编码风格与命名约定

OpenSpec 产物使用简体中文，遵循 `openspec/config.yaml`；产品名、协议、API、技术栈和标识符保留英文。OpenSpec 变更目录命名为 `cNN-short-topic`，能力规格目录使用 kebab case，例如 `model-provider-abstraction`。

任务清单使用 `- [ ] X.Y 描述` 格式，并按依赖顺序排列。规格场景使用 `#### Scenario:` 标题，并用清晰的 WHEN / THEN 行为描述。不要在多个文件中重复维护版本、许可等 ADR 事实；应引用 ADR-0001。

## 测试指南

规格工作中，每条 requirement 至少应包含一个可验证 scenario。实现工作中，应在被修改的 package 或 feature 附近补充契约测试，尤其是 model-provider abstraction、fail-closed 脱敏、RBAC 和 streaming 行为。没有记录验证证据前，不要将任务标记为完成。

## 提交与 Pull Request 规范

近期提交使用简洁的 Conventional Commit 风格，例如 `spec(c08): 在线编辑器提案...`、`chore: ...`、`init: ...`。存在明确范围时，使用 `type(scope): summary`。

远程分支已设为保护分支，禁止直接 push 到受保护分支。每次 commit 之后，必须推送到工作分支并创建 Pull Request；不得把本地 commit 作为最终交付。

Pull Request 创建后，必须启动子 agent 对 PR 进行审查。审查无阻塞问题时，才允许合入；审查发现问题时，先修复问题并追加 commit，更新同一个 PR，再次启动子 agent 审查。重复该流程，直到 PR 审查通过后再合入。

Pull Request 应说明关联的 change ID，总结影响到的 specs 或代码路径，列出已执行的验证，并明确是否影响密级路由、脱敏、RBAC、存储边界或 ADR-0001 决策。

## 安全与配置提示

脱敏与密级路由属于安全关键路径。不要记录 API key、原始映射表或敏感原文。凡是涉及模型出站、NER fallback、审计日志、公网 / 私有路由的变更，都必须保持 ADR-0001 中定义的 fail-closed 默认策略。
