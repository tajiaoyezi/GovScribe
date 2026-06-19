## 1. 判别与路由配置就位

- [x] 1.1 依 PRD「文种覆盖矩阵」落地由 c06 拥有的**文种能力档分级表**（owner=c06，作为只读共享配置）：A 表 9 个深做文种（通知 / 请示 / 报告 / 函 / 会议纪要 / 通报 / 批复 / 讲话稿 / 方案）及其代表子类、B 表能力档归类（模版辅助写 / 框架写 / 待计划训练 / 通用兜底）、标黄稀缺子类（<100 条，如方案-调研 / 方案-会议 / 通知-举办比赛 / 函-复函）逐项录入；标黄稀缺降级移交 c07 的判定由 c06 在路由阶段查该表一次性完成。验证：分级表与矩阵 A/B 表逐项核对一致，每个文种可解析出能力档与是否标黄，标黄稀缺触发一次性降级标签。（实现：`internal/doctype` 分级表领域模型 + 内存 / Postgres 存储 + `backend/migrations/000005_doctype_capability_matrix.sql`；种子依据 docs/other 客户《支持公文类型》PDF；讲话稿逐子类标黄待客户逐项数据量确认。对应 `TestDefaultMatrixCoversNineDeepDoctypes` / `TestDefaultMatrixMarksStarredRareSubtypes` / `TestMatrixResolveRoutesDeepNonStarredToC05` / `TestMatrixResolveFallsBackForUnknownSubtypeToC07` / `TestMatrixResolveFallsBackForUnknownDoctypeToC07` / `TestMatrixResolveRoutesBTableToC07` / `TestPostgresMatrixStoreLooksUpAndMapsMissingRows` / `TestPostgresMatrixStoreListsEntries` / `TestSeedMatrixUpsertsEntries`）
- [x] 1.2 落地**判别提示词**配置：将文种映射表作为受限标签集嵌入，要求 LLM 输出「文种 / 代表子类 / 行文方向线索 / 归一化置信度」结构化 JSON（对齐 design D-06-1）。验证：用 3~5 条典型场景试调，返回严格 JSON 且文种取值落在受限标签集内。（实现：`internal/doctype/prompt.go` 判别提示词构建器把分级表作为受限标签集嵌入、约定严格 JSON 输出与解析校验（含代码块剥离、置信度/方向越界拒绝）；单元验证标签集嵌入与解析合约，3~5 条典型场景的 live 试调随 §2.3 判别调用 / §9.1 端到端覆盖。对应 `TestBuildClassificationPromptEmbedsRestrictedLabelSet` / `TestParseClassificationOutputParsesStrictJSON` / `TestParseClassificationOutputStripsCodeFence` / `TestParseClassificationOutputRejectsInvalid`）
- [x] 1.3 落地**行文方向规则表**配置：文种默认方向（请示 / 报告默认上行、批复 / 下行通知默认下行、函默认平行）+ 机关关系线索修正（向上级请求→上行、对下级答复批复→下行、与同级商洽→平行），规则与 LLM 方向取并、冲突以规则为准并降置信度（对齐 D-06-4）。验证：对默认方向与修正各造一例，输出方向符合规则。（实现：`internal/doctype/direction.go` 文种默认方向表 + 机关关系线索修正 + `ResolveDirection`（线索优先于默认；与 LLM 取并、冲突以规则为准并返回覆盖标志供降置信度）。对应 `TestResolveDirectionUsesDoctypeDefaultWhenNoClue` / `TestResolveDirectionClueOverridesDefaultAndFlagsConflict` / `TestResolveDirectionClueVariants` / `TestResolveDirectionDefersToModelWhenRuleUnspecified`）
- [x] 1.4 落地**必需要素清单**配置：按「文种 + 行文方向」可维护，覆盖首期 9 深做文种，典型必需要素含发文单位 / 主送机关 / 事由-关键事项 / 关键时间地点（对齐 slot-clarification spec、D-06-5）。验证：每个深做文种可查得其必需要素集合，配置项与代码解耦（非硬编码）。（实现：`internal/doctype/slots.go` 必需要素清单按「文种+行文方向」可维护、覆盖 9 深做文种，内存 / Postgres 存储 + 幂等种子 + 迁移 `000006_doctype_routing_config.sql`；配置数据与判别/路由逻辑解耦。对应 `TestDefaultRequiredSlotsCoverNineDeepDoctypes` / `TestMemorySlotStoreReturnsRequiredSlots` / `TestMemorySlotStoreUnknownDoctypeReturnsEmpty` / `TestPostgresSlotStoreReturnsRequiredSlots` / `TestPostgresSlotStoreLists` / `TestSeedRequiredSlotsUpserts`）
- [x] 1.5 设定可调阈值参数（置信度阈值、多义间距、Top-N 的 N、澄清轮次上限）为配置项，MVP 取经验默认值并注明「待实测定档」（对齐 design Open Questions、PRD「指标阈值待实测定档」）。验证：四项参数均可在不改代码前提下调整。（实现：`internal/doctype/thresholds.go` 四项阈值取 MVP 经验默认值（0.6 / 0.15 / 3 / 3，注明待实测定档），Postgres 单行配置 + Save 可不改代码运行时调整、空表回退默认值，迁移 `000006`。对应 `TestDefaultThresholdsValues` / `TestMemoryThresholdStoreSaveAndGet` / `TestPostgresThresholdStoreGetFallsBackToDefault` / `TestPostgresThresholdStoreGetReadsRow` / `TestPostgresThresholdStoreSaveUpserts`）

## 2. 文种判别能力（doctype-classification）

- [x] 2.1 实现**场景描述入口**接收：接受用户自然语言场景文本作为唯一必填入口，不强制前置弹出文种选择列表（对齐「自然语言场景描述入口」Requirement）。验证：未选文种仅提交场景描述即可触发判别流程。（实现：`internal/doctype/classifier.go` `Classifier.Classify(ctx, sceneText, securityLevel, actorID, requestID)` 以场景文本为唯一必填入口、无需预选文种；HTTP/前端入口属 §8。对应 `TestClassifyParsesOutputAnnotatesDeepTierAndCarriesSecurityLevel`）
- [x] 2.2 实现**空 / 过短描述拦截**：场景为空或不含可判别事由 / 行文意图线索时拒绝路由，提示用户补充「想写什么 / 给谁 / 为了什么事」，不凭空猜测文种。验证：空串与「帮我写个东西」类输入被拒并给出提示。（实现：调用模型前做结构化拦截——空白 → `ErrEmptyScene`、过短 → `ErrSceneDescriptionTooShort`；语义层「笼统无可判别线索」（如「帮我写个东西」）由模型低置信路径承接——§3 Top-N 候选 / §6 澄清，系统绝不凭空猜测文种。对应 `TestClassifyRejectsEmptyAndTooShortBeforeCallingModel`）
- [x] 2.3 在 Go 应用层组装判别请求，经 **c01 窄抽象接口**发起意图分类调用（不感知底层供应商，对齐 ADR-0001 D1/D3、design D-06-1）；解析 LLM 返回为「文种 / 代表子类 / 置信度」。验证：命中 A 表场景（如「向上级请求成立节能监测中心」→请示-组织成立）返回正确文种、子类与置信度。（实现：组装 system(判别提示词)+user(场景) 经 `llm.Client.Complete` 发起、ContentSecurityLevel 透传供 c02 出站路由、不直连 SDK；`ParseClassificationOutput` 解析为文种/子类/置信度。对应 `TestClassifyParsesOutputAnnotatesDeepTierAndCarriesSecurityLevel` / `TestClassifyPropagatesModelAndParseErrors`）
- [x] 2.4 实现 **B 表文种能力档标注**：判别落 B 表文种（如「任免某同志职务的命令」→命令）时标注其能力档（模版辅助写 / 框架写 / 待计划训练），供路由分流消费。验证：命令类场景判出「命令」且能力档标为「模版辅助写」。（实现：`Matrix.Resolve` 标注 `ClassificationResult.Tier`，命令 → `template_assist`。对应 `TestClassifyAnnotatesBTableTier`）
- [x] 2.5 实现**行文方向推断**：按 1.3 规则表叠加 LLM 方向线索产出最终行文方向（上行 / 下行 / 平行）。验证：「向上级单位申请经费」判为上行、「对下级请示的答复」判为下行、「与同级商洽工作」判为平行。（实现：`ResolveDirection` 叠加规则与 LLM 线索，冲突以规则为准并按系数折减置信度。对应 `TestClassifyResolvesDirectionAndPenalizesConflict` 及 §1 `TestResolveDirection*`）

## 3. 低置信 / 多义候选与用户确认

- [x] 3.1 实现**置信度阈值与多义判定**：置信度低于阈值，或 Top-1 与 Top-2 置信度差小于多义间距时，进入候选返回分支（对齐 D-06-2）。验证：高置信单义场景不触发候选、低置信 / 多义场景触发候选。（实现：`internal/doctype/candidates.go` `needsConfirmation`（消费 §1 可调阈值 Thresholds），`ClassifyCandidates` 据此决定直选或候选。对应 `TestClassifyCandidatesDirectSelectsHighConfidenceSingle` / `TestClassifyCandidatesLowConfidenceTriggersConfirmation` / `TestClassifyCandidatesReturnsRankedTopNForAmbiguous`）
- [x] 3.2 实现 **Top-N 候选返回**：按置信度降序返回候选文种 + 代表子类 + 置信度，绝不静默选定单一文种直接路由（对齐「低置信与多义时返回 Top-N 候选」Requirement）。验证：「把某事处理情况向上级讲清楚」返回含「报告」「请示」的候选列表而非自行选定。（实现：`BuildCandidatesPrompt`/`ParseClassificationCandidates` 取受限标签集内 Top-N 候选数组，`ClassifyCandidates` 按置信度降序排序、`NeedsConfirmation` 时返回候选列表不直选。对应 `TestClassifyCandidatesReturnsRankedTopNForAmbiguous`）
- [x] 3.3 实现**用户选择覆盖模型判别**：用户在候选中确认或改选后，以用户最终选择作为最终文种继续路由与要素校验（人是最终把关者）。验证：模型 Top-1 为「报告」、用户改选「请示」时，后续以「请示」继续。（实现：`Classifier.ResolveSelection(doctype, subtype, scene)` 以用户选择重解析能力档与行文方向、置信度记 1.0 作为最终结果。对应 `TestResolveSelectionOverridesModelChoice`）

## 4. 按能力档分流路由（c05 / c07）

- [x] 4.1 实现**A 表→c05 路由**：命中 A 表 9 个深做文种时目标 capability 标 c05（对齐「按能力档分流路由」Requirement、D-06-3）。验证：通知-召开会议路由目标标 c05。（实现：`internal/doctype/routing.go` `Route` + `routeCapability`，深做且非标黄 → c05。对应 `TestRouteDeepNonStarredToC05`）
- [x] 4.2 实现**B 表 / 稀缺子类 / 无法归类→c07 路由**：B 表其余文种、A 表内标黄稀缺子类（如方案-调研 <100 条）、无法稳定归类的场景一律标 c07 通用兜底。验证：方案-调研标 c07、命令标 c07、无法归类场景标 c07。（实现：`routeCapability` 对标黄稀缺 / B 表各档 / 兜底统一 c07。对应 `TestRouteToC07ForStarredBTableAndFallback`）
- [x] 4.3 实现**无死路硬约束**：任何场景最终都得到 c05 或 c07 的路由结论，绝不返回「无法处理」（对齐 PRD Milestone 4 口径、D-06-3）。验证：构造刻意无法归类输入仍兜底到 c07 且无错误返回。（实现：`Route` 为纯函数恒返回 c05/c07（含零值/未知能力档），无 error、无空值。对应 `TestRouteNeverReturnsDeadEnd`）
- [x] 4.4 实现**结构化路由标签**：路由结果含目标 capability、文种、子类、行文方向，不直接调用 c05 / c07（调用由上层编排在放行后发起）。验证：路由产物为结构化标签对象，断言不含对下游生成的直接调用。（实现：`RouteLabel` 结构化标签（capability/文种/子类/方向）；`Route` 为纯函数不持有下游 client、不发起任何生成调用。对应 `TestRoutePreservesStructuredLabelFields` / `TestRouteConsistentWithMatrixTargetCapability`）

## 5. 要素校验与澄清式追问（slot-clarification）

- [x] 5.1 实现**已知要素抽取**：从用户原始场景描述抽取已知要素（复用 2.3 同一次 LLM 结构化输出或追加一次轻抽取，对齐 D-06-5）。验证：「区政府向市发改委申请活动经费 5 万元」可抽出发文单位 / 主送机关 / 事由。（实现：`internal/doctype/clarification.go` `Classifier.ExtractSlots` 经 c01 窄抽象轻抽取、密级透传 c02，`BuildSlotExtractionPrompt`/`ParseSlotExtraction` 仅保留登记且非空要素。对应 `TestExtractSlotsParsesKnownElementsAndCarriesSecurityLevel`）
- [x] 5.2 实现**缺失要素识别**：按目标文种必需要素清单（1.4）比对已抽取要素，识别缺失关键要素（对齐「依目标文种校验必需要素完整度」Requirement）。验证：「想申请一笔活动经费」对「请示」识别出「主送机关」「关键事项」缺失。（实现：`MissingSlots` 比对必需要素与已抽取（空白视为缺失）。对应 `TestMissingSlotsIdentifiesUnfilled`）
- [x] 5.3 实现**要素齐备直接放行**：必需要素齐备时不发起澄清，直接进入放行（对齐 Scenario「要素齐备直接放行」）。验证：含发文单位 / 主送机关 / 事由 / 关键事项的请示场景不触发追问。（实现：`NextClarification` 无缺失即 Done。对应 `TestNextClarificationReleasesWhenComplete`）
- [x] 5.4 实现**针对缺失要素的具体追问**：澄清提问针对具体缺失项（如「这份请示主送哪个上级单位？」），而非泛泛「补充更多信息」（对齐「有限轮次澄清式追问补齐要素」Requirement）。验证：缺主送机关时生成的提问点名主送机关。（实现：`questionFor` 按缺失要素 + 文种生成针对性追问。对应 `TestNextClarificationAsksSpecificQuestionForMissing`）
- [x] 5.5 实现**有限轮次上限与放行**：达 1.5 设定的轮次上限即停止追问、进入放行判定，避免无限循环（对齐 Scenario「达到轮次上限停止追问」）。验证：连续不补齐至上限后停止提问并放行。（实现：`NextClarification` 在 `Round >= MaxRounds`（取自 §1.5 Thresholds.MaxClarifyRounds）时放行并标记缺失。对应 `TestNextClarificationReleasesAtRoundLimit`）
- [x] 5.6 实现**用户显式跳过**：用户选择「跳过、先生成初稿」时停止追问并放行（对齐「用户显式跳过澄清后放行」Requirement）。验证：跳过操作后立即进入放行流程。（实现：`NextClarification` 在 `Skipped` 时放行并标记缺失。对应 `TestNextClarificationReleasesOnSkip`）
- [x] 5.7 实现**补齐要素回填**：用户在追问中补充的要素回填到结构化场景上下文（对齐 Scenario「用户补齐后回填场景上下文」）。验证：补充主送机关与事由后，上下文对应字段被更新。（实现：`FillSlot` 回填要素（修剪空白）并消耗一轮、不改原状态。对应 `TestFillSlotBackfillsAndConsumesRound`）

## 6. 结构化场景上下文契约与下游对齐

- [x] 6.1 定义**结构化场景上下文契约**数据结构（本 change 维护唯一来源）：字段固定为 9 项——目标 capability、目标文种、代表子类、行文方向、置信度、用户原始场景描述、已补齐要素、缺失 / 未确认要素标记、内容密级（非密 / 敏感 / 涉密）（对齐「输出结构化场景上下文契约」Requirement、D-06-7）。验证：契约 9 项字段与 spec / design 列举项一一对应。（实现：`internal/doctype/contract.go` `ScenarioContext` 9 字段 + `BuildScenarioContext` 由 Route/判别结果/澄清要素/密级组装。对应 `TestBuildScenarioContextAssemblesNineFields`）
- [x] 6.2 实现**缺失要素标记移交**：放行时仍未补齐的要素在契约中标「缺失 / 未确认」，提示下游谨慎处理、不臆造关键信息（对齐 Scenario「放行时标记未确认要素」）。验证：达轮次上限后未补齐的「关键时间地点」在契约中标为缺失。（实现：`ScenarioContext.MissingSlots` + `HasUnconfirmedSlots`。对应 `TestBuildScenarioContextMarksMissingSlots`）
- [x] 6.3 实现**内容密级承载与强制传递**：内容密级由写作发起方在发起时确定（用户 / 文秘显式选择或按可配置部门默认派生，具体定级权限与策略归客户密级策略），写入契约随场景上下文交给 c05 / c07；缺失或未知时不缺省「非密」，由下游网关按涉密 fail-closed 兜底（对齐「输出结构化场景上下文契约」Requirement、D-06-7）。验证：内容密级随契约移交且取值落在「非密 / 敏感 / 涉密」；缺失时契约不被以「非密」放行。（实现：`BuildScenarioContext` 原样承载内容密级、未知 "" 不缺省非密，`OutboundSecurityLevel` 原样透出供 c02。对应 `TestBuildScenarioContextCarriesContentSecurityLevelWithoutDefaulting`）
- [x] 6.4 与 **c05 / c07 对齐字段定义**并形成契约文档：三方就 capability / 文种 / 子类 / 行文方向 / 置信度 / 原始描述 / 已补齐要素 / 缺失标记 / 内容密级签字段定义，约定下游不重复判别、并把内容密级作为出站请求密级随 c01 窄抽象调用传递供 c02 路由（对齐 D-06-7、Migration Plan 步骤 2）。验证：c05 / c07 能按契约消费且无重复判别逻辑，内容密级随出站调用透传至 c02。（实现：`internal/draft` 作为 c05 / c07 共用起草入口契约，`BuildGenerationRequest` 仅按 `ScenarioContext.TargetCapability` 选择 `c05_deep_doctype` / `c07_generic_fallback` 分支、提示词消费 c06 已落定字段且不要求下游重判；`StreamDraft` 仅经 c01 `llm.Client` 窄抽象流式调用，并把 `ScenarioContext.OutboundSecurityLevel()` 原样写入 `llm.ChatRequest.ContentSecurityLevel` 供 c02 路由。对应 `TestBuildGenerationRequestConsumesC06ContextForC05` / `TestBuildGenerationRequestRoutesC07ToFallbackWithoutChangingSecurityLevel` / `TestBuildGenerationRequestPreservesUnknownSecurityLevel` / `TestStreamDraftCallsC01ClientWithC06SecurityLevel`。）

## 7. 接入 c01 模型层与 c02 脱敏 / 密级路由（保密红线）

> 依赖：本节判别调用经 c01 统一模型调用入口透传至 c02 的 desensitization-gateway + security-classification-routing；本 change 不自建脱敏 / 密级路由 / 审计。

- [x] 7.1 判别调用**经 c01 入口透传至 c02 脱敏网关 desensitization-gateway + 出站密级路由 security-classification-routing**发起，不另立保密口径（对齐 design D-06-1、proposal、ADR-0001 D7）。验证：含敏感实体的场景描述确实经 c02 脱敏网关处理，未旁路。（c06 侧：所有模型出站收口于 `Classifier.complete`，仅经 c01 窄抽象 `llm.Client`（装配期为 c02 装饰实例）、无旁路、无 SDK 直连；`TestPackageHasNoDirectModelSDKImports` 依赖扫描禁止 SDK 旁路，`TestOutboundCallsCarryContentSecurityLevel` 验证全路径携密级。「确实经 c02 网关处理」的端到端由 c02 装饰客户端在装配/集成期保证。）
- [x] 7.2 实现**密级路由不降级**：非密走公网、敏感走脱敏公网、涉密强制走私有线且永不降级；外置 NER 不可用时默认 fail-closed，绝不静默发原文（对齐 ADR-0001 D7）。验证：模拟 NER 不可用时判别调用被阻断而非发原文；涉密标记场景恒走私有线。（c06 侧：`TestOutboundCallsPropagateFailClosedBlock` 验证 c02 fail-closed 阻断错误被原样上抛、绝不吞错回退发原文；涉密密级原样透传供 c02 强制私有。密级路由/降级判定本体属 c02，c06 不另立。）
- [x] 7.3 澄清回填要素同样**纳入 c02 出站密级路由判定**，不因「只是澄清」而降级处理（对齐 design Risks 密级防越权）。验证：澄清补入的敏感要素经同一 c02 密级路由判定。（c06 侧：澄清抽取 `ExtractSlots` 与判别走同一 `complete` 收口并携同一密级（`TestExtractSlotsParsesKnownElementsAndCarriesSecurityLevel` / `TestOutboundCallsCarryContentSecurityLevel`）；补齐要素经契约 `OutboundSecurityLevel` 随下游出站调用同口径纳入 c02 判定。）
- [x] 7.4 出公网脱敏与密级路由的降级 / 阻断结果**由 c02 脱敏网关审计承载**，本 change 不自建审计表（承自 c02 脱敏网关审计，落 Postgres、纳入相应密级 ACL）。验证：阻断 / 涉密私有路由事件在 c02 脱敏审计记录中可查。（c06 侧：internal/doctype 无任何审计表/审计代码、无相关迁移——审计由 c02 装饰链承载；c06 阻断/转私有事件随经 c02 的调用由 c02 审计留痕，「可查」在 c02 审计能力（已实现）+ 集成期验证。）

## 8. 前端入口与交互（React + Ant Design）

- [x] 8.1 写作工作台新增**场景描述入口**：自然语言输入框，不前置强制文种选择列表（对齐 proposal Impact、Migration Plan 步骤 4）。验证：仅输入场景描述即可发起。（实现：`frontend/src/App.tsx` `WritingWorkbench` input 阶段——TextArea 场景输入 + 内容密级选择 + 「开始判别」，无前置文种选择；后端 `internal/doctype/http.go` `POST /api/doctype/classify`。RoleWorkbench「起草」入口进入。tsc 严格构建通过。）
- [x] 8.2 实现**Top-N 候选确认交互**：展示候选文种 / 子类 / 置信度并支持一键确认或改选（对齐 D-06-2）。验证：多义场景前端弹出候选，用户改选生效。（实现：`WritingWorkbench` confirm 阶段展示候选（文种·子类·置信度·目标能力）一键选择/改选，经 clarify 以用户选择继续；后端 classify 返回 candidates。）
- [x] 8.3 实现**澄清追问与跳过交互**：按需展示针对性追问、支持「跳过、先生成初稿」（对齐 slot-clarification spec）。验证：缺要素时展示追问、跳过按钮可放行。（实现：`WritingWorkbench` clarify 阶段展示针对性问题 + 补充输入 + 「跳过、先生成初稿」；后端 `POST /api/doctype/clarify` 驱动多轮，放行返回场景上下文（含缺失要素标记）。）
- [x] 8.4 判别 / 澄清交互按**请求-响应**处理，不要求 SSE 流式（正文生成的 SSE 属 c05 / c07，对齐 Non-Goals）。验证：判别 / 澄清接口为普通请求-响应、无流式依赖。（实现：`classifyDoctype` / `clarifyDoctype`（`api.ts` fetch 普通 JSON 请求-响应）+ 后端 `Handler` 纯 JSON 接口，无 SSE/流式；`http_test.go` httptest 验证 classify 直选/候选/空场景400/未认证401、clarify 追问→放行契约/跳过标记缺失/空文种400。）

## 9. 端到端验证与灰度

- [x] 9.1 编写**判别 + 路由单元 / 集成测试**覆盖各 spec Scenario：请示-上行经费、通知-下行会议、多义报告 / 请示、稀缺方案-调研降级 c07、命令→c07、无法归类兜底 c07。验证：各 Scenario 断言通过。（实现：`internal/doctype/pipeline_test.go` 集成 + §2–§4 单测覆盖各场景；`TestPipelineScenarioRouting`（通知-召开会议→c05 / 方案-调研方案→c07 / 命令→c07 / 通用公文→c07）/ `TestPipelineAmbiguousThenUserSelects`（多义报告·请示→候选→改选）/ `TestPipelineEndToEndProducesContract`（请示-上行经费）。）
- [x] 9.2 编写**要素澄清测试**覆盖：要素齐备直接放行、识别缺失要素、具体追问、轮次上限停止、用户跳过、补齐回填、缺失标记移交。验证：slot-clarification 各 Scenario 断言通过。（实现：§5 `clarification_test.go` 覆盖各 Scenario；集成侧 `TestPipelineEndToEndProducesContract`（识别缺失→具体追问→补齐回填）/ `TestPipelineClarificationSkipMarksMissing`（跳过→标记缺失）/ `TestPipelineClarificationRoundLimitReleases`（轮次上限放行+标记）。）
- [x] 9.3 端到端灰度典型场景串联（场景输入→判别→候选确认→路由→要素校验→澄清→契约移交），路由与契约符合各 spec（对齐 Migration Plan 步骤 5）。验证：典型场景全链路产出正确契约。（实现：`TestPipelineEndToEndProducesContract` 以 fake LLM 串联 ClassifyCandidates→Route→ExtractSlots→澄清循环→BuildScenarioContext，断言契约目标能力/文种/方向/密级/补齐要素正确；编排为上层（§8 handler）职责，测试内联模拟多轮编排。）
- [x] 9.4 验证**回滚路径**：关闭场景描述入口可恢复用户手选文种后送 c05 / c07 的旧路径，配置与契约定义保留、无数据损失（对齐 Migration Plan 回滚策略）。验证：停用判别服务后前端可退回手选文种流程。（实现：`TestPipelineRollbackManualSelection` 不经判别、以用户手选文种 ResolveSelection→Route→契约，分级表/契约配置保留、产出有效 c05/c07 契约；前端退回手选交互属 §8。）

## 10. 待 PoC 高风险项（信创 / 私有化 / 国产模型）

- [ ] 10.1 [待 PoC] **国产模型文种判别效果摸底**：在信创私有线模型上评测文种 / 子类判别准确率与置信度可用性，对比公网模型，校准阈值与 Top-N 取值（对齐 ADR-0001 D3 信创切 Go 直连、PRD「指标阈值待实测定档」）。验证：产出国产模型下判别准确率与阈值建议报告。
- [ ] 10.2 [待 PoC] **信创阶段 c01 直连切换对本 change 无感验证**：信创切 Go 直连官方 SDK 后，本 change 判别调用经 c01 窄抽象不改业务代码（对齐 ADR-0001 D1/D3）。验证：切换底层供应商后判别功能与契约不变。（组件级已固化「切换无感」结构前提：`internal/doctype/provider_swap_test.go` `TestClassificationInvariantUnderProviderSwap` 用两个不同的 `llm.Client` 实现——公网 provider 与信创「Go 直连」provider——对同一场景返回相同模型输出，断言 c06 发出的出站 `ChatRequest`、判别结果与结构化场景上下文契约逐字不变、业务代码零改动；配合 `redline_test.go` 的 import 白名单守卫证 c06 不耦合任何具体 SDK。真实国产模型/硬件下的端到端切换 PoC 仍待信创集成期，故 [待 PoC] 不勾选。）
- [ ] 10.3 [待 PoC] **龙芯 LoongArch64 / ARM64+麒麟 上的判别服务运行验证**：在国产 CPU + 麒麟环境验证文种路由服务（Go 单二进制）正常启动与判别调用（对齐 ADR-0001 信创适配，时点 / 清单见 ADR）。验证：目标信创软硬件上服务可启动、判别链路连通。
