# 02: Support R2 onboarding on a second desktop

**What to build:** 第一台电脑使用私有 R2 桶同步后，第二台新电脑通过配置导入及仓库密钥恢复下载数据，并把后续编辑同步回第一台。

**Blocked by:** 01 — Support manual S3 synchronization without an official account.

**Status:** ready-for-agent

- [ ] R2 参数配置、导入、重启后持久化及双向手动同步完成完整闭环。
- [ ] 既有配置导出/导入通过真实往返验证；若不对称，在保持旧格式可读的前提下修复。
- [ ] 仓库密钥恢复步骤明确，导入配置不擅自替换既有仓库密钥。
- [ ] 私有桶使用受限凭证可工作；公开访问、自定义域名及 Workers 均非前置条件。
- [ ] 测试记录区分模拟 S3 验证与实际 R2 双工作空间验证；未取得云端访问时明确标记后者待验收。
- [ ] 已有数据的工作空间加入流程保留其未上传变更，配置错误或切换失败保留来源。
