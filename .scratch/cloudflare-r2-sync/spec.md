# Support personal desktop synchronization with Cloudflare R2

**Status:** ready-for-agent

**Approved:** 2026-10-28 — user approved the reviewed draft; planning only, implementation not started.

## Problem Statement

用户希望在家中和其他地点的电脑上持续编辑同一套思源笔记，不支付思源同步功能或年费。已有源码包含第三方同步实现，但设置界面、同步执行和云端快照操作存在账号或会员条件。用户选择 Cloudflare 存储并持有域名，希望先得到明确、可验证的实施方案。

## Solution

在自用桌面客户端中提供无需官方账号和会员的 S3 同步路径，连接用户自己的 Cloudflare R2 私有桶。复用现有数据仓库密钥、加密、快照和冲突处理。第一台电脑配置后，另一台电脑导入存储配置并单独恢复相同的数据仓库密钥；完成首次同步后自动交换后续变更。

用户已于 2026-10-28 批准该阶段方案。它降低多电脑同步成本，但首次设备接入仍有配置导入步骤，不等同于自建账号登录。

2026-10-28 实施补充：用户明确批准修复已复现的底层引擎同秒冲突缺陷，并完成旧数据兼容验证。新 S3 文件对象使用含内容信息的 v2 标识，旧标识继续可读；不改变加密、压缩、分块、密钥和元数据结构，不重写旧快照。必要引擎根包以固定来源纳入项目，保留原许可证与哈希，确保修复可随本仓库复现构建；详细边界见 `kernel/internal/dejavu/README.md`。

## User Stories

1. As a personal desktop user, I want to configure my own R2 storage without an official account, so that I can use synchronization independently.
2. As a personal desktop user, I want to synchronize without a paid membership, so that I can avoid the official synchronization fee.
3. As a storage owner, I want my synchronization bucket to remain private, so that only authorized devices can access it.
4. As a storage owner, I want credentials scoped to one bucket, so that other cloud resources are outside their authority.
5. As a first-device user, I want clear required fields and connection errors, so that I can fix configuration mistakes.
6. As a first-device user, I want to initialize and retain my repository key, so that my encrypted data remains recoverable.
7. As a second-device user, I want to import the existing storage configuration, so that I do not repeatedly type connection values.
8. As a second-device user, I want a clear repository-key recovery step, so that I can decrypt the same cloud repository.
9. As a second-device user, I want to download existing notes into a fresh workspace, so that I can continue my work elsewhere.
10. As a desktop user, I want attachments to follow my notes, so that images and files remain usable across devices.
11. As a desktop user, I want changes to synchronize automatically, so that I need fewer manual steps after setup.
12. As a desktop user, I want a manual sync action and useful status, so that I can verify progress before switching computers.
13. As an offline user, I want local changes preserved while disconnected, so that I can keep writing without losing work.
14. As a returning online user, I want failed sync operations to recover through the existing retry flow, so that temporary outages do not require resetting my workspace.
15. As a user editing on two devices, I want conflicting versions preserved through the existing conflict-document option, so that neither edit silently disappears.
16. As a desktop user, I want renames and deletions to converge according to existing synchronization semantics, so that both computers show a consistent workspace.
17. As a user with an incorrect key, I want a clear authentication failure that preserves existing data, so that a setup mistake cannot destroy my notes.
18. As a user rotating a cloud credential, I want to update it without regenerating repository keys, so that access can be restored without losing history.
19. As a user managing history, I want cloud snapshot backup and recovery to work with my own S3 storage, so that I can recover an earlier version.
20. As a cost-conscious user, I want to understand storage and request consumption, so that I can monitor whether I remain within R2 allowances.
21. As a user with encrypted notebooks, I want existing encrypted formats and recovery material preserved, so that this change does not break existing notebooks.
22. As an existing official-cloud user, I want the official service to retain its own authorization rules, so that third-party support does not change official service access.
23. As a domain owner, I want to know whether a custom domain is needed, so that I avoid unnecessary infrastructure.
24. As a desktop user, I want clear first-run instructions for each new computer, so that configuration import is not confused with account login.

## Implementation Decisions

- First phase targets desktop-to-desktop personal use with private R2 storage; exact supported desktop operating systems follow the existing application's build support and available verification environment.
- Reuse the S3 provider and data repository. Do not add a parallel synchronization protocol or change the on-disk or remote encryption format.
- Scope any account/entitlement separation to the S3 path required for this feature. Do not globally change the paid-user predicate or simulate a paid official account.
- Audit UI eligibility, manual and scheduled execution, boot/exit synchronization, cloud snapshot operations, and error reporting as one feature surface.
- Preserve administrative authorization for local API access, read-only mode, configuration validation, and official-cloud service authorization.
- Use existing S3 configuration fields and configuration import/export contracts. Verify actual export/import round-trip behavior before relying on existing packages. Treat exports as sensitive; the current encoding alone is not evidence of strong user-password protection.
- Keep repository-key recovery distinct from storage-credential import. Never reset an existing key or salt to make synchronization proceed.
- Preserve current S3 bucket-based repository addressing and migration behavior. Switching destinations must retain source data and recovery material.
- Use automatic sync through existing scheduling. Official cloud perception mechanisms are not assumed available for R2; no real-time push or simultaneous collaboration guarantee is made.
- Keep R2 buckets private and use the control-panel S3 endpoint and region auto. No public bucket endpoint, caching proxy, or custom domain is needed.
- Prefer bucket-scoped credentials. Validate the exact operations required by the existing engine against R2 before increasing permissions.
- Usage documentation must distinguish storage, request operations, and egress. No hard spending cap or permanently free service guarantee is promised.
- Local planning documents contain placeholders only. Cloud credentials, exported packages, repository keys, and actual note data stay outside the repository.
- The user approved first-phase configuration/key import. Full account-only onboarding remains a subsequent design decision.

## Testing Decisions

- Main seam: two independent workspaces exercising existing sync settings and HTTP/synchronization entry points against a private S3-compatible target.
- Assert externally visible notes, assets, sync state, errors, and recoverability rather than the arrangement of helper functions.
- Cover no official account, a free official account, and official-provider authorization regression cases.
- Cover real configuration export/import round trips, missing required fields, invalid credentials, denied operations, wrong encryption key, restart persistence, and retained source data after failure.
- Cover create/update/rename/delete, automatic and manual sync, conflicting edits with conflict documents enabled, temporary disconnection, and snapshot restore.
- Preserve encrypted-notebook compatibility fixtures when affected paths change; add no new encryption format.
- Reuse existing HTTP contract, sync-provider validation, UI visibility, and repository-recovery regression patterns.
- Automated tests use disposable fixtures; an opt-in real R2 smoke test records upload, second-workspace download, edit-back, attachment access, and credential revocation behavior.
- Never report real R2 verification complete without actual account access and a successful two-workspace run. Do not compile or restart the application kernel during this planning phase.

## Out of Scope

- This phase does not build an account server, email/password login, device-pairing backend, or a cloud key-recovery service.
- Mobile clients, browser-only hosting, real-time collaboration, sharing, public attachment hosting, and custom-domain services are outside phase one.
- No broad opening of every paid feature or changes to official cloud subscriptions.
- No new WebDAV or local-folder synchronization implementation.
- No automatic deletion of history or cloud objects to enforce an estimated free-tier budget.
- Production cloud provisioning and code implementation are separate execution work, not part of publishing this plan.

## Further Notes

The user owns a domain, but it is not a dependency of private R2 S3 synchronization. Keep it available for a later account-portal design if account-only onboarding remains necessary.

R2's current included monthly allowances are 10 GB-month of Standard storage, one million Class A operations, and ten million Class B operations, with free Internet egress. Usage beyond included allowances can be billed. Snapshots, encrypted chunks, and assets contribute to actual usage.

The user approved one-time configuration/key import for phase one. Account-only onboarding remains a future phase; any decision to include it immediately requires revising the scope and ticket breakdown before implementation.


## References

- [R2 S3 API and region](https://developers.cloudflare.com/r2/api/s3/api/)
- [R2 credentials and bucket permissions](https://developers.cloudflare.com/r2/api/tokens/)
- [R2 public buckets and custom domains](https://developers.cloudflare.com/r2/buckets/public-buckets/)
- [R2 pricing and included allowances](https://developers.cloudflare.com/r2/pricing/)
