import * as assert from "node:assert/strict";
import {test} from "node:test";
import {readFileSync} from "node:fs";
import {join} from "node:path";
import {runInNewContext} from "node:vm";
import * as ts from "typescript";

const setup = (options: {failAt?: string, linked?: boolean, canceled?: boolean, browser?: boolean, externalChanged?: boolean} = {}) => {
    const order: string[] = [];
    const task = (name: string) => async () => {
        order.push(name);
        if (options.failAt === name) {
            throw new Error("injected failure");
        }
    };
    const exports: {saveLocalMarkdown?: (editor: unknown) => Promise<void>} = {};
    const source = ts.transpileModule(readFileSync(join(__dirname, "localMarkdownSave.ts"), "utf8"), {
        compilerOptions: {module: ts.ModuleKind.CommonJS},
    }).outputText;
    runInNewContext(source, {
        exports,
        require: (name: string) => {
            switch (name) {
                case "./transactionQueue": return {waitForPendingTransactions: task("transactions")};
                case "./editorSave": return {registerEditorSave: () => ({flush: task("confirmed"), dispose: () => order.push("dispose")})};
                case "./localFileRefresh": return {reportLocalFileFailure: () => order.push("error")};
                case "../../util/hostCapabilities": return {getHostCapabilities: () => ({localFileSystem: true})};
                case "../../util/functions": return {isBrowser: () => options.browser === true};
                case "../../constants": return {Constants: {SIYUAN_GET: "get"}};
                case "electron": return {ipcRenderer: {invoke: async (channel: string, request: {cmd: string}) => {
                    assert.equal(request.cmd, "showSaveDialog");
                    await task("dialog")();
                    return {canceled: options.canceled, filePath: "C:/notes/a.txt"};
                }}};
                case "../../util/localMarkdown": return {localMarkdownRequest: async (request: {
                    action: string, id: string, path?: string, revision?: string
                }) => {
                    assert.equal(request.id, "doc-id");
                    if (request.action === "file") {
                        await task(request.path ? "candidate" : "file")();
                        return {file: {name: "a.md", path: "C:/notes/a.txt", linked: options.linked !== false, revision: "revision"}};
                    }
                    if (request.action === "bind") {
                        assert.equal(request.path, "C:/notes/a.txt");
                        assert.equal(request.revision, "revision");
                        await task("bind")();
                        return {};
                    }
                    if (request.action === "refresh") {
                        await task("refresh")();
                        return {changed: options.externalChanged === true};
                    }
                    assert.equal(request.action, "current");
                    await task("save")();
                    return {};
                }};
                case "../../dialog/message": return {showMessage: (message: string, timeout: number, type?: string) =>
                    order.push(type === "error" ? "error" : "success")};
                default: throw new Error(name);
            }
        },
        window: {siyuan: {languages: {save: "Save", saveAs: "Save as", exportFileSaveFailed: "Save failed"}}},
    });
    const editor = {protyle: {block: {rootID: "doc-id"}, wysiwyg: {flushPendingInput: task("input")},
        title: {flushPendingInput: () => order.push("title")}}};
    return {order, run: () => exports.saveLocalMarkdown(editor)};
};

const internalSave = ["input", "title", "transactions", "confirmed"];

test("Ctrl+S writes a linked local file only after confirmed internal persistence, without a settings dialog", async () => {
    const fixture = setup();
    await fixture.run();
    assert.deepEqual(fixture.order, [...internalSave, "file", "refresh", "save", "success", "dispose"]);
});

test("first save selects a path and records the inspected revision before writing", async () => {
    const fixture = setup({linked: false});
    await fixture.run();
    assert.deepEqual(fixture.order, [...internalSave, "file", "dialog", "candidate", "bind", "refresh", "save", "success", "dispose"]);
});

test("canceling Save as keeps the internal save without creating a file or reporting success", async () => {
    const fixture = setup({linked: false, canceled: true});
    await fixture.run();
    assert.deepEqual(fixture.order, [...internalSave, "file", "dialog", "dispose"]);
});

test("failed internal persistence prevents file writes; failed external writes never report success", async () => {
    const internal = setup({failAt: "confirmed"});
    await internal.run();
    assert.deepEqual(internal.order, [...internalSave, "error", "dispose"]);
    const external = setup({failAt: "save"});
    await external.run();
    assert.deepEqual(external.order, [...internalSave, "file", "refresh", "save", "error", "dispose"]);
});

test("Ctrl+S adopts a clean external change without immediately rewriting it", async () => {
    const fixture = setup({externalChanged: true});
    await fixture.run();
    assert.deepEqual(fixture.order, [...internalSave, "file", "refresh", "success", "dispose"]);
});

test("browser saves and repeated concurrent shortcuts do not open native dialogs", async () => {
    const browser = setup({browser: true});
    await browser.run();
    assert.deepEqual(browser.order, [...internalSave, "dispose"]);
    const fixture = setup({linked: false});
    await Promise.all([fixture.run(), fixture.run()]);
    assert.equal(fixture.order.filter(item => item === "dialog").length, 1);
    assert.equal(fixture.order.filter(item => item === "save").length, 1);
});
