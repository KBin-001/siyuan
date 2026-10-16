import type {LocalMarkdownConfig, LocalMarkdownData} from "../../types/api";
import {localMarkdownRequest} from "../../util/localMarkdown";
import {escapeHtmlTextAndAttr} from "../../util/escape";
import {writeText} from "../../protyle/util/compatibility";
import {getHostCapabilities} from "../../util/hostCapabilities";
import {trackSettingSave} from "../setting/pending";
/// #if !BROWSER
import {ipcRenderer} from "electron";
import {Constants} from "../../constants";
/// #endif

const labels = () => [window.siyuan.languages.localMarkdown, window.siyuan.languages.localMarkdownTip];
export const localMarkdownSearchStrings = () => labels();

export const localMarkdownHtml = () => {
    if (!getHostCapabilities().localFileSystem) {
        return "";
    }
    const l = window.siyuan.languages;
    let chooser = "";
    /// #if !BROWSER
    chooser = `<button data-md="choose" class="b3-button b3-button--outline">${l.select}</button>`;
    /// #endif
    return `<section class="b3-label config-item local-markdown">
        <label class="fn__flex b3-label--inner"><span class="fn__flex-1">${l.localMarkdown}</span>
            <input data-md="enabled" type="checkbox" class="b3-switch" aria-label="${l.localMarkdown}"></label>
        <div class="b3-label__text">${l.localMarkdownTip}</div>
        <label class="fn__block">${l.notebook}<select data-md="box" class="b3-select fn__block"></select></label>
        <label class="fn__block">${l.localMarkdownScope}<select data-md="root" class="b3-select fn__block"></select></label>
        <label class="fn__block">${l.savePath}<input data-md="target" class="b3-text-field fn__block" spellcheck="false"></label>
        <div class="fn__flex local-markdown__actions">
            ${chooser}
            <button data-md="preview" class="b3-button b3-button--outline">${l.preview}</button><span class="fn__space"></span>
            <button data-md="save" class="b3-button">${l.save}</button><span class="fn__space"></span>
            <button data-md="run" class="b3-button b3-button--outline">${l.localMarkdownNow}</button><span class="fn__space"></span>
            <button data-md="retry" class="b3-button b3-button--outline">${l.retry}</button><span class="fn__space"></span>
            <button data-md="logs" class="b3-button b3-button--outline">${l.localMarkdownLogs}</button>
        </div>
        <div data-md="status" class="b3-label__text" role="status" aria-live="polite"></div>
        <pre data-md="mapping" class="b3-label__text local-markdown__mapping"></pre>
        <div data-md="conflicts" class="b3-label__text"></div>
    </section>`;
};

const mounted = new WeakMap<HTMLElement, () => void>();
export const unmountLocalMarkdown = (root: HTMLElement) => {
    mounted.get(root)?.();
    mounted.delete(root);
};

export const mountLocalMarkdown = async (root: HTMLElement) => {
    unmountLocalMarkdown(root);
    const el = root.querySelector<HTMLElement>(".local-markdown");
    if (!el) {
        return;
    }
    const l = window.siyuan.languages;
    const enabled = el.querySelector<HTMLInputElement>('[data-md="enabled"]');
    const box = el.querySelector<HTMLSelectElement>('[data-md="box"]');
    const scope = el.querySelector<HTMLSelectElement>('[data-md="root"]');
    const target = el.querySelector<HTMLInputElement>('[data-md="target"]');
    const status = el.querySelector<HTMLElement>('[data-md="status"]');
    const mapping = el.querySelector<HTMLElement>('[data-md="mapping"]');
    const conflicts = el.querySelector<HTMLElement>('[data-md="conflicts"]');
    const controller = new AbortController();
    let disposed = false;
    let busy = false;
    let latest: LocalMarkdownData;
    let previewConfig = "";
    let poll: number;
    let scopeVersion = 0;
    const active = () => !disposed && el.isConnected;
    const read = (): LocalMarkdownConfig => ({enabled: enabled.checked, box: box.value, root: scope.value, target: target.value.trim()});
    const describe = (state: string) => ({
        idle: l.localMarkdownIdle, exporting: l.exporting, success: l.localMarkdownSuccess,
        failed: l.localMarkdownFailed, conflict: l.localMarkdownConflict,
    }[state] || state);
    const render = (data: LocalMarkdownData) => {
        latest = data;
        status.textContent = [describe(data.state), `${data.pending}`, data.lastSuccess &&
            `${l.localMarkdownLastSuccess}: ${new Date(data.lastSuccess).toLocaleString()}`, data.error].filter(Boolean).join(" | ");
        conflicts.replaceChildren();
        data.entries.filter(entry => entry.state === "failed" || entry.state === "conflict").forEach(entry => {
            const item = document.createElement("div");
            item.textContent = `${describe(entry.state)}: ${entry.path || entry.id}\n${entry.error}\n${entry.conflict}`;
            conflicts.append(item);
        });
    };
    const loadScopes = async (selected = "") => {
        const version = ++scopeVersion;
        const data = await localMarkdownRequest({action: "documents", config: {...read(), root: ""}});
        if (!active() || version !== scopeVersion) {
            return;
        }
        scope.innerHTML = `<option value="">${escapeHtmlTextAndAttr(l.localMarkdownWholeNotebook)}</option>` + data.documents.map(doc =>
            `<option value="${escapeHtmlTextAndAttr(doc.id)}">${escapeHtmlTextAndAttr(doc.source)}</option>`).join("");
        if (selected && !data.documents.some(doc => doc.id === selected)) {
            scope.add(new Option(selected, selected));
        }
        scope.value = selected;
    };
    const preview = async () => {
        const config = read();
        const data = await localMarkdownRequest({action: "preview", config});
        if (!active()) {
            return;
        }
        mapping.textContent = data.documents.map(doc =>
            `${doc.source}\n${doc.target}${doc.exists ? ` (${l.localMarkdownExisting})` : ""}`).join("\n\n");
        previewConfig = JSON.stringify(config);
        return data.documents.some(doc => doc.exists);
    };
    const save = async () => {
        if (read().enabled && previewConfig !== JSON.stringify(read())) {
            await preview();
        }
        const data = await trackSettingSave(localMarkdownRequest({action: "configure", config: read()}));
        if (active()) {
            render(data);
        }
    };
    el.addEventListener("click", async event => {
        const button = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-md]");
        if (!button || busy) {
            return;
        }
        busy = true;
        el.querySelectorAll<HTMLButtonElement>("button").forEach(item => item.disabled = true);
        try {
            switch (button.dataset.md) {
                case "choose": {
                    /// #if !BROWSER
                    const result = await ipcRenderer.invoke(Constants.SIYUAN_GET, {
                        cmd: "showOpenDialog", defaultPath: target.value || window.siyuan.config.system.homeDir,
                        properties: ["createDirectory", "openDirectory"],
                    });
                    if (!result.canceled && result.filePaths.length) {
                        target.value = result.filePaths[0];
                        previewConfig = "";
                    }
                    /// #endif
                    break;
                }
                case "preview": await preview(); break;
                case "save": await save(); break;
                case "run":
                    await preview();
                    await save();
                    render(await localMarkdownRequest({action: "run"}));
                    break;
                case "retry": render(await localMarkdownRequest({action: "retry"})); break;
                case "logs": writeText(JSON.stringify({...latest, clientMessage: status.textContent}, null, 2)); break;
            }
        } catch (error) {
            if (active()) {
                status.textContent = `${l.localMarkdownFailed}: ${error.message}`;
            }
        } finally {
            busy = false;
            if (active()) {
                el.querySelectorAll<HTMLButtonElement>("button").forEach(item => item.disabled = false);
            }
        }
    }, {signal: controller.signal});
    box.addEventListener("change", () => {
        previewConfig = "";
        void loadScopes().catch(error => status.textContent = error.message);
    }, {signal: controller.signal});
    mounted.set(root, () => {
        disposed = true;
        controller.abort();
        window.clearTimeout(poll);
    });
    const refresh = async () => {
        if (!active()) {
            return;
        }
        try {
            const data = await localMarkdownRequest({action: "status"});
            if (active() && !busy) {
                render(data);
            }
        } catch (error) {
            if (active()) {
                status.textContent = error.message;
            }
        } finally {
            if (active()) {
                poll = window.setTimeout(refresh, 1500);
            }
        }
    };
    try {
        const data = await localMarkdownRequest({action: "status"});
        if (!active()) {
            return;
        }
        render(data);
        enabled.checked = data.config.enabled;
        target.value = data.config.target;
        box.innerHTML = window.siyuan.notebooks.filter(item => !item.closed).map(item =>
            `<option value="${escapeHtmlTextAndAttr(item.id)}">${escapeHtmlTextAndAttr(item.name)}</option>`).join("");
        if (data.config.box) {
            box.value = data.config.box;
        }
        if (box.value) {
            await loadScopes(data.config.root);
        }
    } catch (error) {
        if (active()) {
            status.textContent = error.message;
        }
    }
    void refresh();
};
