# 03: Support automatic synchronization and recoverable desktop switching

**What to build:** 配置完成后两台电脑通过现有自动、启动和退出同步持续交换数据；断网、冲突或应用重开后能保留并恢复用户工作。

**Blocked by:** 01 — Support manual S3 synchronization without an official account.

**Status:** resolved

**Triage:** ready-for-agent — 用户已批准扩大到同步引擎修复及旧数据兼容验证

**Started:** 2026-10-28，工作分支 `v2`；验证已有自动、启动、退出同步及恢复语义

- [x] 不依赖官方账号的 S3 路径覆盖自动、启动、退出及手动重试内核入口；真实桌面生命周期待验收。
- [x] 协议测试中新增、更新、重命名和删除收敛，附件可读取；真实 R2 待验收。
- [x] 双设备离线编辑、恢复网络、冲突文档启用和重复同步通过独立进程及工作空间回归。
- [x] 显示可观察的同步中、成功与失败状态；不误报为实时推送或协同编辑。
- [x] 使用已有调度和重试策略；实际平台相关的启动退出行为另做桌面验收。

## Comments

2026-10-28：自动同步复用既有调度，无需新增轮询或重试协议。修复未登录 S3 用户自动同步间隔被会员判断隐藏的问题；官方云同步感知的显示条件保持。`kernel/model/sync_s3_desktop_test.go` 使用两个隔离工作空间和私有 S3 协议测试服务，覆盖启动、定时及退出同步内核入口、附件、新增、重命名、删除、服务暂不可用、配置重新加载与手动恢复、重复同步。`app/src/dialog/processSync.test.ts` 验证同步中、失败、恢复成功的图标和事件。

初始复现（修复前）：两个客户端在同一秒修改同一路径的 `.sy` 文档，即使开启生成冲突文档，也可能只留下一个版本。依赖 `github.com/siyuan-note/dejavu v0.0.0-20261011111741-4465ef631977` 的 `entity.NewFile` 使用路径与秒级修改时间生成文件 ID，忽略内容，因此不同内容可得到相同 ID。

修复前测试结果为 `independent desktops lost an offline edit: first=true second=false`。用户随后明确批准继续修复底层引擎并完成旧数据兼容验证。现回归用例名为 `TestS3DesktopSameSecondConcurrentEdits`，不再使用环境变量或跳过：从 `kernel/` 执行 `go test -tags "fts5 sqlcipher" ./model -run '^TestS3DesktopSameSecondConcurrentEdits$' -count=1`。Windows 需要可用的 Go 和 C 工具链。同时间戳、等长内容的双进程编辑现均保留，重复同步后仅有原文和一份冲突文档。

修复仅对新写入的 S3 文件对象启用包含内容的 v2 标识，旧对象、加密、压缩、分块和仓库密钥保持兼容。引擎必要根包源码固定纳入 `kernel/internal/dejavu` 并记录原始哈希与许可证，避免依赖本机补丁；其他提供商保持原标识方式。缓存按仓库和密钥隔离，错误密钥不能从另一上下文取得已解密文件元数据。

`kernel/internal/dejavu/compatibility_test.go` 覆盖固定旧仓库夹具、新旧引擎分别恢复旧/新快照、旧/新对象经云存储备份后下载恢复、错误密钥及损坏密文保留。真实 R2、实体电脑与操作系统退出流程仍待验收，步骤见 [desktop-acceptance.md](../desktop-acceptance.md)。

## Answer

自动、启动、退出及手动恢复复用原入口，设置和状态回归通过。引擎补丁修复同秒内容版本混淆，并保留旧对象读取和旧快照恢复。独立进程同时间戳、等长离线编辑通过，反复同步后保持两个版本且仅有一份冲突副本。审查进一步发现同伴来源只接受旧标识，已通过先失败再修复的回归补齐：认证解密后严格重算 v1 / v2 标识，云端对象不可读时旧、新快照均可从同伴恢复。

最终审查与验证记录见 [review-03-engine.md](../review-03-engine.md)。代码实现及本地协议回归完成；未构建、重启、打包或发布客户端，未配置真实 R2。两台实体电脑的操作系统生命周期与 R2 真实权限仍按验收清单另行执行，不能据本记录视为云端部署已完成。
