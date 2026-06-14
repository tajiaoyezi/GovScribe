## ADDED Requirements

### Requirement: 成稿导出为 .docx
系统必须（MUST）允许具备导出权限的文秘把成稿导出为 .docx 文件以带走精排。若导出在 OnlyOffice 编辑场景之外、需纯 Go 实现时，导出库选型必须遵 ADR-0001 D8（用 MIT 许可的 `mmonterroca/docxgo`，必须排除 AGPL 的 `unioffice`、`fumiama/go-docx` 以免污染闭源单二进制）。导出权限必须以 c04 登记的 `document.export` 权限点经 c04 的 RBAC 与密级叠加判定校验，不另立口径。导出的 .docx 必须与所导出成稿版本的内容一致。

#### Scenario: 文秘导出成稿 .docx
- **WHEN** 具备导出权限的文秘对某一成稿版本点击导出
- **THEN** 系统必须生成与该版本内容一致的 .docx 文件并提供给文秘下载

#### Scenario: 无导出权限被拒绝
- **WHEN** 不具备导出权限（RBAC 或密级判定不通过）的用户请求导出
- **THEN** 系统必须拒绝该请求并提示无权限，不得生成或返回任何文件

#### Scenario: 纯 Go 导出不引入 AGPL 库
- **WHEN** 在 OnlyOffice 编辑场景之外以纯 Go 实现 docx 导出
- **THEN** 实现必须采用 MIT 许可的导出库，且不得依赖 `unioffice` 或 `fumiama/go-docx` 等 AGPL 库

### Requirement: 统一导出抽象 export(docx) 与 export(ofd) 接口位
系统必须（MUST）提供统一的导出抽象接口，对业务以目标格式参数化（如 `export(docx)`）；MVP 必须实现 `export(docx)`，并必须预留 `export(ofd)` 接口位但 MVP 不实现（OFD 暂不支持，遵 ADR-0001 D9）。当请求 `export(ofd)` 时，系统必须返回明确的"暂不支持"结果，不得回退为静默失败或产出错误格式文件。导出抽象必须与具体编辑器/导出后端解耦，保证后端可替换。

#### Scenario: 经统一抽象导出 docx
- **WHEN** 业务侧以目标格式 docx 调用统一导出接口
- **THEN** 系统必须经 `export(docx)` 实现产出 .docx 文件

#### Scenario: 请求 ofd 返回暂不支持
- **WHEN** 业务侧以目标格式 ofd 调用统一导出接口
- **THEN** 系统必须返回明确的"暂不支持 OFD"结果（遵 D9），不得静默失败、不得产出非 OFD 内容冒充 OFD

#### Scenario: 新增导出后端不改业务调用
- **WHEN** 未来接入 OFD 导出后端以实现 `export(ofd)`
- **THEN** 仅需在导出抽象下补充后端实现，业务侧的统一导出调用契约不得改动
