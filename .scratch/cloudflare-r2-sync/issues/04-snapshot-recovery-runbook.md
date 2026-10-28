# 04: Support cloud snapshot recovery and a verified personal-use runbook

**What to build:** 用户能在自用 R2 同步路径备份和恢复云端快照，并依据完整操作说明接入电脑、监控免费额度、处理凭证失效和恢复历史。

**Blocked by:** 02 — Support R2 onboarding on a second desktop; 03 — Support automatic synchronization and recoverable desktop switching.

**Status:** ready-for-agent

- [ ] 云端快照的列表、上传、下载和恢复不因 S3 自用路径缺官方会员而被拦截。
- [ ] 可在测试工作空间还原此前笔记和附件；失败时保留恢复材料与源数据。
- [ ] 加密笔记本及受支持既有数据仓库格式保持可读，无需重建密钥。
- [ ] 操作说明覆盖私有桶、限定凭证、首次和第二台电脑配置、密钥保存、凭证轮换及独立备份。
- [ ] 成本说明写明免费额度、快照与附件占用、请求次数及超额收费，不把告警称为硬性停费。
- [ ] 依据前述场景执行最终双工作空间 R2 验收；缺真实凭证或运行环境时保持验收未完成。
