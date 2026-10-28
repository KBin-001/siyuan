import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {ModuleKind, transpileModule} from "typescript";
import {escapeHtml} from "../util/escape";

const {parse} = require("ifdef-loader/preprocessor");

type ListResponse = {
    code: number;
    msg?: string;
    data?: {
        syncDirs: {cloudName: string, hSize: string, updated: string}[];
        checkedSyncDir: string;
    };
};

const loadSyncGuide = (mobile: boolean, provider: number, paid = true) => {
    const source = readFileSync(resolve(process.cwd(), "src/sync/syncGuide.ts"), "utf8");
    const processed = parse(source, {MOBILE: mobile, BROWSER: true}, false, true);
    const code = transpileModule(processed, {compilerOptions: {module: ModuleKind.CommonJS}}).outputText;
    const sync = {
        provider, cloudName: "work", enabled: true, mode: 2,
        s3: {endpoint: "https://s3.example.com", accessKey: "test-key", secretKey: "test-secret",
            bucket: "notes.backup", region: "us-east-1"},
    };
    const requests: {url: string, data: {name?: string; sync?: {cloudName: unknown}}, reply: (response: ListResponse) => void}[] = [];
    const messages: string[] = [];
    const config = {sync, readonly: false, repo: {key: "test-repository-key"}};
    const moduleExports = {} as {
        syncGuide: () => void;
        renderSyncCloudList: (element: Element, reload: boolean, cb: (ready: boolean) => void) => void;
        bindSyncCloudListEvent: (element: Element, cb?: (ready: boolean) => void) => void;
    };
    runInNewContext(code, {
        exports: moduleExports,
        window: {siyuan: {config, languages: {emptyContent: "Empty", cloudConfigTip: "Configure storage",
            _kernel: {29: "Subscription required", 214: "Membership required"}}}},
        require: (name: string) => {
            if (name === "../util/needSubscribe") {
                return {isPaidUser: () => paid, needSubscribe: () => !paid};
            }
            if (name === "../dialog/message") {
                return {showMessage: (message: string) => messages.push(message)};
            }
            if (name === "../config/util/about") {
                return {getCloudURL: () => "https://example.com"};
            }
            if (name === "../config/util/configApi") {
                return {config: {set: (_path: string, value: unknown, applied?: () => void) => {
                    requests.push({url: "/api/setting/patch", data: {sync: {cloudName: value}}, reply: (response: ListResponse) => {
                        if (response.code === 0) { sync.cloudName = String(value); applied?.(); }
                    }});
                    return Promise.resolve();
                }}};
            }
            if (name === "../util/fetch") {
                return {fetchPost: (url: string, data: {name?: string}, reply: (response: ListResponse) => void) => {
                    requests.push({url, data, reply});
                }};
            }
            if (name === "../util/hostCapabilities") {
                return {sanitizeKernelHTML: (html: string) => html};
            }
            if (name === "../util/escape") {
                return {escapeHtml};
            }
            return {};
        },
    });
    let click: (event: Event) => void;
    const list = {
        innerHTML: "",
        firstElementChild: null,
        addEventListener: (_name: string, cb: (event: Event) => void) => {
            click = cb;
        },
    } as unknown as Element;
    const readiness: boolean[] = [];
    return {
        ...moduleExports, list, requests, sync, config, messages, readiness,
        render: () => moduleExports.renderSyncCloudList(list, true, (ready) => readiness.push(ready)),
        select: () => {
            moduleExports.bindSyncCloudListEvent(list);
            click({
                target: {
                    isEqualNode: () => false,
                    getAttribute: (name: string) => name === "data-type" ? "selectCloud" : "chosen",
                },
                preventDefault: () => {},
                stopPropagation: () => {},
            } as unknown as Event);
        },
    };
};

test("an unpaid desktop user can manually synchronize S3 while other providers retain membership checks", () => {
    for (const provider of [2, 0, 3, 4]) {
        const guide = loadSyncGuide(false, provider, false);
        guide.syncGuide();
        assert.deepEqual(guide.requests.map(request => request.url), provider === 2 ? ["/api/sync/performSync"] : []);
        assert.equal(guide.messages.length, provider === 2 ? 0 : 1);
    }
});

test("manual S3 sync still respects read-only mode and disabled synchronization", () => {
    const guide = loadSyncGuide(false, 2, false);
    guide.config.readonly = true;
    guide.syncGuide();
    guide.config.readonly = false;
    guide.sync.enabled = false;
    guide.syncGuide();
    assert.equal(guide.requests.length, 0);
});

const listResponse = (cloudName: string, checkedSyncDir: string): ListResponse => ({
    code: 0,
    data: {syncDirs: [{cloudName, hSize: "-", updated: "2026-01-01"}], checkedSyncDir},
});

for (const mobile of [false, true]) {
    test(`S3 displays its configured bucket without requiring bucket listing (mobile=${mobile})`, () => {
        const guide = loadSyncGuide(mobile, 2);
        guide.render();
        assert.equal(guide.requests.length, 0);
        assert.match(guide.list.innerHTML, /notes\.backup/);
        assert.doesNotMatch(guide.list.innerHTML, /type="radio"|data-type="(?:selectCloud|addCloud|removeCloud)"/);
        assert.deepEqual(guide.readiness, [true]);
        assert.equal(guide.sync.cloudName, "work");
        guide.select();
        assert.equal(guide.requests.length, 0);
        assert.doesNotMatch(guide.list.innerHTML, /test-key|test-secret/);
    });

    test(`S3 refreshes the configured bucket and escapes its name (mobile=${mobile})`, () => {
        const guide = loadSyncGuide(mobile, 2);
        guide.render();
        Object.defineProperty(guide.list, "firstElementChild", {value: {tagName: "DIV"}});
        guide.sync.s3.bucket = "<img src=x onerror=alert(1)>&";
        guide.renderSyncCloudList(guide.list, false, ready => guide.readiness.push(ready));
        assert.match(guide.list.innerHTML, /&lt;img src=x onerror=alert\(1\)>&amp;/);
        assert.doesNotMatch(guide.list.innerHTML, /<img|notes\.backup/);
        assert.equal(guide.requests.length, 0);
        assert.deepEqual(guide.readiness, [true, true]);
    });

    for (const provider of [0, 3, 4]) {
        test(`directory selection remains available (provider=${provider}, mobile=${mobile})`, () => {
            const guide = loadSyncGuide(mobile, provider);
            guide.render();
            guide.requests[0].reply(listResponse("work", "work"));
            assert.match(guide.list.innerHTML, /type="radio" name="cloudName" checked/);
            assert.match(guide.list.innerHTML, /data-type="selectCloud"/);
            assert.equal(guide.list.innerHTML.includes("data-type=\"removeCloud\""), provider !== 3);
            assert.equal(guide.list.innerHTML.includes("data-type=\"addCloud\""), provider !== 3);
            assert.deepEqual(guide.readiness, [false, true]);
            guide.select();
            assert.equal(guide.requests[1].url, "/api/setting/patch");
            assert.equal(guide.requests[1].data.sync.cloudName, "chosen");
            guide.requests[1].reply({code: 0});
            assert.equal(guide.sync.cloudName, "chosen");
            assert.equal(guide.requests[2].url, "/api/sync/listCloudSyncDir");
        });

        test(`the guide requires a selected directory (provider=${provider}, mobile=${mobile})`, () => {
            const guide = loadSyncGuide(mobile, provider);
            guide.render();
            guide.requests[0].reply(listResponse("main", "work"));
            assert.deepEqual(guide.readiness, [false, false]);
        });
    }

    for (const field of ["endpoint", "accessKey", "secretKey", "bucket", "region"] as const) {
        test(`S3 requires ${field} before enabling the guide (mobile=${mobile})`, () => {
            const guide = loadSyncGuide(mobile, 2);
            guide.sync.s3[field] = "   ";
            guide.render();
            assert.deepEqual(guide.readiness, [false]);
            assert.match(guide.list.innerHTML, /Configure storage/);
            assert.doesNotMatch(guide.list.innerHTML, /type="radio"|data-type="selectCloud"/);
            assert.equal(guide.sync.cloudName, "work");
            assert.equal(guide.requests.length, 0);
        });
    }

    test(`a pending listing cannot overwrite the UI after switching provider (mobile=${mobile})`, () => {
        const guide = loadSyncGuide(mobile, 0);
        guide.render();
        guide.sync.provider = 2;
        guide.render();
        const bucketHTML = guide.list.innerHTML;
        guide.requests[0].reply(listResponse("work", "work"));
        assert.equal(guide.list.innerHTML, bucketHTML);
        assert.match(guide.list.innerHTML, /notes\.backup/);
        assert.deepEqual(guide.readiness, [false, true]);
    });
}
