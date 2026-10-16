import {waitForPendingTransactions} from "./transactionQueue";
import {registerEditorSave} from "./editorSave";
import {localMarkdownRequest} from "../../util/localMarkdown";
import {getHostCapabilities} from "../../util/hostCapabilities";
import {showMessage} from "../../dialog/message";
import {Constants} from "../../constants";
import {isBrowser} from "../../util/functions";
/// #if !BROWSER
import {ipcRenderer} from "electron";
/// #endif
import type {Protyle} from "../index";
import {reportLocalFileFailure} from "./localFileRefresh";

const saving = new Set<string>();

export const saveLocalMarkdown = async (editor: Protyle) => {
    const protyle = editor.protyle;
    const id = protyle.block.rootID;
    if (saving.has(id)) {
        return;
    }
    saving.add(id);
    const tracker = registerEditorSave(protyle);
    try {
        await protyle.wysiwyg.flushPendingInput();
        protyle.title?.flushPendingInput();
        await waitForPendingTransactions(protyle);
        await tracker.flush();
        if (getHostCapabilities().localFileSystem && !isBrowser()) {
            /// #if !BROWSER
            let {file} = await localMarkdownRequest({action: "file", id});
            if (!file?.linked) {
                const result = await ipcRenderer.invoke(Constants.SIYUAN_GET, {
                    cmd: "showSaveDialog",
                    title: window.siyuan.languages.saveAs,
                    defaultPath: file?.name || `${id}.md`,
                    filters: [{name: "Markdown", extensions: ["md", "markdown"]}, {name: "TXT", extensions: ["txt"]}],
                    properties: ["showOverwriteConfirmation"],
                });
                if (result.canceled || !result.filePath) {
                    return;
                }
                ({file} = await localMarkdownRequest({action: "file", id, path: result.filePath}));
                await localMarkdownRequest({action: "bind", id, path: file.path, revision: file.revision});
            }
            const refreshed = await localMarkdownRequest({action: "refresh", id});
            if (!refreshed.changed) {
                await localMarkdownRequest({action: "current", id});
            }
            showMessage(`${window.siyuan.languages.save}: ${file.path}`, 3000);
            /// #endif
        }
    } catch (error) {
        reportLocalFileFailure(id, error, true);
    } finally {
        tracker.dispose();
        saving.delete(id);
    }
};
