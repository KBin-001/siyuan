interface EditorSaveRecord {
    pending: Set<Promise<void>>;
    results: Map<string, {version: number, failed: boolean}>;
    version: number;
    failedStructure: boolean;
    disposed: boolean;
}

const editors = new Map<IProtyle, Set<EditorSaveRecord>>();

interface ActiveEditorSave {
    session?: string;
    notebook?: string;
    path?: string;
    keys: string[];
    result: Promise<boolean>;
}

const activeSaves = new Set<ActiveEditorSave>();
const matchesEditor = (save: ActiveEditorSave, protyle: IProtyle) => save.session !== undefined ?
    save.session === protyle.id : Boolean(protyle.path && protyle.notebookId &&
        save.path === protyle.path && save.notebook === protyle.notebookId);

const observeSave = (record: EditorSaveRecord, save: ActiveEditorSave) => {
    const version = ++record.version;
    const pending = save.result.then(failed => {
        if (record.disposed) {
            return;
        }
        if (!save.keys.length) {
            record.failedStructure ||= failed;
        } else {
            save.keys.forEach(key => {
                if ((record.results.get(key)?.version || 0) < version) {
                    record.results.set(key, {version, failed});
                }
            });
        }
    });
    record.pending.add(pending);
    void pending.then(() => record.pending.delete(pending));
};

// 仅由需要确认保存结果后才能销毁的编辑器启用，不改变普通请求的错误处理。
export const registerEditorSave = (protyle: IProtyle) => {
    const record: EditorSaveRecord = {
        pending: new Set(), results: new Map(), version: 0, failedStructure: false, disposed: false,
    };
    const records = editors.get(protyle) || new Set<EditorSaveRecord>();
    records.add(record);
    editors.set(protyle, records);
    activeSaves.forEach(save => {
        if (matchesEditor(save, protyle)) {
            observeSave(record, save);
        }
    });
    return {
        flush: async () => {
            while (!record.disposed && record.pending.size) {
                await Promise.all([...record.pending]);
            }
            if (!record.disposed && (record.failedStructure || [...record.results.values()].some(result => result.failed))) {
                throw new Error(window.siyuan.languages.settingsPendingSaveError);
            }
        },
        dispose: () => {
            record.disposed = true;
            record.pending.clear();
            record.results.clear();
            record.failedStructure = false;
            records.delete(record);
            if (!records.size) {
                editors.delete(protyle);
            }
        },
    };
};

export const trackEditorSaveRequest = <T>(url: string, data: any, promise: Promise<T>): Promise<T> => {
    if (url !== "/api/transactions" && url !== "/api/filetree/renameDoc") {
        return promise;
    }
    const transactions = url === "/api/transactions";
    // 只保存目标 ID 和结果，不保留块 HTML、标题或可重放的请求数据。
    const transactionList = transactions && Array.isArray(data?.transactions) ? data.transactions : [];
    const operations = transactionList.flatMap((item: {doOperations?: IOperation[]}) =>
        Array.isArray(item?.doOperations) ? item.doOperations : []);
    const keys: string[] = transactions ? operations.length && operations.every((item: IOperation) =>
        item?.action === "update" && item.id && typeof item.data === "string") ?
        [...new Set<string>(operations.map((item: IOperation) => `block:${item.id}`))] : [] : ["title"];
    const transactionCount = transactionList.length;
    const save: ActiveEditorSave = {
        session: transactions ? data?.session || "" : undefined,
        notebook: transactions ? undefined : data?.notebook,
        path: transactions ? undefined : data?.path,
        keys,
        result: promise.then((response: any) => response?.code !== 0 || (transactions && (!transactionCount ||
            !Array.isArray(response.data) || response.data.length !== transactionCount ||
            response.data.some((item: {doOperations?: IOperation[]}) => !Array.isArray(item?.doOperations)))), () => true),
    };
    activeSaves.add(save);
    void save.result.then(() => activeSaves.delete(save));
    editors.forEach((records, protyle) => {
        if (matchesEditor(save, protyle)) {
            records.forEach(record => observeSave(record, save));
        }
    });
    return promise;
};
