import {localMarkdownRequest} from "../../util/localMarkdown";
import {getHostCapabilities} from "../../util/hostCapabilities";
import {isBrowser} from "../../util/functions";
import {registerEditorSave} from "./editorSave";
import {waitForPendingTransactions} from "./transactionQueue";
import {reloadProtyle} from "./reload";
import {showMessage} from "../../dialog/message";
import {confirmDialog} from "../../dialog/confirmDialog";
import {escapeHtmlTextAndAttr} from "../../util/escape";

const editors = new Set<IProtyle>();
const running = new Set<string>();
const failures = new Map<string, string>();
const confirmations = new Set<string>();

export const reportLocalFileFailure = (id: string, error: Error, manual = false) => {
    const conflict = error.message.includes("local file conflict:");
    if (manual && conflict && !confirmations.has(id)) {
        failures.set(id, error.message);
        confirmations.add(id);
        confirmDialog(window.siyuan.languages.localMarkdownConflict,
            `${escapeHtmlTextAndAttr(window.siyuan.languages.localFileReloadPrompt)}<br><br>${escapeHtmlTextAndAttr(error.message)}`,
            () => {
                confirmations.delete(id);
                const editor = [...editors].find(item => item.block.rootID === id);
                if (editor) {
                    void refreshLocalFile(editor, true);
                }
            }, () => confirmations.delete(id), false, undefined, window.siyuan.languages.refresh);
        return;
    }
    if (failures.get(id) !== error.message) {
        failures.set(id, error.message);
        showMessage(`${window.siyuan.languages[conflict ? "localMarkdownConflict" : "exportFileSaveFailed"]}: ${error.message}`, 7000, "error");
    }
};

export const refreshLocalFile = async (protyle: IProtyle, acceptExternal = false) => {
    const id = protyle.block.rootID;
    if (!id || running.has(id) || protyle.disabled || !protyle.path?.endsWith(".sy")) {
        return;
    }
    running.add(id);
    const related = [...editors].filter(item => item.block.rootID === id && !item.disabled && item.element.isConnected);
    if (!related.includes(protyle)) {
        related.push(protyle);
    }
    const restore: (() => void)[] = [];
    const guards = related.map(item => registerEditorSave(item));
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 5000);
    try {
        if (!acceptExternal) {
            const checked = await localMarkdownRequest({action: "check", id}, controller.signal);
            if (!checked.changed) {
                return;
            }
        }
        if (!protyle.element.isConnected || protyle.block.rootID !== id || protyle.disabled) {
            return;
        }
        // 只给只读检查设置超时；载入提交后要等待结果，避免提前恢复输入。
        window.clearTimeout(timeout);
        for (const item of related) {
            await item.wysiwyg.flushPendingInput();
            item.title?.flushPendingInput();
            // 载入只在失焦或重新获得焦点时执行，提交期间暂时禁止输入，避免覆盖新输入。
            [item.wysiwyg.element, item.title?.editElement].filter(Boolean).forEach(element => {
                const editable = element.getAttribute("contenteditable");
                element.setAttribute("contenteditable", "false");
                restore.push(() => {
                    if (!item.disabled && item.block.rootID === id) {
                        if (editable === null) {
                            element.removeAttribute("contenteditable");
                        } else {
                            element.setAttribute("contenteditable", editable);
                        }
                    }
                });
            });
            await waitForPendingTransactions(item);
        }
        await Promise.all(guards.map(guard => guard.flush()));
        const data = await localMarkdownRequest({action: "refresh", id, acceptExternal}, controller.signal);
        failures.delete(id);
        if (data.changed) {
            related.filter(item => item.block.rootID === id).forEach(item => reloadProtyle(item, false));
        }
    } catch (error) {
        if (error.name !== "AbortError") {
            reportLocalFileFailure(id, error);
        }
    } finally {
        window.clearTimeout(timeout);
        restore.forEach(callback => callback());
        guards.forEach(guard => guard.dispose());
        running.delete(id);
    }
};

export const registerLocalFileRefresh = (protyle: IProtyle) => {
    if (isBrowser() || !getHostCapabilities().localFileSystem) {
        return () => {};
    }
    editors.add(protyle);
    const check = () => {
        if (protyle.element.isConnected) {
            void refreshLocalFile(protyle);
        }
    };
    const timer = window.setInterval(() => {
        if (!document.hasFocus()) {
            check();
        }
    }, 1500);
    window.addEventListener("focus", check);
    return () => {
        window.clearInterval(timer);
        window.removeEventListener("focus", check);
        editors.delete(protyle);
        if (![...editors].some(item => item.block.rootID === protyle.block.rootID)) {
            failures.delete(protyle.block.rootID);
        }
    };
};
