import { showErrorToast } from "@/api/error-handler";
import { useExploreStore } from "@/stores/explore";
import { useTeamsStore } from "@/stores/teams";
import type { TimeRange } from '@/types/query';
import { useQuery } from "@/composables/useQuery";
import { fromDate } from '@internationalized/date';
import {SqlManager} from "@/services/SqlManager";
import { useSourcesStore } from "@/stores/sources";
import { useLiveLogStore } from "@/stores/liveLog"
import type {
    QuerySuccessResponse,
} from "@/api/explore";

let socket: WebSocket | null = null;
let logRequestIntervalId: ReturnType<typeof setInterval> | null = null;
const longPoolingTimeOut = 3000; // how frequently client request log to server
const query = useQuery();
const exploreStore = useExploreStore();
const sourcesStore = useSourcesStore();
const liveLogStore = useLiveLogStore();

export function connectWebSocket(path: string): WebSocket | null {

    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        return socket;
    }

    if (path == 'log') {
        // Get current team ID
        const teamID = useTeamsStore().currentTeamId;
        if (!teamID) {
            return null;
        }
        const sourceID = useExploreStore().sourceId;
        if (!sourceID) {
            return null;
        }
        socket = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/teams/${teamID}/sources/${sourceID}/ws/log`);
    } else {
        return null;
    }

    socket.addEventListener("open", () => {
        console.info("[WebSocket] Connected");
        // send log request every 3 seconds (long polling)
        // -> do not receive from server, it is requested by client.
        // -> to reduce duplicated http connections, we use websocket for one time connection
        if (!logRequestIntervalId) {
            logRequestIntervalId = setInterval(() => {
                if (!liveLogStore.isPause) {
                    runLiveLogQuery();
                }
            }, longPoolingTimeOut);
        }
    });

    socket.addEventListener("close", (event) => {
        console.warn(`[WebSocket] Closed: code=${event.code}, reason=${event.reason}`);

        // remove interval for long polling when connection closed
        if (logRequestIntervalId) {
            clearInterval(logRequestIntervalId);
            logRequestIntervalId = null;
        }

        if (event.code === 1006) {
            showErrorToast({
                status: "error",
                message: "Lost connection to the server.",
                error_type: "WebSocketDisconnected"
            });
        }
    });

    socket.addEventListener("error", (error) => {
        console.error("[WebSocket] Error", error);
        showErrorToast({
            status: "error",
            message: "WebSocket encountered an error.",
            error_type: "WebSocketError"
        });
    });

    socket.addEventListener("message", (e) => {
        let parsedData: any;

        try {
            parsedData = JSON.parse(e.data); // string → object
        } catch (err) {
            console.warn("Failed to parse message as JSON", err);
            return;
        }
        // if return from request log
        if (isQuerySuccessResponse(parsedData)) {
            useExploreStore().fetchLogData(parsedData);
        }
        // handle for other messages for other request

    });

    return socket;
}

export function getWebSocket(): WebSocket | null {
    return socket;
}

export function sendMessage(data: any | null): void {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
        console.warn("[WebSocket] Cannot send message: socket not open");
        return;
    }

    const payload = typeof data === "string" ? data : JSON.stringify(data);
    socket.send(payload);
}

export function closeWebSocket(): void {
    if (logRequestIntervalId) {
        clearInterval(logRequestIntervalId);
        logRequestIntervalId = null;
    }

    if (socket) {
        socket.close();
        socket = null;
    }
}

export function isQuerySuccessResponse(data: any): data is QuerySuccessResponse {
    return (
        data !== null &&
        typeof data === 'object' &&
        'logs' in data
    );
}

export function runLiveLogQuery() {


    // Make sure query is valid before execution
    const result = query.prepareQueryForExecution();

    if (!result.success) {
        throw new Error(result.error || 'Failed to prepare query for execution');
    }

    if (!exploreStore.lastExecutedState) return;
    let sql = exploreStore.sqlForExecution;
    if (!sql || !sql.trim()) {
        console.log("No SQL provided, generating default query");
        const sourceDetails = sourcesStore.currentSourceDetails;
        if (!sourceDetails) {
            throw new Error("Source details not available");
        }
        const tsField = sourceDetails._meta_ts_field || 'timestamp';
        const tableName = sourcesStore.getCurrentSourceTableName || 'default.logs';

        // Generate default SQL using SqlManager
        const result = SqlManager.generateDefaultSql({
            tableName,
            tsField,
            timeRange: exploreStore.timeRange as TimeRange,
            limit: exploreStore.limit,
            timezone: exploreStore.selectedTimezoneIdentifier || undefined
        });

        if (!result.success) {

        }

        sql = result.sql;
    }

    const timezone = exploreStore.selectedTimezoneIdentifier || exploreStore.getTimezoneIdentifier();
    const now = new Date();
    const exploreExecutionTimestamp = exploreStore.lastExecutionTimestamp
        ? exploreStore.lastExecutionTimestamp
        : now.getTime() - longPoolingTimeOut;
    const lastExecutionTimestamp = liveLogStore.end ? liveLogStore.end : exploreExecutionTimestamp;
    const startTime = new Date(lastExecutionTimestamp);
    const endTime = now;
    const range: TimeRange = {
        start: fromDate(startTime, timezone), // number → Date → DateValue
        end: fromDate(endTime, timezone), // 현재 시간
    };

    let updatedSql;
    try {
        const sourceDetails = sourcesStore.currentSourceDetails;
        if (!sourceDetails) {
            throw new Error('No source selected');
        }

        // Use SqlManager to update time range in SQL
        updatedSql = SqlManager.updateTimeRange({
            sql: sql!,
            tsField: sourceDetails._meta_ts_field || 'timestamp',
            timeRange: range,
            timezone: exploreStore.selectedTimezoneIdentifier || undefined
        });

        // Update SQL if it was changed
        if (updatedSql !== sql) {
            exploreStore.setRawSql(updatedSql);
            console.log("TimeRangeSelector: SQL updated for new time range");
        }
    } catch (err) {
        console.error("Error updating SQL for time range:", err);
    }
    if (updatedSql) {
        // update live-log store
        const params = exploreStore.prepareQueryParams(updatedSql);
        sendMessage(params);
        liveLogStore.setStart(startTime.getTime());
        liveLogStore.setEnd(endTime.getTime());
        if (sql) {
            liveLogStore.setSql(sql);
        }
    }
}