import {test} from "node:test";
import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {join} from "node:path";
import {runInNewContext} from "node:vm";
import {transpileModule, ModuleKind} from "typescript";

const setup = (options: {changed?: boolean, fail?: boolean} = {}) => {
    const events: string[] = [];
    const attributes = new Map([["contenteditable", "true"]]);
    const element = {isConnected: true, getAttribute: (key: string) => attributes.get(key) ?? null,
        setAttribute: (key: string, value: string) => attributes.set(key, value), removeAttribute: (key: string) => attributes.delete(key)};
    const protyle = {block: {rootID: "doc"}, path: "/doc.sy", element, wysiwyg: {element,
        flushPendingInput: async () => { events.push("input"); }}};
    const exports: {refreshLocalFile?: (editor: unknown) => Promise<void>, reportLocalFileFailure?: (id: string, error: Error) => void} = {};
    const source = transpileModule(readFileSync(join(__dirname, "localFileRefresh.ts"), "utf8"), {
        compilerOptions: {module: ModuleKind.CommonJS},
    }).outputText;
    runInNewContext(source, {
        exports, AbortController,
        window: {setTimeout: () => 1, clearTimeout: () => {}, siyuan: {languages: {localMarkdownConflict: "Conflict", exportFileSaveFailed: "Failed"}}},
        require: (name: string) => {
            if (name === "../../util/localMarkdown") {
                return {localMarkdownRequest: async (request: {action: string}) => {
                    events.push(request.action);
                    if (request.action === "check") {
                        return {changed: options.changed === true};
                    }
                    assert.equal(attributes.get("contenteditable"), "false");
                    if (options.fail) {
                        throw new Error("local file conflict: file.md");
                    }
                    return {changed: true};
                }};
            }
            if (name === "./editorSave") {
                return {registerEditorSave: () => ({flush: async () => { events.push("confirmed"); }, dispose: () => events.push("dispose")})};
            }
            if (name === "./transactionQueue") {
                return {waitForPendingTransactions: async () => { events.push("transactions"); }};
            }
            if (name === "./reload") {
                return {reloadProtyle: () => events.push("reload")};
            }
            if (name === "../../dialog/message") {
                return {showMessage: () => events.push("error")};
            }
            if (name === "../../dialog/confirmDialog") {
                return {confirmDialog: () => events.push("confirm")};
            }
            if (name === "../../util/escape") {
                return {escapeHtmlTextAndAttr: (text: string) => text};
            }
            if (name === "../../util/hostCapabilities") {
                return {getHostCapabilities: () => ({localFileSystem: true})};
            }
            if (name === "../../util/functions") {
                return {isBrowser: () => false};
            }
            throw new Error(name);
        },
    });
    return {events, attributes, run: () => exports.refreshLocalFile(protyle), report: exports.reportLocalFileFailure};
};

test("unchanged files do not flush input, disable editing or reload", async () => {
    const fixture = setup();
    await fixture.run();
    assert.deepEqual(fixture.events, ["check", "dispose"]);
    assert.equal(fixture.attributes.get("contenteditable"), "true");
});

test("external reload waits for saved input and restores editability", async () => {
    const fixture = setup({changed: true});
    await fixture.run();
    assert.deepEqual(fixture.events, ["check", "input", "transactions", "confirmed", "refresh", "reload", "dispose"]);
    assert.equal(fixture.attributes.get("contenteditable"), "true");
});

test("a repeated conflict produces one notice and never reloads over local content", async () => {
    const fixture = setup({changed: true, fail: true});
    await fixture.run();
    await fixture.run();
    assert.equal(fixture.events.filter(event => event === "error").length, 1);
    assert.equal(fixture.events.includes("reload"), false);
    assert.equal(fixture.attributes.get("contenteditable"), "true");
});
