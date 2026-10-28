# Windows 双击编译并打开

适用于 Windows 10/11 x64。下载或拉取 `v2` 分支后，在项目根目录双击 `build-and-run.cmd`，无需管理员权限。首次会联网准备工具和依赖，编译界面、内核，再打开思源；后续会复用下载和编译缓存。

- 默认工作空间为系统“文档”目录下的 `SiYuan-v2`，不会自动打开原有工作空间
- 首次使用时只初始化尚不存在的工作空间列表；已有列表与设置不覆盖。编译版使用独立的 Electron 应用标识，避免官方客户端接管启动请求
- 构建输出与日志位于 `.cache/windows-desktop/`，不会写入开发服务器使用的 `app/stage/build`，工具缓存在用户目录 `.cache/siyuan-toolchain/`
- 重新编译前关闭该脚本打开的客户端；脚本不会强制结束已有进程
- 只打开上次的构建：运行 `build-and-run.cmd -LaunchOnly`
- 只编译：运行 `build-and-run.cmd -BuildOnly`
- 需要重新检查安装依赖：运行 `build-and-run.cmd -ReinstallDependencies`
- 指定工作空间：运行 `build-and-run.cmd -Workspace "D:\Notes\SiYuan-v2"`
- 出错时窗口保留，先检查 `.cache/windows-desktop/build.log`；下载需要能够访问 Node.js、Go、Zig、npm 注册表及 Electron 镜像

本次已在本机验证完整编译及从双击入口启动，新生成的内核版本接口返回 `3.8.7`。首次依赖供应链检查约用时六分钟，后续安装缓存已复用；实际耗时取决于网络。界面构建产生现有的资源体积提示，不影响编译完成。R2 接入与双电脑同步仍按单独清单验收。

脚本读取项目要求的 Go 与 pnpm 版本，使用 Node.js 24 和 Zig 0.15.2；自动下载的归档按发布方摘要校验。PowerShell 执行策略仅对本次启动进程设置，不修改系统策略。该目录是依赖源码和本机缓存的开发运行目录，不是可以复制给另一台电脑的独立安装包。

两台电脑均应从 `v2` 拉取代码后各自双击构建，再按 [R2 双电脑验收清单](../.scratch/cloudflare-r2-sync/desktop-acceptance.md)配置同一个私有桶并分别恢复同一个数据仓库密钥。
