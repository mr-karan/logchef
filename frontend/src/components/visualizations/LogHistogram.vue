<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue';
const isMounted = ref(true);
import { toCalendarDateTime, CalendarDateTime } from '@internationalized/date';
import * as echarts from 'echarts';
import { debounce } from 'lodash-es';
import { useExploreStore } from '@/stores/explore';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { getLocalTimeZone } from '@internationalized/date';
import type { DateValue } from '@internationalized/date';
import { HistogramService, type HistogramData } from '@/services/HistogramService';
import { useQuery } from '@/composables/useQuery';

interface Props {
    timeRange?: {
        start: DateValue | any;
        end: DateValue | any;
    };
    isLoading?: boolean;
    height?: string;
}

// Define props with defaults
const props = withDefaults(defineProps<Props>(), {
    height: '180px',
    isLoading: false,
});

const emit = defineEmits<{
    (e: 'zoom-time-range', range: { start: Date, end: Date }): void;
    (e: 'update:timeRange', range: { start: DateValue, end: DateValue }): void;
}>();

// Component state
const chartRef = ref<HTMLElement | null>(null);
let chart: echarts.ECharts | null = null;
const histogramData = ref<HistogramData[]>([]);
const isChartLoading = ref(false);
const initialDataLoaded = ref(false);
const lastProcessedTimestamp = ref<number | null>(null);

// Access stores
const exploreStore = useExploreStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();

// Use the same query composable that the main log query uses
const {
    logchefQuery,
    sqlQuery,
    activeMode,
    prepareQueryForExecution
} = useQuery();

// Computed properties
const hasValidSource = computed(() => sourcesStore.hasValidCurrentSource);
const currentSourceId = computed(() => exploreStore.sourceId);
const currentTeamId = computed(() => teamsStore.currentTeamId);

// Window resize handler with debounce
const windowResizeEventCallback = debounce(async () => {
    try {
        await nextTick();
        chart?.resize();
    } catch (e) {
        console.error('Error during chart resize:', e);
    }
}, 100);

// Convert the histogram data to chart options
const convertHistogramData = (buckets: HistogramData[]) => {
    if (!buckets || buckets.length === 0) {
        return {
            title: {
                text: 'Log Distribution',
                left: 'center',
                textStyle: {
                    fontSize: 12,
                    fontWeight: 'normal'
                }
            },
            backgroundColor: 'transparent',
            grid: {
                containLabel: true,
                left: 20,
                right: 20,
                bottom: 20,
                top: 30
            },
            xAxis: {
                type: 'category',
                data: [],
                silent: false,
                splitLine: { show: false }
            },
            yAxis: {
                type: 'value',
                axisLine: { show: true },
                splitLine: {
                    show: true,
                    lineStyle: { type: 'dashed' }
                }
            },
            series: [{
                type: 'bar',
                data: [],
                itemStyle: {
                    color: '#7A80C2'
                }
            }]
        };
    }

    // Format data for echart
    const categoryData: string[] = [];
    const valueData: number[] = [];
    const timestamps: number[] = []; // Store actual timestamps for calculation

    // Format the dates for x-axis
    buckets.forEach(item => {
        const date = new Date(item.bucket);
        timestamps.push(date.getTime());
        const formatDate = `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`;
        categoryData.push(formatDate);
        valueData.push(item.log_count);
    });

    // Handle single data point case
    if (categoryData.length === 1) {
        const date = new Date(buckets[0].bucket);

        // Add points before and after
        const oneMinBefore = new Date(date.getTime() - 60000);
        const oneMinAfter = new Date(date.getTime() + 60000);

        categoryData.unshift(`${oneMinBefore.toLocaleDateString()} ${oneMinBefore.toLocaleTimeString()}`);
        categoryData.push(`${oneMinAfter.toLocaleDateString()} ${oneMinAfter.toLocaleTimeString()}`);

        valueData.unshift(0);
        valueData.push(0);

        timestamps.unshift(oneMinBefore.getTime());
        timestamps.push(oneMinAfter.getTime());
    }

    return {
        title: {
            text: `${buckets.length.toLocaleString()} Log Records`,
            left: 'center',
            textStyle: {
                fontSize: 12,
                fontWeight: 'normal'
            }
        },
        backgroundColor: 'transparent',
        grid: {
            containLabel: true,
            left: 20,
            right: 20,
            bottom: 20,
            top: 30
        },
        tooltip: {
            show: true,
            trigger: 'axis',
            textStyle: {
                fontSize: 12
            },
            axisPointer: {
                type: 'shadow'
            },
            formatter: function (params: any) {
                // Add null checks for DOM element existence
                if (!params || !params[0] || params[0].dataIndex === undefined) return '';

                const index = params[0].dataIndex;
                // Add bounds checking for data arrays
                if (index < 0 || index >= valueData.length) return '';

                // Check for padding points
                if (valueData.length === 3 && buckets.length === 1) {
                    if (index === 0 || index === 2) return '';
                }

                const value = params[0].data;
                const timeStr = params[0].axisValue;

                // Return simple text instead of HTML to avoid DOM manipulation issues
                return `Time: ${timeStr}\nLog Count: ${value.toLocaleString()}`;
            }
        },
        xAxis: {
            type: 'category',
            data: categoryData,
            silent: false,
            axisLabel: {
                interval: function (index: number) {
                    // Show fewer labels when there are many data points
                    return index % Math.max(Math.ceil(categoryData.length / 15), 1) === 0;
                },
                formatter: function (value: string) {
                    // Only show time part in the labels for cleaner appearance
                    return value.split(' ')[1];
                },
                fontSize: 10
            },
            splitLine: { show: false }
        },
        yAxis: {
            type: 'value',
            axisLine: { show: true },
            axisPointer: {
                label: { precision: 0 }
            },
            splitLine: {
                show: true,
                lineStyle: { type: 'dashed' }
            },
            axisLabel: {
                formatter: (value: number) => Math.round(value).toLocaleString()
            }
        },
        toolbox: {
            orient: 'horizontal',
            show: true,
            showTitle: true,
            itemSize: 15,
            right: 15,
            top: 5,
            feature: {
                dataZoom: {
                    show: true,
                    yAxisIndex: 'none',
                    title: {
                        zoom: 'Area Zoom',
                        back: 'Reset Zoom'
                    }
                },
                saveAsImage: {
                    show: true,
                    title: 'Save',
                    pixelRatio: 2
                }
            }
        },
        dataZoom: [
            {
                type: 'inside',
                xAxisIndex: 0,
                start: 0,
                end: 100
            }
        ],
        series: [
            {
                name: 'Log Count',
                type: 'bar',
                barMaxWidth: 40,
                data: valueData,
                large: true,
                largeThreshold: 100,
                itemStyle: {
                    color: '#7A80C2',
                    borderRadius: [2, 2, 0, 0]
                },
                emphasis: {
                    itemStyle: {
                        color: '#5b63b7'
                    }
                }
            }
        ],
        animation: false
    };
};

// Debounced fetch function to prevent multiple simultaneous calls
const debouncedFetchHistogramData = debounce(async (forceGranularity?: string) => {
    if (isChartLoading.value) return; // Skip if already loading
    await fetchHistogramData(forceGranularity);
}, 200);

// Fetch histogram data from the backend
const fetchHistogramData = async (forceGranularity?: string) => {
    if (!isMounted.value || !hasValidSource.value || !currentSourceId.value || !currentTeamId.value) {
        histogramData.value = [];
        return;
    }

    try {
        isChartLoading.value = true;

        // Use the same query preparation logic as the main log query
        const queryResult = prepareQueryForExecution();

        if (!queryResult.success) {
            console.error("Failed to prepare query for histogram:", queryResult.error);
            histogramData.value = [];
            return;
        }

        // Call the histogram service with the properly prepared SQL
        const response = await HistogramService.fetchHistogramData({
            sourceId: currentSourceId.value,
            teamId: currentTeamId.value,
            timeRange: {
                start: props.timeRange?.start
                    ? (typeof props.timeRange.start.toDate === 'function'
                        ? new Date(props.timeRange.start.toDate(getLocalTimeZone()).getTime()).toISOString()
                        : new Date().toISOString())
                    : new Date(Date.now() - 3600000).toISOString(),
                end: props.timeRange?.end
                    ? (typeof props.timeRange.end.toDate === 'function'
                        ? new Date(props.timeRange.end.toDate(getLocalTimeZone()).getTime()).toISOString()
                        : new Date().toISOString())
                    : new Date().toISOString(),
            },
            query: queryResult.sql,
            queryType: activeMode.value,
            granularity: forceGranularity,
        });

        if (response.success && response.data) {
            histogramData.value = response.data.data || [];
            initialDataLoaded.value = true;
        } else {
            histogramData.value = [];
        }

    } catch (error) {
        console.error('Error fetching histogram data:', error);
        histogramData.value = [];
    } finally {
        isChartLoading.value = false;
    }
};

// Reset and set the global cursor for datazoom
const restoreChart = () => {
    chart?.dispatchAction({
        type: "restore"
    });

    // Set toolbox datazoom button state
    chart?.dispatchAction({
        type: "takeGlobalCursor",
        key: "dataZoomSelect",
        dataZoomSelectActive: true
    });
};

// Setup chart event handlers
const setupChartEvents = () => {
    if (!chart) return;

    // Create safe handler wrapper to check component is still mounted
    const safeHandler = (handler: Function) => {
        return (...args: any[]) => {
            if (!isMounted.value || !chart) return;
            handler(...args);
        };
    };

    // Handle chart zoom events
    chart.on('datazoom', safeHandler((params: any) => {
        // Either handle batch or direct parameters
        if (params?.batch && params.batch.length > 0) {
            // For the inside zoom with batch
            const batch = params.batch[0];
            handleZoomAction(batch);
        } else if (params?.startValue !== undefined && params?.endValue !== undefined) {
            // For direct toolbox dataZoom
            handleZoomAction(params);
        }
    }));

    // Restore event
    chart.on('restore', safeHandler(() => {
        try {
            // Reset to full time range
            if (props.timeRange?.start && props.timeRange?.end) {
                const startDate = props.timeRange.start.toDate(getLocalTimeZone());
                const endDate = props.timeRange.end.toDate(getLocalTimeZone());

                // Emit native Date event (original behavior)
                emit('zoom-time-range', {
                    start: startDate,
                    end: endDate
                });

                // No need to emit update:timeRange here since we're restoring to the original timeRange
            }
        } catch (e) {
            console.error('Error handling restore event:', e);
        }
    }));

    // Handle window resize
    window.addEventListener('resize', windowResizeEventCallback);
};

// Initialize chart
const initChart = async () => {
    if (!chartRef.value || !isMounted.value) return;

    try {
        // Wait for DOM to be ready
        await nextTick();

        // Clear any existing chart instance
        if (chart) {
            chart.dispose();
        }

        // Create chart instance
        chart = echarts.init(chartRef.value);

        // Set initial options
        updateChartOptions();

        // Setup event handlers
        setupChartEvents();

        // Ensure the dataZoom button is active
        restoreChart();
    } catch (e) {
        console.error('Error initializing chart:', e);
    }
};

// Update chart with new options
const updateChartOptions = () => {
    if (!chart) return;

    try {
        // Get options based on data
        const options = convertHistogramData(histogramData.value);

        // Set options
        chart.setOption(options, true);

        // Update loading state
        if (isChartLoading.value) {
            chart.showLoading({
                text: 'Loading data...',
                maskColor: 'rgba(255, 255, 255, 0.8)',
                fontSize: 14
            });
        } else {
            chart.hideLoading();
        }

        // Make sure dataZoom tool is selected
        chart.dispatchAction({
            type: "takeGlobalCursor",
            key: "dataZoomSelect",
            dataZoomSelectActive: true
        });
    } catch (e) {
        console.error('Error updating chart options:', e);
    }
};

// Watch for changes that should trigger chart updates
watch(() => [props.isLoading, histogramData.value], () => {
    isChartLoading.value = props.isLoading || false;
    updateChartOptions();
}, { deep: true });

// Watch for changes that should trigger data reload - but ONLY react to lastExecutionTimestamp changes
watch(
    () => exploreStore.lastExecutionTimestamp,
    (newTimestamp, oldTimestamp) => {
        // Skip if we're already loading
        if (isChartLoading.value) return;

        // Handle undefined/null timestamps
        const safeNewTimestamp = newTimestamp || null;
        const safeOldTimestamp = oldTimestamp || null;

        // Skip if timestamp hasn't changed or if it's the same one we already processed
        if (safeNewTimestamp === safeOldTimestamp || safeNewTimestamp === lastProcessedTimestamp.value) return;

        // Update the last processed timestamp
        lastProcessedTimestamp.value = safeNewTimestamp;

        // Fetch new data if we have a valid source (this should happen when query is executed)
        if (hasValidSource.value && currentSourceId.value) {
            console.log('Histogram fetching data due to lastExecutionTimestamp change:', safeNewTimestamp);
            debouncedFetchHistogramData();
        } else if (currentSourceId.value) {
            console.warn('Skipping histogram data fetch for disconnected source');
            histogramData.value = [];
        }
    },
    { immediate: true } // Add immediate option to ensure it runs on component mount
);

// Add a separate deep watcher specifically for time range changes
watch(
    () => props.timeRange,
    (newRange, oldRange) => {
        // Never fetch histogram data when time range changes
        // Only clear data if completely outside current range

        // Skip if no valid source
        if (!hasValidSource.value || !currentSourceId.value) return;

        // Only trigger if time range actually changed
        if (newRange && oldRange &&
            (newRange.start?.toString() !== oldRange.start?.toString() ||
                newRange.end?.toString() !== oldRange.end?.toString())) {

            // Only clear data if this is a completely different time range
            // This prevents showing outdated data
            if (histogramData.value.length > 0) {
                const firstBucketTime = new Date(histogramData.value[0].bucket).getTime();
                const lastBucketTime = new Date(histogramData.value[histogramData.value.length - 1].bucket).getTime();

                // Convert new range to timestamps for comparison
                const newStartTime = newRange.start?.toDate ?
                    newRange.start.toDate(getLocalTimeZone()).getTime() : 0;
                const newEndTime = newRange.end?.toDate ?
                    newRange.end.toDate(getLocalTimeZone()).getTime() : 0;

                // Check if new range is completely outside the current data range
                const isDisjoint = newEndTime < firstBucketTime || newStartTime > lastBucketTime;

                if (isDisjoint) {
                    // Clear histogram data as it's completely irrelevant for the new range
                    histogramData.value = [];
                }
            }
        }
    },
    { deep: true }
);

// Watch for height changes to resize chart
watch(() => props.height, async () => {
    try {
        await nextTick();
        chart?.resize();
    } catch (e) {
        console.error('Error resizing chart:', e);
    }
});

// Add a separate watcher for source changes - we should refresh data when source changes
watch(
    () => currentSourceId.value,
    (newSourceId, oldSourceId) => {
        // Skip if source hasn't changed
        if (newSourceId === oldSourceId) return;

        // If we have a valid source and an execution has already happened,
        // fetch histogram data for the new source
        if (newSourceId && hasValidSource.value && exploreStore.lastExecutionTimestamp) {
            console.log('Histogram source changed, fetching data for new source');
            lastProcessedTimestamp.value = exploreStore.lastExecutionTimestamp;
            debouncedFetchHistogramData();
        }
    }
);

// Helper function to convert native Date to CalendarDateTime and emit update
function updateTimeRangeForDatePicker(startDate: Date, endDate: Date) {
    // Convert native Date objects to CalendarDateTime objects
    const startDateTime = new CalendarDateTime(
        startDate.getFullYear(),
        startDate.getMonth() + 1,
        startDate.getDate(),
        startDate.getHours(),
        startDate.getMinutes(),
        startDate.getSeconds()
    );

    const endDateTime = new CalendarDateTime(
        endDate.getFullYear(),
        endDate.getMonth() + 1,
        endDate.getDate(),
        endDate.getHours(),
        endDate.getMinutes(),
        endDate.getSeconds()
    );

    // Emit event to update the DateTimePicker
    emit('update:timeRange', {
        start: startDateTime,
        end: endDateTime
    });

    console.log('Emitted updated time range for DateTimePicker:', {
        start: startDateTime,
        end: endDateTime
    });
}

// Helper function to handle zoom action uniformly
function handleZoomAction(zoomParams: any) {
    if (histogramData.value.length === 0) return;

    try {
        console.log('DataZoom triggered with params:', zoomParams, 'Histogram data length:', histogramData.value.length);

        // Special case: Very small datasets (1-3 points)
        if (histogramData.value.length <= 3) {
            // Just use the original time range from the data
            const startDate = new Date(histogramData.value[0].bucket);

            // For the end date, use the last bucket plus a small increment
            const lastIndex = histogramData.value.length - 1;
            const lastBucketTime = new Date(histogramData.value[lastIndex].bucket).getTime();
            // Add 1 minute if only one bucket, or use bucket width if more than one
            const increment = lastIndex > 0
                ? (lastBucketTime - new Date(histogramData.value[0].bucket).getTime())
                : 60000; // 1 minute default

            const endDate = new Date(lastBucketTime + increment);

            console.log('Small dataset zoom: Using full range', { startDate, endDate });

            // Emit native Date event
            emit('zoom-time-range', { start: startDate, end: endDate });

            // Also emit DateValue event for DateTimePicker
            updateTimeRangeForDatePicker(startDate, endDate);

            return;
        }

        const totalPoints = histogramData.value.length;
        let startIndex: number;
        let endIndex: number;

        // Handle different zoom parameter formats
        if (zoomParams.startValue !== undefined && zoomParams.endValue !== undefined) {
            // Direct index values from toolbox dataZoom
            startIndex = Math.max(0, Math.min(parseInt(zoomParams.startValue), totalPoints - 1));
            endIndex = Math.max(startIndex, Math.min(parseInt(zoomParams.endValue), totalPoints - 1));

            console.log('Using direct index values:', { startIndex, endIndex });
        } else if (zoomParams.start !== undefined && zoomParams.end !== undefined) {
            // Percentage values from inside zoom
            const startPercent = zoomParams.start || 0;
            const endPercent = zoomParams.end || 100;

            // Calculate indices based on percentages
            startIndex = Math.floor(totalPoints * startPercent / 100);
            endIndex = Math.ceil(totalPoints * endPercent / 100) - 1;

            console.log('Using percentage values:', { startPercent, endPercent, startIndex, endIndex });
        } else {
            console.error('Invalid zoom parameters:', zoomParams);
            return;
        }

        // Ensure indices are within bounds
        const validStartIndex = Math.max(0, Math.min(startIndex, totalPoints - 1));
        const validEndIndex = Math.max(validStartIndex, Math.min(endIndex, totalPoints - 1));

        // Get the actual timestamps from the data
        const startDate = new Date(histogramData.value[validStartIndex].bucket);

        // For the end date, we need to include the entire bucket
        // Calculate the bucket width (assume uniform width)
        let endDate: Date;

        if (validEndIndex >= totalPoints - 1) {
            // If at the end of the data, add a bucket width to include the entire last bucket
            const lastBucketTime = new Date(histogramData.value[validEndIndex].bucket).getTime();

            // Calculate bucket width if possible
            let bucketWidth = 60000; // Default 1 minute
            if (totalPoints > 1) {
                // Get average bucket width
                const firstTime = new Date(histogramData.value[0].bucket).getTime();
                const lastTime = new Date(histogramData.value[totalPoints - 1].bucket).getTime();
                bucketWidth = (lastTime - firstTime) / (totalPoints - 1);
            }

            endDate = new Date(lastBucketTime + bucketWidth);
        } else {
            // Use next bucket's start time as the end time
            endDate = new Date(histogramData.value[validEndIndex + 1].bucket);
        }

        console.log('Selected time range:', {
            startDate,
            endDate,
            startIndex: validStartIndex,
            endIndex: validEndIndex
        });

        // Emit native Date event (original behavior)
        emit('zoom-time-range', {
            start: startDate,
            end: endDate
        });

        // Also emit DateValue event for DateTimePicker
        updateTimeRangeForDatePicker(startDate, endDate);
    } catch (e) {
        console.error('Error handling zoom event:', e);

        // Fallback to using the full range
        if (histogramData.value.length > 0) {
            try {
                const startDate = new Date(histogramData.value[0].bucket);
                const lastBucket = histogramData.value[histogramData.value.length - 1].bucket;
                const endDate = new Date(new Date(lastBucket).getTime() + 60000);

                console.log('Fallback: Using full time range after error');

                // Emit native Date event (original behavior)
                emit('zoom-time-range', { start: startDate, end: endDate });

                // Also emit DateValue event for DateTimePicker
                updateTimeRangeForDatePicker(startDate, endDate);
            } catch (fallbackError) {
                console.error('Even fallback failed:', fallbackError);
            }
        }
    }
}

// Component lifecycle
onMounted(async () => {
    isMounted.value = true;

    // Wait for multiple ticks to ensure DOM is fully rendered
    for (let i = 0; i < 5; i++) {
        await nextTick();
    }

    // Initialize chart
    await initChart();

    // Load data if:
    // 1. We have a valid source
    // 2. A query has already been executed (lastExecutionTimestamp exists)
    if (hasValidSource.value && currentSourceId.value && exploreStore.lastExecutionTimestamp) {
        console.log('Histogram loading initial data on mount');
        lastProcessedTimestamp.value = exploreStore.lastExecutionTimestamp;
        debouncedFetchHistogramData();
    }
});

onBeforeUnmount(() => {
    // Mark component as unmounted
    isMounted.value = false;

    // Clean up resources
    window.removeEventListener('resize', windowResizeEventCallback);

    if (chart) {
        // Remove event listeners first
        chart.off('datazoom');
        chart.off('restore');
        // Then dispose the chart
        chart.dispose();
        chart = null;
    }
});
</script>

<template>
    <div class="log-histogram">
        <div v-if="!initialDataLoaded && isChartLoading" class="histogram-loading-overlay">
            <div class="loading-spinner"></div>
            <span>Loading histogram data...</span>
        </div>
        <div v-else-if="!histogramData.length && !isChartLoading" class="histogram-empty-state">
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mb-2">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <line x1="9" y1="9" x2="9" y2="15"></line>
                <line x1="15" y1="9" x2="15" y2="15"></line>
            </svg>
            <span>No histogram data available</span>
        </div>
        <div ref="chartRef" class="chart-container" :style="{ height }"></div>
    </div>
</template>

<style scoped>
.log-histogram {
    position: relative;
    width: 100%;
    border-radius: 0.375rem;
    background-color: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
    overflow: hidden;
    margin-bottom: 0.5rem;
}

.chart-container {
    width: 100%;
    min-height: 150px;
    padding: 0.25rem;
}

.histogram-loading-overlay {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background-color: rgba(255, 255, 255, 0.85);
    z-index: 10;
    gap: 0.5rem;
    font-size: 0.875rem;
    color: hsl(var(--muted-foreground));
}

.loading-spinner {
    width: 1.5rem;
    height: 1.5rem;
    border: 2px solid hsl(var(--muted));
    border-top-color: hsl(var(--primary));
    border-radius: 50%;
    animation: spinner 0.8s linear infinite;
}

.histogram-empty-state {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: hsl(var(--muted-foreground));
    font-size: 0.875rem;
    background-color: hsl(var(--card));
}

@keyframes spinner {
    to {
        transform: rotate(360deg);
    }
}
</style>
