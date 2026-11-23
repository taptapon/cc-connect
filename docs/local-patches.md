# 本地补丁集（分支 `local/main-patched`）
>
> 本文件只记录**这组本地改动本身**：改了什么、为什么改、动到哪些文件、回归测试在哪。
> **不含部署侧信息**（构建产物装到哪、如何安装/升级/回滚）——那属于运行方的运维文档，不在本仓。
>
> English: this document lists the local patch set carried by the `local/main-patched`
> branch on top of upstream `main` (`ba299781`, 2026-11-23). Install / upgrade / rollback
> steps are intentionally out of scope.

## 分支

| 分支 | 基线 | 用途 |
|---|---|---|
| `local/main-patched` | upstream `main` `ba299781`（2026-11-23） | 实际运行版；下列补丁全部在此分支 |
| `local/v1.5.1-beta.3-patched` | upstream `v1.5.1-beta.3` | 旧运行版，保留为整体回滚点（tag `backup/pre-upstream-upgrade-20261022` = `f39246e9`） |
| `feat/model-picker-aggregation` | `main` | 备用上游 PR（模型选择器聚合） |

remote：`origin` = `git@github.com:taptapon/cc-connect.git`（fork）。

> ⚠️ **commit 号会随 rebase 重写**：下表登记的 hash 是各次 rebase 当时的快照。2026-11-23
> rebase 到 `ba299781` 后全部重写，当前有效对照表见文末「上游跟进记录（2026-11-23）」节；
> 找代码请按"改了什么"描述 grep，不要按老 hash。

## 背景：这组改动解决什么

上游 `v1.5.1-beta.3` 在下面几类场景下不可用，故在本分支修复：

1. **飞书 @ 通知只在纯文本消息里触发**——卡片/富文本中的 at 标签只渲染、不触发提及提醒；而全局 `[display] mode = "quiet"` 与默认 `reply_footer` 都会让回复走卡片路径。
2. **quiet 模式的流式预览卡片（`UpdateMessage` 路径）不跑 mention 解析**——卡片里的 at 也不会变成真 mention、不触发提醒。
3. **bot @ 人类用户从未生效**——两个 bug 叠加：SDK 群成员迭代器在单页群里重复首页，去重时把 open_id 抹成空；text 语法 at 在纯文本消息里被转成卡片语法后削成空 at 标签。
4. 附带项：无头会话的模型选择器与桌面端不同源、timer one-shot job 死循环、footer `ctx` 百分比虚高、weixin 平台 `Send` 路径分类错误。

## 补丁清单

| # | commit | 改了什么 | 主要文件 | 回归测试 |
|---|---|---|---|---|
| 1 | `f848321d` `57ebd8c0` | `/model` 命令读 `~/.claude/settings.json` 的 `modelPicker.options`（聚合模式写入）；`replaceBuiltInOptions=true` 时优先于注入的 provider models——**无头会话与桌面端模型选择器同源** | `agent/claudecode/claudecode.go` | `claudecode_model_test.go` |
| 2 | `bcf7c566` | 飞书 at 按目标类型选路：**@bot 整条降级 `MsgTypeText`** 保 mention 送达，@human 走卡片。**仅对配了 `peer_bots` 的项目生效**（没配则退回"任何 at 都降级"的老行为） | `platform/feishu/feishu.go` | `feishu_test.go` |
| 3 | `74da08f3` | quiet 模式的流式卡片走 `UpdateMessage`，**绕过** mention 解析与 @bot 降级——最终回复里提及另一个 bot 时删掉预览卡片、回落普通 `Send`（新增 `core.FinalTextDetacher` 接口 + `feishu.ShouldDetachForFinalText`）。**不装它，quiet 模式下回复里 @ 别的 bot 对方收不到**（症状：群客户端看到卡片、"@"是纯文字不带蓝） | `core/streaming.go`、`platform/feishu/feishu.go` | `feishu_test.go` |
| 4 | `79df7c2d` | timer 的 one-shot job 改为**执行前**就同步落盘 `fired`（`MarkFired` 前移），执行错误改由新增的 `MarkError` 单独记录。修的是死循环：exec 里重启 daemon → daemon 在 `MarkFired` 落盘前就死了 → 该 job 永远停在"未触发" → 每次重启重新触发它（每 ~10s 一条通知 + 一次重启） | `core/timer.go` | `core/cuj_test.go` |
| 5 | `3af6380a` | quiet 模式下每个 thinking/工具边界都往正文插 `\n\n`，连续多个边界无文本间隔时会累积成 4~12 个连续空行——加 `streamPreview.endsWithParagraphBreak()` 守卫。**只在纯文本回复里看得见**（卡片 markdown 会折叠多余空行） | `core/engine.go`、`core/streaming.go` | `core/streaming_test.go` |
| 6 | `2cc97715` | footer 的 `ctx N%` 虚高：网关会把 API 响应里的 model 字段改写掉 `[1m]` 后缀，usage 走 transcript 回读时按 200k 窗口算，ctx 从真实 ~18% 虚标到 91%——新增 `alignContextWindow`，transcript 回读的两处优先用 init 事件的会话模型名重推窗口。token 数值不动 | `agent/claudecode/session.go` | `session_test.go` |
| 7 | `3cd7f168` `35a24ba5` | bot @ 人类用户修复（**两个 bug 叠加**导致 @人 从未生效）：① lark SDK `GetChatMembers` 迭代器在单页群无限重复第一页，`fetchChatMembers` 的同名去重把重复成员当重名、将 open_id 抹成 `""` → 群名片解析层拿到全空 map，静默失效还缓存 1 小时；② `buildReplyContent` 对 @human 纯文本消息也把 text 语法 at 转成卡片语法再按 `MsgTypeText` 发送，飞书削成空 `<at user_id=""></at>` | `platform/feishu/feishu.go` | `TestAddChatMember`、`TestBuildReplyContent_HumanMention*` |
| 7b | `d20b442e` | `ShouldDetachForFinalText` 从「含 @bot」放宽到「含**任意**解析后 at」——quiet 模式下最终回复里 **@人类** 原本留在卡片里（不解析、不触发提醒）静默丢，现在也回落文本、真发蓝 @。代价：带 @ 的最终回复失去卡片 markdown | `platform/feishu/feishu.go` | `TestShouldDetachForFinalText`（人类 mention 用例已翻转） |
| W | `4a27a987` | **weixin 平台**：interactive `Send` 改走 reply 路径 + 新增 budget notice。⚠️ 该 commit 自称 "patch 7"，与上表补丁 7 是**两套独立编号**（weixin 侧自编号，故此处记作 `W` 以免混淆）。**2026-11-23 部分退役**：时间窗重分类（`classifySendPath`/`recordInbound`）删除，改用上游 #1937 的 `replyContext.inbound` 标记（media 发送同改）；**budget notice 与 burst_limit=6 保留**（上游无对应实现） | `platform/weixin/weixin.go`、`platform/weixin/media_outbound.go` | `weixin_test.go` |
| 8 | `18402a8e` | **mute timer 吞掉显式外发**：`--mute` 的 timer/cron job 把会话 platform 包成 `mutePlatform`（丢弃一切 Send/Reply 且返回 nil），而 `resolveOutboundSessionTarget` / `resolveSendTarget` 把这个包装器直接交给外部 API 调用——mute 回合内 `cc-connect send -s` **假报成功、消息静默丢弃**（2026-10-14 实锤：重启回执、定时天气、接续确认全灭，且误诊过"卡片渲染"）。两处 resolver 现在解包回真实 platform；job 自身的 ⏰通知/结果回执仍被 mute 静音，语义不变 | `core/engine.go`、`core/cron.go` | `TestUnwrapMutePlatform` |
| 9 | `41cd87fa` | **私聊豁免弃卡降级**：`ShouldDetachForFinalText` 原先对"最终文本含可解析 @"一律删预览卡回落纯文本——私聊里这是纯损失（收件人只有用户本人，消息必达，at 无 mention 事件可触发，降级只丢 markdown 渲染；2026-10-14 mac-c 实测：DM 回复顺嘴写 `@h-cc` 命中 mention_map，整条表格变乱码）。现在 Platform 维护 chat_id→chat_type 缓存（消息入口记录），**p2p 会话直接保卡**；群聊与缓存未命中（如 timer/cron 主动外发）保持原判定 fail closed | `platform/feishu/feishu.go` | `TestShouldDetachP2PExemption` |
| 10 | `18c9fd5f` | **空闲重置提示匿名**：`reset_on_idle_mins` 触发的自动重置两条提示（⏳ 收尾中/⏰ 已重置）既不说重置的是**哪个项目**的会话、也不说**刚到的消息去哪了**——2026-10-15 实测（w-cc 接入裁定时撞上 180 分钟重置），用户"看不出来重置的谁和谁"。两条提示现带项目名（`e.name`），⏰ 补"你刚才的消息将由新会话继续处理（引用的回复上下文会一并转交）"。注意：重置发生在消息处理**之前**，触发消息本身从不丢；跨会话上下文靠飞书引用回复链注入（`fetchReplyChain`），裸发不带引用的回复仍会丢上下文 | `core/i18n.go`、`core/engine.go` | `TestIdleResetNoticesCarryProjectAndFate`、`TestHandleMessage_AutoResetOnIdle_*`（断言已随新文案更新） |
| 11 | `dad43437` | **空消息守门根治**：content / ExtraContent（含纯空白引文，现在会 trim）/ images / files / location **全空**的消息永不 spawn turn。人类发送者回固定 i18n 提示（带项目名）；**bot 发送者（新增 `core.Message.FromBot`，feishu 按 `sender_type == "app"` 判定）保持静默**——否则共享群里两个 bot 会互相回提示语成 ping-pong 环。残留 `ExtraContent` 不 trim 的洞一并堵上 | `core/engine.go`、`core/message.go`、`core/i18n.go`、`platform/feishu/feishu.go` | `core/engine_empty_guard_test.go`（human 回提示 / bot 静默 / 空白引文 / 非空消息对照） |
| 12 | `7fb35b60` | **stderr 降噪两层**：① `[claude-code:unrecognized_model]` 提示行（网关自定义模型 id，如 CC Switch 聚合）降 DEBUG，且不再充当错误文案——原先 stderr 只有该提示时，崩溃会顶着提示文案报错（或干脆静默）；② `Close()` 开头置 `killing` 标志，其后观察到的非零退出（SIGKILL / 0xc000013a / taskkill /F exit 1——**按标志分类，不匹配退出码**）记 INFO `session terminated by stop/new/shutdown`、不发错误事件。附带修掉：err≠nil 且 stderr 全空时此前**完全无日志无事件**的静默洞 | `agent/claudecode/session.go` | `session_noise_test.go`（split 表驱动 + killed 降级 + notice-only 崩溃 + 混合 stderr + 裸崩溃） |
| 13 | `dad43437`（引擎侧与补丁 11 同 commit） | **send/turn 指标拆分 + 实收正文审计**：废弃单一 `slow agent send` elapsed（阈值 2s，实测≈turn 全长，几乎每 turn 误报）。每 turn 记一条 INFO `agent turn metrics`：`send_latency`（send → 首个 agent 事件，WARN ≥5s）/ `turn_duration`（全 turn，WARN ≥10min）；事件级打点在 `interactiveState.firstEventAt`。另将 daemon **实收正文**（引文合并后、门控前）审计进主日志（`message text audit`，`previewText` 截 2000）——空消息排查反复要"当时到底收到了什么"，这是刻意凌驾于"正文只进 DEBUG"规则的例外 | `core/engine.go` | `core/engine_empty_guard_test.go`（对照路径）；指标断言依赖运行日志核验（无独立单测） |
| 14 | `65ced2d9` | **/new 杀树 win 分支**：`taskkill` 不带 `/F` 只"请求"GUI 程序关闭，控制台进程（claude.exe 及其 node 子进程）完全无视——原先 graceful(5s) + 温和信号(5s) 两段是纯死等，每次 /new ≈13s。win 现跳过两段直通 `taskkill /T /F` 重试（实测 13.2s→1-3s）。**unix 侧 SIGTERM 阶梯原样不动**（`fastKillTree()` 平台开关，unix 恒 false，有单测钉住）。代价：跳过 Stop 钩子与在途回复——/new 语义本就如此，win 侧裁定接受 | `agent/claudecode/proc_windows.go`、`proc_unix.go`、`session.go` | `TestFastKillTree_OffOnUnix`（unix 关死）；win 路径靠交叉编译 + 上机实测 |
| 15 | `47ab290e` | **unsolicited 补发路径静默过滤**：主回复交付后回合尾部冒出的输出（如后台任务通知落在收尾瞬间的小回应）由 unsolicited reader 补发到聊天，但该路径没有套 NO_REPLY 静默过滤——2026-10-16 mac-h 实测：h-quant-stock 一轮 15m45s turn 尾部的裸 NO_REPLY（8 字符）被字面量发进聊天（自 2026-09-12 起 143 次补发中首例，需"通知恰落回合结束 + 模型回 NO_REPLY"两条件凑齐）。现对齐前台语义：裸标记→整段不发（INFO `silent unsolicited reply suppressed`）；混合内容尾部标记→剥掉发剩余；session history 照记原文。附带：修 `tests/integration/engine_platform_test.go` 一处漏跟 Send 加 messageID 参数的陈旧调用（该包编译曾断） | `core/engine.go` | `TestUnsolicitedReader_SilentReplySuppressed`（裸吞 / 剥尾 / 纯内容照发 / history 留档） |
| 16 | `7cc5fe6b` | **群聊 bot 发送者一律要求真 @**（台账 #6 落地）：`group_reply_all` 是人类便利开关，不应对 app 发送者生效——否则对端 bot 每条未 @ 的群消息（收尾、知会、闲聊）都白开一个全量 turn（2026-10-15 三机群实测单次 ~5.5 万 token 冷启动）。群 mention 过滤器条件加 `fromBot` 分支：bot 消息无真 @ 即丢，**只落 INFO 日志不回复**（回 bot 正是要防的 ping-pong 风源）；协议内的 @ 消息照常 spawn、p2p 不受影响、人类消息不受影响、relay 不受影响（daemon 内部路径非平台入站）。未开 group_reply_all 的项目（mac-h 默认）行为不变。⚠️ win/c 侧开了 group_reply_all 的项目**换装后才生效** | `platform/feishu/feishu.go` | `TestOnMessageBotSenderGroupGate`（bot 无 @ 丢 / bot 带 @ 过且 FromBot=true / 人类无 @ 照过） |
| 17 | `8f8c8c62` | **空闲重置提示两条合一 + 文案精简**：`maybeAutoResetSessionOnIdle` 原本最多发两条——⏳「正在结束上一会话」（仅旧会话有存活 agent 进程时，发在慢关闭前）+ ⏰「已切换新会话」（无条件，发在关闭后），旧进程未退时用户连收两条（2026-10-17 mac-h 实测）。现**每次重置只发一条**：有活进程 → 发合并版 `MsgSessionAutoResetIdleClosing`（阈值 + 收尾警告 + 消息去向，`TryLock` 成功后、慢关闭前一次发出，关闭后不再补发）；无活进程 → 维持单条 ⏰（精简版 `MsgSessionAutoResetIdle`）。两文案五语言同步精简（去"通常几秒""你刚才的"等冗词，保留项目名/阈值/消息去向/引用上下文转交/2 分钟上限）。顺带把 `TryLock` 挪到任何回复之前——锁失败静默返回，不再有"已预告切换却没切换"的空炮 | `core/engine.go`、`core/i18n.go` | `TestIdleResetNoticesCarryProjectAndFate`（两文案五语言：格式动词/项目名/阈值/消息去向/上限全查）、`TestHandleMessage_AutoResetOnIdle_*`（无活进程路径行为不变） |
| 18 | `40a4b7f8` `8be01a81` | **空提及意图判断**：人类「仅提及无文字」（群内真 @ 本 bot / 私聊空文本——飞书剥提及后正文为空）在平台层被判空静默丢弃，呼叫信号被吞、对话无声中断（2026-10-17 蒋宏实测：纯提及消息零响应零日志）。放行为「空提及回合」：平台 dispatch 带标（新增 `core.Message.EmptyMentionTurn`，dispatchMessage 加 `chatType` 参），空消息守门对其合成 i18n 意图占位 prompt（`MsgEmptyMentionPrompt`，五语言：延续话题则接话 / 呼叫则简短应答 / 疑似误触一句话确认）替代固定 ℹ️ 提示，由 agent 结合对话历史自行判断。开关 `empty_mention_turn` **默认开**（`8be01a81` 蒋宏裁定；显式 `false` 回退旧静默行为）。**bot 发送者永不带标**（ping-pong 底线，补丁 11/16 不动）；群内无 @ 空消息照丢；`group_reply_all`/mention 过滤不受影响 | `platform/feishu/feishu.go`、`core/engine.go`、`core/message.go`、`core/i18n.go` | `TestDispatchMessageEmptyMentionTurn`（群 @ 人过 / 群无 @ 丢 / bot 带 @ 仍丢 / p2p 过 / 开关关保持丢）、`TestHandleMessage_EmptyMentionTurn*`（合成 prompt 开 turn / bot 组合防御性静默）、`TestNewEmptyMentionTurnDefault`（默认开 / 显式关） |
| 19 | `c216f351` | **【已退役 2026-11-23，上游 #2286 原生覆盖】** 非 Windows 的 daemon 重启被拆成 Stop→Start 导致自杀：上游 `987f454f` 为 Windows 孤儿进程把 `daemon restart` 统一改成 stop→reap→start。launchd 的 `Stop()` 是 `bootout`，会连带杀掉 daemon 进程树里的一切——包括 ops timer 脚本和它拉起的 CLI 本身，于是 `Start()` 永远跑不到，服务停在"未加载"（2026-10-22 mac-c 实锤）。上游 2026-11-17 `59653b74`/`9438ef27` 引入 `daemon.InPlaceRestarter`（launchd `kickstart`、兼容旧 label），严格覆盖本地实现，故本地补丁删除 | `cmd/cc-connect/daemon.go` | 上游 `daemon/launchd_test.go`、`cmd/cc-connect/daemon_orphan_test.go` |
| 20 | `4cad14ec` | **/model 列表显示长路由 id**：CC Switch 聚合模式写进 settings.json 的 `modelPicker.options` 里 `model` 是完整路由 id（`ccs-claude-p00b5d1--glm-5.3-flash[1M]`），而 `label` 本来就是短友好名（`glm-5.3-flash（智普）`）——无头 /model 卡片把两者拼在一起渲染（`alias - name` / `name — desc`），下拉项超长且重复（2026-11-21 mac-h 用户反馈）。现 `modelPickerOptions()` 把 `label` 填进既有的 `ModelOption.Alias` 通道，卡片下拉与非卡片文本列表改为 **alias-only 显示**（完整 id 仍可在卡片"当前模型"行看到，`/model <完整id>` 直输与序号切换不受影响）。副作用：config 配了 alias 的其他 agent（codex 等）显示从 `alias - name` 变 `alias`——alias 的设计意图即是短名替代 | `agent/claudecode/claudecode.go`、`core/engine.go` | `claudecode_model_test.go`（Alias 填充/缺 label 时为空断言）；core 模型相关用例全过 |
| 21 | （本次提交） | **回复 footer 显示长路由 id**：CCD 风格 footer（`<model> · out N · in N cw N cr N · ctx N%`）上游刻意保留原始模型 id（"for diagnostic clarity"），聚合模式下即整串 `ccs-claude-…--glm[1m]`（2026-11-21 mac-h 用户反馈，紧随补丁 20）。现 `replyFooterModel()` 优先采用 agent 可选接口 `ModelDisplayName(model)` 的解析结果：claudecode 实现为 config provider-model alias → modelPicker label 两级查找（**忽略大小写**——网关会把 `[1M]` 改写成小写 `[1m]`，精确匹配会漏），查不到返回空、footer 保持原 id（非聚合 setups 行为不变）。footer 从 `ccs-claude-p00b5d1--glm-5.3-flash[1m]` 变 `glm-5.3-flash（智普）` | `core/engine.go`、`agent/claudecode/claudecode.go` | `claudecode_model_test.go`（picker 命中含大小写折叠 / 无 label 回退 / config alias 优先）、`core/engine_test.go`（replyFooterModel 有显示名用显示名 / 无接口用原 id） |
| — | `f520c48a` | **版本戳兜底**（补丁 11 顺带项）：裸 `go build`（win 构建流程无 ldflags）使 `--version` 自报 dev/none。ldflags 缺席时改从工具链内嵌的 VCS 构建信息读 `vcs.revision`/`vcs.time` 兜底 | `cmd/cc-connect/main.go`、`cmd/cc-connect/version_info.go` | 编译期；实测看 `--version` |

## 上游跟进记录（2026-10-22，基线 `afbbc240`）

上游自 `v1.5.1-beta.3` 后未再发 tag，`main` 相对本分支旧基线领先 115 个提交。本次把 33 个本地提交
（18 个补丁 + 15 个文档提交）按原顺序 cherry-pick 到 `upstream/main` 上：**26 个干净落地，7 个冲突**，
冲突全部是"上游重构后接口搬家"，逐个移植完成：

| 补丁 | 冲突文件 | 处理 |
|---|---|---|
| 8 `18402a8e` | `core/cron.go` `core/engine.go` | 上游重写 cron 调度循环；`unwrapMutePlatform` 原样保留，两处 resolver 调用点照旧 |
| 9 `41cd87fa` | `platform/feishu/feishu.go` | 上游新增 `groupChatHistoryShare` 字段；`chatTypes` 缓存与之并列保留，入口记录点不变 |
| 11+13 `dad43437` | `core/*` `feishu.go` | 上游把 `dispatchMessage` 拆成 wrapper + `dispatchMessageWithHistory`（多 `groupHistoryCtx` 参），`fromBot` 穿透到两层 |
| 14 `65ced2d9` | `proc_windows.go` `proc_unix.go` | 上游新增 `internal/taskkill` 包并删除 `isTaskkillNotRunning`；本地只保留 `fastKillTree`，判定改用上游 `taskkill.IsNotRunning` |
| 16 `7cc5fe6b` | `platform/feishu/feishu.go` | 上游群过滤器新增活跃话题/降级分支；`fromBot` 条件重挂在 `(!groupReplyAll \|\| fromBot)` 上 |
| 18 `40a4b7f3` `8be01a81` | `core/*` `feishu.go` | 空提及标记得以穿透新的 `dispatchMessageWithHistory` 签名；`chatType` 取自上游新增的 `msg.ChatType` 解析；空文本判据补上上游的 `historyText` |

### 补丁重评估结论

**18 个补丁全部保留**——逐条 grep 上游确认：`modelPicker`、`endsWithParagraphBreak`、`FromBot`、
`unrecognized_model`、`turn metrics`、`fastKillTree`、NO_REPLY 静默、`IdleClosing`、`EmptyMentionTurn`
在上游均无对应实现；`peer_bots` 上游只有配置字段，没有本分支的 at 选路逻辑。两处做合并/降级：

装机首轮（2026-10-22 14:02）又暴露一个上游回归：`987f454f` 把 `daemon restart` 改成 stop→start，
在 launchd 上等于 bootout 自杀，导致升级后服务未加载、需要手工 `launchctl bootstrap`。已补**补丁 19**
（非 Windows 走原子 Restart），重试升级一次通过。

| 补丁 | 结论 | 原因 |
|---|---|---|
| 6 `2cc97715` | **保留为兜底**（适配提交 `cf3ad5ce`） | 上游 `c3a6a25e` 新增 `context_window_tokens` 覆盖、`93b3a5da` 改用 result 事件的 `modelUsage.contextWindow`，已覆盖主场景。本补丁改为走上游 `cs.contextWindow()`：覆盖值/上报值优先，网关改写模型名时用 init 事件模型重推窗口兜底。**装机后需实测 footer**：若网关上报的窗口本身是 200k，用 `context_window_tokens = 1000000` 钉死 |
| 14 `65ced2d9` | **保留，砍掉一半** | 上游 `internal/taskkill` 已覆盖"目标不存在"的多语言判定；本地只保留 `fastKillTree()`（win 直通 `/T /F`） |
| W `4a27a987` | 保留（可再议） | weixin 平台补丁，三机均未启用该平台；无冲突、无成本，暂留。若确定永不使用可单独回滚 |

### 顺带获得的上游修复（本次升级的主要收益）

- cron 调度器改为最多睡 30s、唤醒重算：**合盖睡眠后到点不触发**的问题修掉
- busy 期间排队的消息也计入 `reset_on_idle_mins`，避免对话中途被轮转
- `group_reply_all` 下旁听到的未授权群成员消息静默丢弃，不再逐条回"无权限"
- `--resume` 时空 task-notification result 不再被当成回合结束（"(empty response)"错位）
- unsolicited reader 取消/退出时补发已累积文本，并保证顺序在下一回合回复之前
- `AskUserQuestion` 卡片点击可靠性（扁平 action 行 + 过期点击丢弃）、卡片 `/switch` 不清历史

**有意不采纳**：飞书 `group_chat_history_share`（需额外 `im:message.group_msg` 权限、增 token）、
回执表情 `receipt_ack`、multi-workspace / web 面板，以及 slack/dingtalk/qq/wecom/codex/kimi 等
未使用平台的改动。

### 测试状态

`go test -tags 'no_web goolm' ./...`：4535 通过 / 12 失败 / 26 跳过。12 个失败均与本地补丁无关，
已在纯净 `afbbc240` worktree 对照复现：9 个 core workspace/skills 用例因 macOS `/private/var` 路径
规范化失败、codex skills 用例依赖本机 app-server、opencode/antigravity 两条为重跑即过的偶发。
本地补丁相关的 38 条回归用例（mention 选路/弃卡、空消息守门、空提及、idle 提示、NO_REPLY、mute 解包、
win fast kill、ctx window）全部通过。

## 上游跟进记录（2026-11-23，基线 `ba299781`）

上游自上次基线 `afbbc240`（2026-10-22）后再领先 **408 个提交**（247 文件，+37K 行）。本次把 42 个本地提交
（含文档/测试提交）rebase 到 `ba299781`，冲突集中在 6 处，逐个处理：

| 补丁 | 冲突文件 | 处理 |
|---|---|---|
| 1/2 `/model` 聚合 | `agent/claudecode/claudecode*.go` | 上游新增 MiniMax 模型优先分支。优先级定为 **聚合 picker(replace) → MiniMax → 已配置 → API → 内置回退**；非覆盖式 picker 仍追加在末尾 |
| 6 ctx 窗口 | `agent/claudecode/session.go` | 上游 `claudeContextWindow(model, override)` 改双参、新增 `handleRefusalFallback`；`alignContextWindow` 跳板到上游 `cs.contextWindow(m)`（保留 init 模型重推语义） |
| 11 空消息守门 | `core/message.go`、`platform/feishu/feishu.go` | 上游新增 `CardAnswerRendered` 字段与 `staged` 附件参数；`FromBot` 与之并存，三处 `core.Message` 构造取上游字段 + `FromBot` |
| 16 bot 提及门 | `platform/feishu/feishu.go` | 上游把全局 `groupReplyAll` 改为按会话 `groupReplyAllForChat`；条件重挂 `(!groupReplyAllForChat \|\| fromBot)` |
| 18 空提及 | `platform/feishu/feishu.go`、`*_test.go` | `dispatchMessage`/`dispatchMessageWithHistory` 签名同时容纳上游 `staged []stagedAttachment` 与本地 `fromBot`+`chatType`；上游引文/帖子解析重构（`quotedExtra`、`postFiles`→`files`）按上游变量合流；上游测试调用统一补 `false, ""` |
| 21 footer 显示名 | `core/engine_test.go` | 纯相邻插入冲突，双方测试全保留 |

### 本次退役 / 降级

| 补丁 | 结论 | 原因 |
|---|---|---|
| 19 daemon 重启 | **退役** | 上游 #2286 引入 `daemon.InPlaceRestarter`（launchd kickstart + 旧 label 兼容），严格覆盖本地实现 |
| W weixin 时间窗重分类 | **部分退役** | 上游 #1937 `replyContext.inbound` 根治同一问题；`media_outbound` 同改用 `inbound`。budget notice 与 `burst_limit=6` 保留（上游无对应） |
| 其余补丁 | 全部保留 | 逐条核对上游无对应实现（modelPicker、FromBot、EmptyMentionTurn、ShouldDetachForFinalText、mute 解包、fastKillTree、NO_REPLY 静默、idle 提示、ctx 窗口兜底等） |

### 顺带获得的上游修复（与 mac-h 部署直接相关）

- **飞书**：静默 WebSocket 恢复（#2120/#2288）、AskUserQuestion/权限卡点击权限门与双答抑制、进度卡更新失败自恢复、Markdown 表格按内容定列宽与宽度填满、引用/转发展开与链接保真、视频/媒体下载、`attachment_wait` 附件暂存
- **claudecode**：优雅取消 Ctrl 中断（#1765）、>256KB transcript 行、/usage 探针挂死、/model 回退到 Claude 5 家族、footer 显示 applied effort
- **core/cron/timer**：timer `--exec` 走配置 shell（无头 PATH 坑）、停机不等运行中 job、长回复分段保留闭合代码围栏、UTF-8 截断不切烂、回合出错后后台事件不丢
- **relay**：群回显保全响应、超时不再漏对方工具输出（跨机 bot 对话受益）
- **daemon**：`InPlaceRestarter` 原地重启
- 新平台 max/wps-xiezuo/wps-agentspace、新 agent pi/antigravity、约 10 个供应商预设、俄语 i18n、黑盒测试基建：本部署不用，未评估

### 测试状态

`TMPDIR=/private/tmp/cc-go-test go test ./...` **全绿**。⚠️ 用默认 `TMPDIR`（`/var/folders/...`）直接跑会
在 core workspace/skills 等用例上报 `/var` vs `/private/var` 假失败——**上游原版在纯净 worktree 同样失败**，
属 macOS 符号链环境问题而非补丁引入。跑测试前 `mkdir -p /private/tmp/cc-go-test` 并设置 TMPDIR 即可。
另修一处本分支测试前置污染：上游 `TestAvailableModels_*`/`TestMiniMaxAvailableModels*` 未隔离
`CLAUDE_CONFIG_DIR`，读到本机 CC Switch 聚合 `modelPicker` 就短路——已补 `t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())`。

## 构建

```bash
make build-noweb          # 需要 go；国内网络可加 GOPROXY=goproxy.cn
# 版本戳：上游 main 的 Makefile 仍写死 VERSION := v1.5.1-beta.1（发行线分叉后未回灌），
# 直接用会在 --version 里报出比旧二进制更老的版本。构建时显式覆盖：
make build-noweb VERSION=v1.5.1-beta.3+main.ba299781
```

（装到哪个路径、如何升级/回滚、备份约定：属部署侧，见运行方自己的运维文档。）
