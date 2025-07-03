
import {defineStore} from "pinia";

import {useBaseStore} from "@/stores/base.ts";
import {computed} from "vue";


interface LiveLogState {
    isOn: boolean; // is live log on
    start: number; // last request start
    end: number; // last request end
    sql: string | null; // last request sql
    isPause: boolean; // is live log paused or not
}

export const useLiveLogStore = defineStore("liveLog", () => {

    const state = useBaseStore<LiveLogState>({
        isOn: false,
        start: Date.now(),
        end: Date.now(),
        sql: null,
        isPause: false,
    });



    // Computed properties
    const isOn = computed(() => state.data.value.isOn);
    const start = computed(() => state.data.value.start);
    const end = computed(() => state.data.value.end);
    const sql = computed(() => state.data.value.sql);
    const isPause  = computed(() => state.data.value.isPause);

    // Set data
    function setIsOn(isOn: boolean) {
        state.data.value.isOn = isOn;
    }
    function setStart(start: number) {
        state.data.value.start = start;
    }
    function setEnd(end: number) {
        state.data.value.end = end;
    }
    function setSql(sql: string | null) {
        state.data.value.sql = sql;
    }
    function setIsPause(isPause: boolean) {
        state.data.value.isPause = isPause;
    }

    return {
        isOn,
        start,
        end,
        sql,
        isPause,
        setIsOn,
        setStart,
        setEnd,
        setSql,
        setIsPause
    };
});