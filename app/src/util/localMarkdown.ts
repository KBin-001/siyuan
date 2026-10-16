import type {LocalMarkdownData, LocalMarkdownRequestInput} from "../types/api";
import {fetchSyncPost} from "./fetch";

export const localMarkdownRequest = async (request: LocalMarkdownRequestInput, signal?: AbortSignal): Promise<LocalMarkdownData> => {
    const response = await fetchSyncPost("/api/export/localMarkdown", request, undefined, false, signal);
    if (!response || response.code !== 0 || !response.data) {
        throw new Error(response?.msg || window.siyuan.languages.exportFileSaveFailed);
    }
    return response.data;
};
