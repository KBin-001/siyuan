# 03: Support automatic synchronization and recoverable desktop switching

**What to build:** 配置完成后两台电脑通过现有自动、启动和退出同步持续交换数据；断网、冲突或应用重开后能保留并恢复用户工作。

**Blocked by:** 01 — Support manual S3 synchronization without an official account.

**Status:** claimed

**Triage:** needs-info — 同秒并发编辑缺陷已复现，等待确认是否扩大到同步引擎依赖修复及兼容验证

**Started:** 2026-10-28，工作分支 `v2`；验证已有自动、启动、退出同步及恢复语义

- [x] 不依赖官方账号的 S3 路径覆盖自动、启动、退出及手动重试内核入口；真实桌面生命周期待验收。
- [x] 协议测试中新增、重命名和删除收敛，附件可读取；文档并发更新验收未通过。
- [ ] 双设备离线编辑、恢复网络、冲突文档启用和重复同步不会静默丢失变更。
- [x] 显示可观察的同步中、成功与失败状态；不误报为实时推送或协同编辑。
- [x] 使用已有调度和重试策略；实际平台相关的启动退出行为另做桌面验收。

## Comments

2026-10-28：自动同步复用既有调度，无需新增轮询或重试协议。修复未登录 S3 用户自动同步间隔被会员判断隐藏的问题；官方云同步感知的显示条件保持。`kernel/model/sync_s3_desktop_test.go` 使用两个隔离工作空间和私有 S3 协议测试服务，覆盖启动、定时及退出同步内核入口、附件、新增、重命名、删除、服务暂不可用、配置重新加载与手动恢复、重复同步。`app/src/dialog/processSync.test.ts` 验证同步中、失败、恢复成功的图标和事件。

独立进程复现发现：两个客户端在同一秒修改同一路径的 `.sy` 文档，即使开启生成冲突文档，也可能只留下一个版本。当前依赖 `github.com/siyuan-note/dejavu v0.0.0-20261011111741-4465ef631977` 的 `entity.NewFile` 使用路径与秒级修改时间生成文件 ID，忽略内容，因此不同内容可得到相同 ID。此问题位于既有同步引擎，尚未修复，不能关闭 03。

可选择运行已知失败的独立进程回归用例：在 `kernel/` 设置 `SIYUAN_REPRO_S3_SAME_SECOND=1`，执行 `go test -tags "fts5 sqlcipher" ./model -run '^TestS3DesktopSameSecondConflictReproducer$' -count=1`。Windows 需要可用的 Go 和 C 工具链。本次复现结果为 `independent desktops lost an offline edit: first=true second=false`。默认全套跳过此已知失败用例并显示跳过原因；这不代表冲突已通过，单独保留失败用例供后续引擎修复使用。

原规格要求复用同步协议且不改变远端加密格式。引擎文件身份算法及依赖交付方案的修改涉及兼容取舍，已向用户提出具体范围选择；未修改依赖版本、模块缓存、加密格式或仓库密钥。真实 R2、实体电脑与操作系统退出流程仍待验收，步骤见 [desktop-acceptance.md](../desktop-acceptance.md)。
