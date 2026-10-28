# 03: Support automatic synchronization and recoverable desktop switching

**What to build:** 配置完成后两台电脑通过现有自动、启动和退出同步持续交换数据；断网、冲突或应用重开后能保留并恢复用户工作。

**Blocked by:** 01 — Support manual S3 synchronization without an official account.

**Status:** ready-for-agent

- [ ] 不依赖官方账号的 S3 路径覆盖自动、启动、退出及手动重试入口。
- [ ] 新增、更新、重命名和删除按现有同步语义收敛，附件可读取。
- [ ] 双设备离线编辑、恢复网络、冲突文档启用和重复同步不会静默丢失变更。
- [ ] 显示可观察的同步中、成功与失败状态；不误报为实时推送或协同编辑。
- [ ] 使用已有调度和重试策略；实际平台相关的启动退出行为另做桌面验收。
