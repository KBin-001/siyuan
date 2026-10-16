const assert = require("node:assert/strict");
const {test} = require("node:test");
const {findConflicts, executePlan, waitForKernel, commandOptions} = require("./start-local-dev.cjs");

test("Electron launches visibly while console helpers remain hidden", () => {
    assert.equal(commandOptions({visible: true}, {}).windowsHide, false);
    assert.equal(commandOptions({}, {}).windowsHide, true);
});

test("only this checkout's kernel, compiler and launcher block another launch", () => {
    const processes = [
        {ProcessId: 1, CommandLine: '"C:\\Repo Space\\siyuan\\app\\kernel\\SiYuan-Kernel.exe" serve'},
        {ProcessId: 2, CommandLine: 'node "C:\\Repo Space\\siyuan\\app\\node_modules\\webpack\\bin\\webpack.js"'},
        {ProcessId: 3, CommandLine: 'electron "C:\\Other\\app\\electron\\main.js"'},
        {ProcessId: 4, CommandLine: 'node "C:\\Repo Space\\siyuan\\scripts\\start-local-dev.cjs"'},
        {ProcessId: process.pid, CommandLine: 'node "C:\\Repo Space\\siyuan\\scripts\\start-local-dev.cjs"'},
    ];
    assert.deepEqual(findConflicts(processes, "C:\\Repo Space\\siyuan").map(item => item.ProcessId), [1, 2, 4]);
});

test("compile failures stop before a kernel or Electron is started", async () => {
    const calls = [];
    await assert.rejects(executePlan({build: "go", frontend: "frontend", kernel: "kernel", electron: "ui"}, {
        run: async step => { calls.push(step); if (step === "frontend") { throw new Error("compile failed"); } },
        startKernel: async () => calls.push("kernel"),
        ready: async () => calls.push("ready"),
    }), /compile failed/);
    assert.deepEqual(calls, ["go", "frontend"]);
});

test("a ready kernel is required before opening the UI", async () => {
    const calls = [];
    await executePlan({build: "go", frontend: "frontend", kernel: "kernel", electron: "ui"}, {
        run: async step => calls.push(step),
        startKernel: async () => calls.push("kernel"),
        ready: async () => { calls.push("ready"); await waitForKernel(async () => ({ok: true, json: async () => ({code: 0, data: "version"})})); },
    });
    assert.deepEqual(calls, ["go", "frontend", "kernel", "ready", "ui"]);
    await assert.rejects(waitForKernel(async () => { throw new Error("unreachable"); }, () => false), /内核在启动完成前退出/);
});
