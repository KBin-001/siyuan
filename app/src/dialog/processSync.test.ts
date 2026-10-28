import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {ModuleKind, transpileModule} from "typescript";
import {createHostQuitGuard} from "./hostQuit";

const {parse} = require("ifdef-loader/preprocessor");

for (const mobile of [false, true]) {
    test(`accountless S3 shows syncing, failure and recovery states (mobile=${mobile})`, () => {
        const source = parse(readFileSync("src/dialog/processSystem.ts", "utf8"), {MOBILE: mobile, BROWSER: true});
        const code = transpileModule(source, {compilerOptions: {module: ModuleKind.CommonJS}}).outputText;
        const createClasses = () => {
            const classes = new Set<string>();
            return {add: (value: string) => classes.add(value), remove: (value: string) => classes.delete(value),
                contains: (value: string) => classes.has(value)};
        };
        let icon = "";
        const svg = {classList: createClasses()};
        const use = {parentElement: svg, setAttribute: (_name: string, value: string) => {icon = value;}};
        const toolbar = {classList: createClasses(), firstElementChild: svg, querySelector: () => use};
        const config = {sync: {provider: 2, enabled: true}};
        const events: string[] = [];
        const pluginEvents: string[] = [];
        const exports = {} as {processSync: (data: {code: number}) => void};
        runInNewContext(code, {
            exports,
            document: {getElementById: () => toolbar, querySelector: (selector: string) => selector === "#barSync" ? toolbar : use},
            window: {siyuan: {config}, dispatchEvent: (event: {type: string}) => events.push(event.type)},
            CustomEvent: class {constructor(public type: string) {}},
            require(name: string) {
                if (name === "./hostQuit") return {createHostQuitGuard};
                if (name === "../util/needSubscribe") return {needSubscribe: () => true};
                if (name === "../plugin/EventBusCore") return {emitToPlugins: (name: string) => pluginEvents.push(name)};
                return {};
            },
        });
        exports.processSync({code: 0});
        assert.equal(icon, "#iconRefresh");
        assert.equal(svg.classList.contains("fn__rotate"), true);
        exports.processSync({code: 2});
        assert.equal(icon, "#iconCloudError");
        assert.equal(svg.classList.contains("fn__rotate"), false);
        exports.processSync({code: 0});
        exports.processSync({code: 1});
        assert.equal(icon, "#iconCloudSucc");
        assert.deepEqual(events, ["siyuan-sync-success"]);
        assert.deepEqual(pluginEvents, ["sync-start", "sync-fail", "sync-start", "sync-end"]);
        config.sync.enabled = false;
        exports.processSync({code: 2});
        assert.equal(icon, "#iconCloudOff");
        config.sync.enabled = true;
        config.sync.provider = 0;
        exports.processSync({code: 2});
        assert.equal(icon, "#iconCloudOff");
    });
}
