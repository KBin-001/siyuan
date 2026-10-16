const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const net = require("node:net");
const {spawn, spawnSync} = require("node:child_process");

const root = path.resolve(__dirname, "..");
const appDir = path.join(root, "app");
const port = 6806;

function firstExisting(candidates, label) {
    const found = candidates.find(candidate => candidate && fs.existsSync(candidate));
    if (!found) {
        throw new Error(`找不到${label}，请先安装工具链或恢复之前准备的临时工具链`);
    }
    return found;
}

function findOnPath(name) {
    const result = spawnSync("where.exe", [name], {encoding: "utf8", windowsHide: true});
    return result.status === 0 ? result.stdout.trim().split(/\r?\n/)[0] : "";
}

function createPlan(env = process.env) {
    const temporaryTools = path.join(env.TEMP || os.tmpdir(), "siyuan-toolchain");
    const runtime = path.join(env.USERPROFILE || os.homedir(), ".cache", "codex-runtimes",
        "codex-primary-runtime", "dependencies", "node");
    const go = firstExisting([path.join(temporaryTools, "go", "bin", "go.exe"), findOnPath("go.exe")], " Go");
    const gcc = firstExisting([path.join(temporaryTools, "w64devkit", "bin", "gcc.exe"), findOnPath("gcc.exe")], " GCC");
    const pnpm = firstExisting([
        path.join(runtime, "node_modules", "pnpm", "bin", "pnpm.mjs"),
        path.join(env.APPDATA || "", "npm", "node_modules", "pnpm", "bin", "pnpm.cjs"),
    ], " pnpm");
    const electron = firstExisting([path.join(appDir, "node_modules", "electron", "dist", "electron.exe")], " Electron 依赖");
    firstExisting([path.join(appDir, "node_modules", "webpack", "bin", "webpack.js")], "前端依赖");
    const workspace = path.resolve(env.SIYUAN_DEV_WORKSPACE || path.join(root, "..", "siyuan-test-workspace"));
    const childEnv = {
        ...env,
        PATH: [path.dirname(go), path.dirname(gcc), path.dirname(process.execPath), env.PATH || env.Path || ""].join(path.delimiter),
        CC: gcc,
        CGO_ENABLED: "1",
        npm_config_manage_package_manager_versions: "false",
        npm_config_package_manager_strict: "false",
    };
    if (go.startsWith(temporaryTools + path.sep)) {
        childEnv.GOCACHE = path.join(temporaryTools, "go-cache");
        childEnv.GOMODCACHE = path.join(temporaryTools, "go-modules");
        childEnv.GOPATH = path.join(temporaryTools, "gopath");
    }
    // 避免从设置了 Electron Node 模式的终端启动时丢失图形界面。
    delete childEnv.ELECTRON_RUN_AS_NODE;
    const kernel = path.join(appDir, "kernel", "SiYuan-Kernel.exe");
    return {
        root, workspace, env: childEnv,
        build: {file: go, args: ["build", "-p", "1", "-tags", "fts5 sqlcipher", "-o", kernel, "."], cwd: path.join(root, "kernel")},
        // 沿用开发编译命令，但只编译一次，避免额外留下后台监听进程。
        frontend: {file: process.execPath, args: [pnpm, "run", "dev", "--no-watch"], cwd: appDir},
        kernel: {file: kernel, args: ["serve", "--wd", appDir, "--workspace", workspace, "--port", String(port), "--mode", "dev", "--lang", "zh-CN"], cwd: appDir},
        electron: {file: electron, args: [path.join(appDir, "electron", "main.js"), `--workspace=${workspace}`, `--port=${port}`], cwd: appDir, visible: true},
    };
}

function findConflicts(processes, repoRoot = root) {
    const normalizedRoot = repoRoot.toLowerCase().replaceAll("/", "\\");
    return processes.filter(item => {
        const command = String(item.CommandLine || "").toLowerCase().replaceAll("/", "\\");
        return item.ProcessId !== process.pid && command.includes(normalizedRoot + "\\") &&
            (/siyuan-kernel\.exe/.test(command) || /webpack(?:-cli)?[\\/]/.test(command) ||
                command.includes("electron\\main.js") || command.includes("start-local-dev.cjs"));
    });
}

function inspectProcesses() {
    const powershell = path.join(process.env.SystemRoot || "C:\\Windows", "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
    const command = "Get-CimInstance Win32_Process -Filter \"Name='SiYuan-Kernel.exe' OR Name='node.exe' OR Name='electron.exe'\" | Select-Object ProcessId,CommandLine | ConvertTo-Json -Compress";
    const result = spawnSync(powershell, ["-NoProfile", "-NonInteractive", "-Command", command], {
        encoding: "utf8", windowsHide: true, timeout: 15000,
    });
    if (result.status !== 0) {
        throw new Error("无法检查旧开发进程。请使用普通 Windows 终端运行，确认可以查询当前用户的进程");
    }
    const value = result.stdout.replace(/^\uFEFF/, "").trim();
    const parsed = value ? JSON.parse(value) : [];
    return Array.isArray(parsed) ? parsed : [parsed];
}

function checkPort() {
    return new Promise((resolve, reject) => {
        const server = net.createServer();
        server.once("error", () => reject(new Error(`端口 ${port} 正在使用，请先退出之前的测试版思源和内核窗口`)));
        server.listen(port, "127.0.0.1", () => server.close(resolve));
    });
}

function commandOptions(spec, env) {
    return {cwd: spec.cwd, env, windowsHide: spec.visible !== true, stdio: ["inherit", "pipe", "pipe"]};
}

function startCommand(spec, env, log) {
    const child = spawn(spec.file, spec.args, commandOptions(spec, env));
    for (const stream of [child.stdout, child.stderr]) {
        stream.on("data", data => { process.stdout.write(data); log.write(data); });
    }
    const done = new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("close", (code, signal) => {
            if (code === 0) { resolve(); } else { reject(new Error(`进程退出：${path.basename(spec.file)}，代码 ${code}，信号 ${signal || "无"}`)); }
        });
    });
    // 后台进程可能在就绪检查之前失败，立即记录结果，避免未处理的 Promise 拒绝。
    done.catch(() => {});
    return {child, done};
}

async function waitForKernel(fetcher = fetch, alive = () => true, timeout = 120000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
        if (!alive()) { throw new Error("内核在启动完成前退出，请查看上方日志"); }
        try {
            const response = await fetcher(`http://127.0.0.1:${port}/api/system/version`, {signal: AbortSignal.timeout(2000)});
            const data = await response.json();
            if (response.ok && data.code === 0 && typeof data.data === "string") { return; }
        } catch (_) {
            // 内核尚未监听时继续等待，不提前启动 Electron。
        }
        await new Promise(resolve => setTimeout(resolve, 500));
    }
    throw new Error("等待内核超时，请保留日志检查错误，不要反复启动多个内核");
}

async function executePlan(plan, operations) {
    await operations.run(plan.build, "1/4 编译 Go 内核");
    await operations.run(plan.frontend, "2/4 编译桌面界面");
    await operations.startKernel(plan.kernel);
    await operations.ready();
    await operations.run(plan.electron, "4/4 打开思源测试版");
}

async function main() {
    if (process.platform !== "win32") { throw new Error("此启动器仅适用于 Windows"); }
    const checkOnly = process.argv.includes("--check");
    const plan = createPlan();
    const conflicts = findConflicts(inspectProcesses());
    console.log(`源码：${root}\n测试工作空间：${plan.workspace}`);
    console.log("检查：Go、GCC、pnpm、Electron 和前端依赖已找到");
    if (checkOnly) {
        console.log(`只检查，不编译、不启动。发现旧开发进程：${conflicts.map(item => item.ProcessId).join(", ") || "无"}`);
        return;
    }
    if (conflicts.length) {
        throw new Error(`请先退出旧测试版，并在旧内核或 pnpm dev 窗口按 Ctrl+C。相关进程 PID：${conflicts.map(item => item.ProcessId).join(", ")}。关闭后重新双击 BAT`);
    }
    await checkPort();
    fs.mkdirSync(plan.workspace, {recursive: true});
    const logDir = path.join(os.tmpdir(), "siyuan-dev-launcher");
    fs.mkdirSync(logDir, {recursive: true});
    const logFile = path.join(logDir, `${new Date().toISOString().replaceAll(":", "-")}.log`);
    const log = fs.createWriteStream(logFile);
    console.log(`日志：${logFile}\n首次编译可能较慢，请保持这个窗口打开`);
    let kernel;
    process.on("SIGINT", () => console.log("正在等待子进程正常退出；请优先通过思源界面退出应用"));
    try {
        await executePlan(plan, {
            run: async (spec, label) => {
                console.log(`\n[${label}]`);
                await startCommand(spec, {...plan.env, NODE_ENV: "development"}, log).done;
            },
            startKernel: async spec => {
                console.log("\n[3/4 启动内核，等待端口就绪]");
                kernel = startCommand(spec, plan.env, log);
            },
            ready: () => waitForKernel(fetch, () => kernel.child.exitCode === null && !kernel.child.killed),
        });
        console.log("\n思源界面已退出。若内核仍在运行，可在此窗口按 Ctrl+C 正常结束内核");
        await kernel.done;
    } finally {
        if (kernel && kernel.child.exitCode === null) {
            console.log("内核仍在运行；请在此窗口按 Ctrl+C 正常退出，不会强制结束进程");
            await kernel.done.catch(() => {});
        }
        log.end();
    }
}

module.exports = {createPlan, findConflicts, executePlan, waitForKernel, commandOptions};
if (require.main === module) {
    main().catch(error => { console.error(`\n启动失败：${error.message}`); process.exitCode = 1; });
}
