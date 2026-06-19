## 1. 前置盘点与配置数据落库

- [ ] 1.1 开工前按 9 个高频文种盘点客户存量范文数量（对照 <100 条稀缺线，每文种宜 ≥100 篇），登记到位时间与清洗 / 脱敏入库责任方，产出"哪些文种样本充足、哪些不足"的盘点结论作为灰度放开顺序输入（对齐 PRD Risks「范文库数据不足」）；文种 / 子类的能力档与是否标黄分级判定属 c06，本盘点仅供 c06 与灰度参考、不在 c05 落分级配置；验证：每个文种有明确的"样本是否充足"标注，作为后续灰度放开顺序输入。
- [x] 1.2 在 PostgreSQL 设计文种结构契约配置表：承载 9 个高频文种的"行文关系（上行 / 下行 / 平行）→ 必备要素清单"等结构契约标量字段（对齐 design D-1、ADR-0001 D5）；不承载文种 / 子类能力档分级与是否标黄稀缺（属 c06 文种能力档分级表，c06 权威）；验证：表结构能表达任一高频文种的行文关系与必备要素清单，可被 SQL 查询，且不含能力档 / 标黄字段。（实现：`backend/migrations/000007_high_freq_doctype_contracts.sql` 新增 `high_freq_doctype_structure_contracts`，字段覆盖文种、行文方向、标题 / 称谓 / 主送 / 正文结构 / 必备要素 / 结尾 / 落款 / 口吻 / 红线及模板引用；不包含 `capability_tier`、`is_starred_rare`、`target_capability` 等 c06 分级字段。对应 `TestMigrationDefinesHighFreqStructureContractsWithoutC06ClassificationFields`。）
- [x] 1.3 为 9 个高频文种初始化结构契约配置：标注每个文种行文关系（请示 / 报告上行、通知 / 通报 / 批复下行、函平行）与必备要素清单；文种 / 子类的能力档分级与标黄标记不在本 change 初始化（属 c06）；验证：抽查 9 文种行文关系与必备要素清单与 PRD「文种覆盖矩阵 A」结构口径一致，表内无能力档 / 标黄分级配置。（实现：`internal/draft/structure_contract.go` 提供 `DefaultStructureContracts` 覆盖 9 个高频文种，行文关系按 c05 口径初始化，必备要素复用 c06 `DefaultRequiredSlots` 作为要素名称来源；`SeedStructureContracts` 幂等写入 PostgreSQL 且不写入 c06 分级字段。对应 `TestDefaultStructureContractsCoverNineHighFreqDoctypes` / `TestSeedStructureContractsInsertsWithoutClassificationFields`。）
- [x] 1.4 将 9 个文种的提示模板文本（标题构成 / 称谓 / 主送 / 正文段落结构与必备要素 / 落款 / 口吻指令 / few-shot 框架文本 / 机关口径红线指令）入 MinIO 业务桶并记版本，与 1.2 标量配置分层存放（对齐 design D-1）；验证：按文种标识 + 版本可从 MinIO 取到对应模板文本，措辞调整改对象不改代码、无需重发版。（实现：`internal/draft/prompt_template.go` 定义 `PromptTemplateObject`、版本化对象键、`SeedPromptTemplateObjects` 与 `GetPromptTemplateObject`，以业务对象存储接口承接 MinIO 业务桶写入 / 读取；`DefaultPromptTemplateObjects` 覆盖 9 个文种并包含标题、称谓、主送、正文结构与必备要素、落款、口吻、Few-shot、机关口径红线段落；`DefaultStructureContracts` 同步写入 `template_object_key` / `template_version`。对应 `TestDefaultPromptTemplateObjectsCoverNineDoctypesAndSections` / `TestGetPromptTemplateObjectReadsStoredContentByDoctypeAndVersion`。）

## 2. 文种结构契约库（doctype-prompt-templating）

- [x] 2.1 实现"按文种取结构契约"读取层：以文种标识从 PostgreSQL（1.2）+ MinIO（1.4）组装该文种的标题 / 称谓 / 主送机关 / 正文段落结构 / 落款各要素规范定义；生成编排代码内不出现 `if 文种==请示` 类硬编码分支（对齐 spec「9 个高频文种的结构契约库」、design D-1）；验证：对"请示"取契约返回的必备要素集合覆盖该文种全部必备要素、无缺漏；对各文种重复同一验证。（实现：`internal/draft/structure_contract_reader.go` 新增 `StructureContractReader`，组合 `StructureContractStore`（可接 PostgreSQL）与 `PromptTemplateObjectReader`（可接 MinIO 业务桶）按文种读取完整契约；模板对象按结构契约记录的 `template_object_key` / `template_version` 获取，不在读取层写 `if 文种==...` 分支。对应 `TestStructureContractReaderAssemblesAllHighFreqContracts` / `TestStructureContractReaderUsesStoredTemplateObjectKey`。）
- [x] 2.2 实现"非高频文种不返回深做契约"防御语义：对不属于 9 个高频文种的文种标识（正常路由下不应出现，因 c06 仅将这 9 个深做文种判为 capability==c05），明确返回"无深做契约"，不得返回某高频文种契约，且不在本 change 内自行判定移交 c07（文种分流与降级移交单一权威 = c06）（对齐 spec 同名 Scenario）；验证：传入非 9 文种标识，断言返回无深做契约且不含 c05 自行移交 c07 的判定。（实现：`internal/draft/structure_contract_reader.go` 新增 `ErrNoDeepStructureContract` / `NoDeepStructureContractError`，在结构契约 store 未命中时返回显式无深做契约语义，且不触发模板对象读取、不携带 c07 / fallback 移交判断。对应 `TestStructureContractReaderReturnsNoDeepContractForNonHighFrequencyDoctype`。）
- [x] 2.3 在契约中固化行文关系口吻规则：上行文（请示 / 报告）要求呈请 / 报告口吻并含相称结束语（如请示"妥否，请批示"）；下行文（通知 / 通报 / 批复）要求部署 / 告知口吻；平行文（函）要求商洽 / 询问口吻（对齐 spec「行文关系决定口吻与措辞」）；验证：同一事由分别按请示（上行）与通知（下行）取契约，两者施加的口吻 / 措辞约束不同，不共用同一套口吻规则。（实现：`internal/draft/structure_contract.go` 在默认结构契约中固化请示 / 报告上行口吻与结束语、通知 / 通报 / 批复下行口吻、函平行商洽 / 询问口吻；`internal/draft/structure_contract_test.go` 新增 `TestDefaultStructureContractsCarryDirectionToneRules`，验证请示与通知同一事由取契约时口吻 / 结束语约束不同且不共用同一套口吻规则。）
- [x] 2.4 在每个契约内置机关口径红线约束：政治性表述准确、不臆造事实 / 数据 / 文号 / 人名单位、不输出敏感或不合规表述；用户未提供的关键要素以占位或明确待补提示呈现而非编造；产出恒定性为"待人工核稿的初稿"，不含任何自动发文 / 自动提交语义（对齐 spec「机关口径红线约束」、design D-5、PRD「人工最终把关」）；验证：缺文号 / 单位全称的场景生成结果用占位 / 待补提示，不出现虚构文号或单位名。（实现：`internal/draft/structure_contract.go` 在 9 个高频文种默认结构契约共享的 `RedlineRules` 中固化政治性准确、禁止臆造事实 / 数据 / 文号 / 人名 / 单位名 / 单位全称、禁止敏感或不合规表述、缺关键要素留占位或待补提示、产出仅为待人工核稿初稿且不含自动发文 / 自动提交语义；`BuildPromptTemplateContent` 继续从契约数据写入 `## 机关口径红线` 段落。对应 `TestDefaultStructureContractsCarryOrganRedlineRules`。）

## 3. 范文样例编排与降级口径

- [x] 3.1 实现 few-shot 样例编排契约：样例仅取自 c03（corpus-rag-retrieval）检索接口返回结果，以 few-shot 形式注入、与待写场景要素分区呈现、设注入条数上限（TopK，具体值待 6.x 实测校准）、样例与目标文种 / 子类一致性取用规则（对齐 spec「范文样例编排进提示的契约」、design D-4）；验证：c03 返回若干样例时按契约分区注入且不超上限。（实现：`internal/draft/fewshot.go` 新增 `FewShotInput` / `AssembleFewShotPrompt`，输入只接收 c03 `retrieval.TemplateExample`，按目标文种过滤、按 `MaxExamples` TopK 上限截断，并将待写场景要素与 `## Few-shot 范文样例` 分区输出；提示文本显式标注来源为 c03 `corpus-rag-retrieval`、同文种 / 优先同子类取用规则、TopK 上限。`internal/draft/prompt_template.go` 的模板框架同步写入同文种 / 同子类一致性规则。对应 `TestAssembleFewShotPromptPartitionsC03ExamplesAndScenario` / `TestDefaultPromptTemplateObjectsCoverNineDoctypesAndSections`。）
- [ ] 3.2 实现"不臆造、不改写范文"约束：编排层只使用 c03 返回的脱敏后样例文本，不从其它来源臆造范文、不还原 / 修改 c03 样例内容（对齐 spec 同名 Scenario、design D-4、ADR-0001 D7 脱敏红线）；验证：对编排路径断言除 c03 接口外无其它范文文本来源，且样例文本逐字透传不被改写。
- [ ] 3.3 实现范文不足降级口径：深做文种样例数量低于充足阈值时仍按结构契约拼装提示（不退回无契约通用生成），并在生成元数据标记"范文样例不足、质量可能下降"；深做文种零样例时仍施加结构契约约束、不在无标记情况下退回无文种结构约束输出（对齐 spec「范文不足时的降级口径」、design Risks「范文库数据不足」）；验证：构造零样例 / 少样例两种输入，断言均带结构契约且元数据含样例不足标记。

## 4. 消费 c06 文种分流上下文

- [ ] 4.1 实现 c06 结构化场景上下文契约的消费层：解析 c06 `doctype-classification` 输出（目标 capability、目标文种、代表子类、行文方向、置信度、用户原始场景描述、已补齐要素、缺失 / 未确认要素标记、内容密级（取值：非密 / 敏感 / 涉密）），仅当目标 capability==c05 时进入本管线；不在 c05 维护任何文种 / 子类能力档分级配置、不复算是否标黄稀缺、不自行判定移交 c07（文种分流与降级移交单一权威 = c06）、不自建密级判定，仅取出上下文中的内容密级备作出站请求密级透传（对齐 c06 文种分流单一权威、design D-2 / D-6）；验证：传入 capability==c05 的上下文进入深做管线并解析出内容密级，传入非 c05 capability 时本管线不处理且不自行移交。

## 5. 初稿生成编排与流式输出（draft-generation-streaming）

- [ ] 5.1 定义"高频文种初稿生成"请求 / 响应契约：入参为 c06 输出的结构化场景上下文契约（目标 capability / 目标文种 / 代表子类 / 行文方向 / 置信度 / 用户原始场景描述 / 已补齐要素 / 缺失 / 未确认要素标记 / 内容密级（取值：非密 / 敏感 / 涉密）），仅处理 capability==c05；出参为符合结构契约的规范正文（标题 / 称谓 / 主送 / 正文结构 / 落款），并随响应回传所消费的文种 / 子类等上下文标识，不在本接口重新判别文种或复算分级、不自建密级判定（对齐 spec「高频文种初稿生成请求 / 响应契约」）；验证：提交 capability==c05 的 c06 上下文，返回规范正文 + 回传上下文标识，且无文种重判 / 分级复算。
- [ ] 5.2 实现固定编排管线：① 消费 c06 上下文（4.1），仅 capability==c05 进入、不复算分级、不自行移交 c07 → ② 据上下文文种 / 子类调 c03 按文种过滤检索范文 → ③ 按 2.x / 3.x 契约与 few-shot 规则拼装提示 → ④ 仅经 c01 窄抽象接口发起生成（对齐 spec「生成编排顺序」、design D-2）；验证：capability==c05 时检索先于生成、以 c06 上下文文种为准；非 c05 capability 不进入本管线。
- [ ] 5.3 确保模型调用只经 c01（model-provider-abstraction）窄抽象接口，编排模块导入图中不出现 `openai-go`、`anthropic-sdk-go` 或任何 LiteLLM 专有客户端类型（对齐 spec「生成仅经 c01 窄抽象接口」、design D-2、ADR-0001 D3）；验证：对编排模块执行依赖扫描断言导入图无上述具体 SDK / LiteLLM 类型。
- [ ] 5.4 实现 SSE 流式输出：复用 c01（model-streaming-passthrough）三类归一事件（文本增量 / 正常结束 / 异常结束），按顺序增量下发正文片段、正常结束发完成事件，仅在流首 / 流尾追加本 change 元数据（所消费的文种 / 子类等上下文标识），不另造第二套 SSE 帧格式（对齐 spec「SSE 流式输出初稿」、design D-3、ADR-0001 D2）；验证：流式拼接所得正文与同参数一次性生成正文内容等价。
- [ ] 5.5 输出限于规范正文内容，不含 GB/T 9704 版式 / 红头 / 字体字号等排版结果（版式后置）（对齐 spec「输出不含版式排版」、design Non-Goals、PRD Scope）；验证：抽查各文种输出，无任何版式 / 红头 / 字号字段。
- [ ] 5.6 在"高频文种初稿生成"接口入口接入操作层 RBAC：发起起草前消费 c04 `rbac-authorization` 的授权决策（`draft.create` 权限点），未授予即拒绝（fail-closed），本 change 不自建 RBAC 判定；该操作层 RBAC 与 c03 落地的密级数据 ACL 预过滤两层正交叠加、不可相互替代（对齐 spec「未授权角色不可发起起草」、c04 权限点登记中心、ADR-0001 D10）；验证：未授予 `draft.create` 的角色发起起草被拒绝（fail-closed），授予者放行；构造 RBAC 通过但密级数据 ACL 不通过（及反之）两组样例，均不因单层通过而放行。

## 6. 边界语义与窄抽象红线断言

- [ ] 6.1 实现调用方主动中断语义：用户取消 / 连接断开时及时停止该次生成并释放资源，已下发片段标识为未完成、不得标记为完成结果（对齐 spec「生成中断与失败语义」）；验证：流式进行中触发取消，断言生成停止、资源释放、片段标未完成。
- [ ] 6.2 实现底层失败语义：c01 模型调用失败 / 超时时经流发出明确错误事件并终止该流，不静默挂起、不把半截正文伪装为正常完成（对齐 spec 同名 Scenario、design D-3 复用 c01 异常结束事件）；验证：注入模型失败，断言流发错误事件且半截正文未被标完成。
- [ ] 6.3 实现"模型调用仅经 c01 窄抽象、不绕过其直连 SDK"红线断言：脱敏与出站密级路由由 c01 公网分支上的 c02 强制拦截点承接，c05 不直接面对公网、不自行实现或断言该脱敏 / 路由环节，仅保证编排层无绕过 c01 窄抽象直连具体供应商 SDK 或 LiteLLM 客户端的旁路；同时将 c06 上下文中的内容密级（取值：非密 / 敏感 / 涉密）作为出站请求密级随 c01 窄抽象调用透传供 c02 路由，仅透传该密级输入值、不自建密级判定（对齐 spec「模型调用仅经 c01 窄抽象接口、脱敏与密级路由由 c02 拦截点承接」、design Risks、ADR-0001 D7）；验证：对编排路径断言全部模型调用经 c01 窄抽象接口、导入图无具体 SDK / LiteLLM 类型、无直连旁路，且出站调用携带 c06 上下文中的内容密级。

## 7. 验证、灰度与采纳率校准

- [ ] 7.1 建立验收 rubric 四维（文种规范 / 结构完整 / 行文关系 / 机关口径）+ 采纳率口径的样稿评测集，覆盖 9 文种代表性子类（对齐 PRD「验收 rubric 草案」「Success Metrics」）；验证：每个深做文种有可重复打分的样稿与评分记录。
- [ ] 7.2 灰度放开顺序按 1.1 盘点结论：先开放样本充足的深做文种（如通知 / 请示 / 报告）给试点文秘，按 rubric 四维 + 采纳率打标，再逐步放开其余文种（对齐 design Migration Plan 第 3 步）；验证：每批灰度有采纳状态（直接用 / 小改 / 大改 / 弃用）标注。
- [ ] 7.3 用真实范文与目标模型实测校准 few-shot 注入条数上限与提示总长上限、契约措辞（具体取值待首版实测后定档，回填 3.1）（对齐 design Open Questions、PRD「指标阈值待实测定档」）；验证：给出一组校准后的 TopK / 总长取值及其采纳率 / 速度依据。

## 8. 信创 / 私有化高风险项（待 PoC）

- [ ] 8.1 待 PoC：国产模型在 9 个高频文种上的效果摸底——用校准评测集（7.1）评估国产 / 私有化模型产出的 rubric 四维得分与采纳率，判定深做文种在私有化阶段是否仍达"像公文"质量、哪些文种需降级（对齐 PRD Risks「数据稀缺 / 效果验证」、ADR-0001 §5 待确认项）；验证：产出各文种国产模型得分对照表与是否达标结论。
- [ ] 8.2 待 PoC：c01 由 LiteLLM Proxy 切 Go 直连官方 SDK 后，本 change 生成编排业务代码零改动验证——确认 5.3 的窄抽象隔离在后端切换下成立、SSE 三类归一事件仍正常（对齐 ADR-0001 D3、design D-2 / D-3）；验证：切换后端后重跑 5.2 / 5.4 流式与编排用例全绿、编排模块无改动。
- [ ] 8.3 待 PoC：龙芯 LoongArch64 与 ARM64 + 麒麟下生成编排服务（Go 单二进制应用层）运行验证——确认单二进制在国产 CPU / OS 上可运行、SSE 流式输出与 PostgreSQL / MinIO / c01 / c03 依赖连通（对齐 ADR-0001 D1 / §5、PRD「私有化 / 信创部署」后置项）；验证：在龙芯与麒麟环境各跑通一次端到端初稿流式生成。
- [ ] 8.4 边界确认（非本 change 实现）：本 change 仅交付生成接口与流式契约，在线编辑 / 核稿（OnlyOffice）属 c08；采纳稿回流由 c08 产回流信号、经 c08 与 c03 约定的同一 outbox 交付、c03 单点入库并对账，c05 不承担回流（遵 c08 design D-6 回流信号契约 + c03 corpus-ingestion 单点入库 + ADR-0001 §2/D5），无需在本 change 实现或对齐回流归属；OnlyOffice 信创认证（麒麟有企业版认证、统信 / 龙芯待确认）由 c08 / 商务承接；验证：本 change 不含任何回流入库代码路径，OnlyOffice 认证项登记到风险表且明确归属 c08。
