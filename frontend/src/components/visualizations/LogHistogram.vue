<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue';
import { useColorMode } from '@vueuse/core';
const isMounted = ref(true);
import { toCalendarDateTime, CalendarDateTime } from '@internationalized/date';
// Tree-shaking echarts imports
import { init, use, registerTheme } from 'echarts/core'; // Import registerTheme
import type { ECharts } from 'echarts/core';
import { BarChart } from 'echarts/charts';
import {
    TitleComponent,
    TooltipComponent,
    GridComponent,
    DataZoomComponent,
    ToolboxComponent,
    LegendComponent
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';

// Register necessary components
use([
    BarChart,
    TitleComponent,
    TooltipComponent,
    GridComponent,
    DataZoomComponent,
    ToolboxComponent,
    LegendComponent,
    CanvasRenderer
]);

// Import the dark theme
import { logchefDarkTheme } from '@/utils/echarts-theme-dark';

// Register the dark theme only once
let themeRegistered = false;
if (!themeRegistered) {
    registerTheme('logchef-dark', logchefDarkTheme);
    themeRegistered = true;
    console.log("LogChef dark theme registered for ECharts.");
}


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
    groupBy?: string;
}

// Define props with defaults
const props = withDefaults(defineProps<Props>(), {
    height: '180px',
    isLoading: false,
    groupBy: '',
});

const emit = defineEmits<{
    (e: 'zoom-time-range', range: { start: Date, end: Date }): void;
    (e: 'update:timeRange', range: { start: DateValue, end: DateValue }): void;
}>();

// Component state
const chartRef = ref<HTMLElement | null>(null);
let chart: ECharts | null = null;
const histogramData = ref<HistogramData[]>([]);
const isChartLoading = ref(false);
const initialDataLoaded = ref(false);
const lastProcessedTimestamp = ref<number | null>(null);
const currentGroupBy = ref<string>('');
const currentGranularity = ref<string>(''); // Store the granularity used

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

// Theme state
const colorMode = useColorMode(); // Gets the resolved mode ('light' or 'dark')

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

// Revised color palette for a more professional/dev-tool look
const colorPalette = [
    '#5470C6', // Blue
    '#EE6666', // Red
    '#FAC858', // Yellow
    '#91CC75', // Green
    '#73C0DE', // Light Blue
    '#FC8452', // Orange
    '#9A60B4', // Purple
    '#ea7ccc', // Pink (more muted)
    // Extended colors if needed
    '#3BA272', // Darker Green
    '#27727B', // Teal Blue
    '#E062AE', // Magenta
    '#FFB980', // Light Orange
    '#5D9B9B', // Grayish Cyan
    '#D48265', // Brownish Orange
    '#C6E579', // Lime Green (more muted)
    '#F4E001', // Bright Yellow (use sparingly)
    '#B5C334', // Olive Green
    '#6E7074', // Gray
    '#8378EA', // Lavender
    '#7A455D'  // Maroon
];


// Convert the histogram data to chart options with Kibana-like styling
const convertHistogramData = (buckets: HistogramData[]) => {
    if (!buckets || buckets.length === 0) {
        return {
            title: {
                text: 'Log Distribution',
                left: 'center',
                textStyle: {
                    fontSize: 14,
                    fontWeight: '500',
                }
            },
            backgroundColor: 'transparent',
            grid: {
                containLabel: true,
                left: 25,
                right: 25,
                bottom: 20,
                top: 35
            },
            xAxis: {
                type: 'category',
                data: [],
                silent: false,
                splitLine: { show: false },
                axisLine: {
                    lineStyle: {
                        color: colorMode.value === 'dark' ? '#71708A' : 'hsl(var(--border))'
                    }
                },
                axisTick: {
                    lineStyle: {
                        color: colorMode.value === 'dark' ? '#71708A' : 'hsl(var(--border))'
                    }
                }
            },
            yAxis: {
                type: 'value',
                axisLine: { show: false },
                splitLine: {
                    show: true,
                    lineStyle: {
                        type: 'dashed',
                        color: colorMode.value === 'dark' ? 'rgba(120, 120, 140, 0.3)' : 'hsl(var(--border))',
                        opacity: 0.5
                    }
                },
                axisTick: { show: false },
                axisLabel: {
                    fontSize: 11
                }
            },
            series: [{
                type: 'bar',
                data: [],
                itemStyle: {
                    color: '#5794F2'
                }
            }]
        };
    }

    // Check if data is grouped
    const isGrouped = buckets.some(item => item.group_value && item.group_value !== '');

    // Format data for echart
    const categoryData: string[] = [];
    const timestamps: number[] = []; // Store actual timestamps for calculation
    let seriesData: any[] = [];

    // --- Determine Optimal Date/Time Format ---
    // Default: HH:MM in 24-hour format
    let timeFormatOptions: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hour12: false };
    const showSeconds = currentGranularity.value.endsWith('s');
    if (showSeconds) {
        timeFormatOptions.second = '2-digit'; // Add seconds if granularity requires it (still 24-hour)
    }

    if (buckets.length > 0) {
        const firstTimestamp = new Date(buckets[0].bucket).getTime();
        const lastTimestamp = new Date(buckets[buckets.length - 1].bucket).getTime();
        const spanMs = lastTimestamp - firstTimestamp;
        const spanHours = spanMs / (1000 * 60 * 60);

        const firstDate = new Date(firstTimestamp);
        const lastDate = new Date(lastTimestamp);
        const isSameDay = firstDate.toDateString() === lastDate.toDateString();

        // If span is >= 24 hours OR data spans multiple days, include the date
        if (spanHours >= 24 || !isSameDay) {
            timeFormatOptions = {
                month: 'numeric', // MM
                day: 'numeric',   // DD
                ...timeFormatOptions // Include HH:MM or HH:MM:SS
            };
            // Optional: Add year if span is very large (e.g., > 365 days)
            // const spanDays = spanHours / 24;
            // if (spanDays > 365) {
            //     timeFormatOptions.year = 'numeric';
            // }
        }
    }
    // --- End Determine Format ---


    if (isGrouped) {
        // Group data by bucket and group_value
        const groupedByBucket: Record<string, Record<string, number>> = {};
        const uniqueGroups: string[] = [];

        buckets.forEach(item => {
            const date = new Date(item.bucket);
            const bucketKey = date.toISOString(); // Use ISO string as key for consistency
            if (!groupedByBucket[bucketKey]) {
                groupedByBucket[bucketKey] = {};
                // Use the determined intelligent format
                categoryData.push(date.toLocaleString([], timeFormatOptions));
                timestamps.push(date.getTime());
            }
            const groupVal = item.group_value || 'Other';
            if (!uniqueGroups.includes(groupVal)) {
                uniqueGroups.push(groupVal);
            }
            groupedByBucket[bucketKey][groupVal] = item.log_count;
        });

        // Create series for each group
        seriesData = uniqueGroups.map((group, index) => {
            const dataValues = Object.values(groupedByBucket).map(bucketData => bucketData[group] || 0);
            return {
                name: group,
                type: 'bar',
                stack: 'group',
                data: dataValues,
                barMaxWidth: '80%',
                barMinWidth: 2,
                large: true,
                largeThreshold: 100,
                itemStyle: {
                    color: colorPalette[index % colorPalette.length],
                    borderRadius: [2, 2, 0, 0],
                    opacity: 0.85
                },
                emphasis: {
                    itemStyle: {
                        color: colorPalette[index % colorPalette.length],
                        opacity: 1,
                        shadowBlur: 4,
                        shadowColor: 'rgba(0, 0, 0, 0.2)'
                    }
                }
            };
        });
    } else {
        const valueData: number[] = [];
        buckets.forEach(item => {
            const date = new Date(item.bucket);
            timestamps.push(date.getTime());
            // Use the determined intelligent format
            categoryData.push(date.toLocaleString([], timeFormatOptions));
            valueData.push(item.log_count);
        });

        // Handle single data point case
        if (categoryData.length === 1) {
            const date = new Date(buckets[0].bucket);

            // Add points before and after
            const oneMinBefore = new Date(date.getTime() - 60000);
            const oneMinAfter = new Date(date.getTime() + 60000);

            // Format added points consistently using the determined format
            categoryData.unshift(oneMinBefore.toLocaleString([], timeFormatOptions));
            categoryData.push(oneMinAfter.toLocaleString([], timeFormatOptions));

            valueData.unshift(0);
            valueData.push(0);

            timestamps.unshift(oneMinBefore.getTime());
            timestamps.push(oneMinAfter.getTime());
        }

        seriesData = [{
            name: 'Log Count',
            type: 'bar',
            barMaxWidth: '80%',
            barMinWidth: 2,
            data: valueData,
            large: true,
            largeThreshold: 100,
            itemStyle: {
                color: '#5794F2', // Professional-style blue for log analytics
                borderRadius: [2, 2, 0, 0],
                opacity: 0.85
            },
            emphasis: {
                itemStyle: {
                    color: '#44A2F3', // Slightly darker blue for hover/selection
                    opacity: 1,
                    shadowBlur: 4,
                    shadowColor: 'rgba(0, 0, 0, 0.2)'
                }
            }
        }];
    }

    return {
        title: {
            show: false, // Hide the title
            // text: `${buckets.length.toLocaleString()} Log Records`,
            // left: 'center',
            // textStyle: {
            //     fontSize: 14,
            //     fontWeight: '500',
            // }
        },
        backgroundColor: 'transparent',
        grid: {
            containLabel: true,
            left: 25,
            right: 25,
            bottom: 20,
            top: 35
        },
        tooltip: {
            show: true,
            trigger: 'axis',
            confine: false,
            z: 60,
            position: function (point: any, params: any, dom: any, rect: any, size: any) {
                // Position the tooltip slightly below the cursor
                // point[0] is x, point[1] is y coordinate of the mouse
                const x = point[0];
                const y = point[1];
                const viewWidth = size.viewSize[0];
                const viewHeight = size.viewSize[1];
                const boxWidth = size.contentSize[0];
                const boxHeight = size.contentSize[1];
                let posX = 0;
                let posY = 0;

                // Calculate horizontal position
                if (x + boxWidth > viewWidth) {
                    // If tooltip overflows right, position it to the left of the cursor
                    posX = x - boxWidth - 10; // 10px offset
                } else {
                    // Otherwise, position it to the right of the cursor
                    posX = x + 15; // 15px offset
                }

                // Calculate vertical position - always below the cursor
                posY = y + 20; // 20px offset below cursor

                // Prevent tooltip from going off the bottom edge
                if (posY + boxHeight > viewHeight) {
                    posY = y - boxHeight - 10; // Position above cursor if it overflows bottom
                }
                // Prevent tooltip from going off the top edge (if positioned above)
                if (posY < 0) {
                    posY = 5; // Small offset from top edge
                }
                // Prevent tooltip from going off the left edge (if positioned left)
                if (posX < 0) {
                    posX = 5; // Small offset from left edge
                }

                return [posX, posY];
            },
            backgroundColor: colorMode.value === 'dark' ? '#1c1c25' : 'hsl(var(--popover))',
            borderColor: colorMode.value === 'dark' ? '#302f3d' : 'hsl(var(--border))',
            borderWidth: 1,
            padding: 10,
            textStyle: {
                fontSize: 12,
                color: colorMode.value === 'dark' ? '#FFFFFF' : 'hsl(var(--popover-foreground))'
            },
            axisPointer: {
                type: 'shadow',
                shadowStyle: {
                    color: 'rgba(0, 0, 0, 0.1)'
                }
            },
            formatter: function (params: any) {
                // Add null checks for DOM element existence
                if (!params || !params.length || params[0].dataIndex === undefined) return '';

                const index = params[0].dataIndex;
                // Add bounds checking for data arrays
                if (index < 0 || index >= categoryData.length) return '';

                // Use the pre-formatted time string directly from categoryData
                const displayTimeStr = categoryData[index];

                let total = 0;
                const details = params.map((p: any) => {
                    const value = p.data || 0;
                    total += value;
                    return `<div style="display: flex; align-items: center; margin-bottom: 4px;">
                        <span style="display: inline-block; width: 10px; height: 10px; background-color: ${p.color}; margin-right: 6px; border-radius: 2px;"></span>
                        <span>${p.seriesName}: <strong>${value.toLocaleString()}</strong></span>
                    </div>`;
                }).join('');

                return `<div style="font-size: 12px;">
                    <div style="font-weight: 500; margin-bottom: 4px;">${displayTimeStr}</div>
                    <div style="margin-bottom: 4px;">Total: <strong>${total.toLocaleString()}</strong></div>
                    ${details}
                </div>`;
            }
        },
        legend: isGrouped ? {
            show: true,
            bottom: 0,
            textStyle: {
                fontSize: 11
            }
        } : {
            show: false
        },
        xAxis: {
            type: 'category',
            data: categoryData,
            silent: false,
            axisLabel: {
                interval: function (index: number) {
                    // Show fewer labels when there are many data points
                    // Show fewer labels when there are many data points to avoid overlap
                    // Adjust the divisor (e.g., 15) based on desired density
                    return index % Math.max(Math.ceil(categoryData.length / 15), 1) === 0;
                },
                formatter: function (value: string) {
                    // Value is already pre-formatted intelligently
                    return value;
                },
                fontSize: 11,
                margin: 10
            },
            axisLine: {
                lineStyle: {
                    color: colorMode.value === 'dark' ? '#71708A' : 'hsl(var(--border))'
                }
            },
            axisTick: {
                alignWithLabel: true,
            },
            splitLine: { show: false }
        },
        yAxis: {
            type: 'value',
            axisLine: { show: false },
            axisTick: { show: false },
            axisPointer: {
                label: { precision: 0 }
            },
            splitLine: {
                show: true,
                lineStyle: {
                    type: 'dashed',
                    color: colorMode.value === 'dark' ? 'rgba(120, 120, 140, 0.3)' : 'hsl(var(--border))',
                    opacity: 0.5
                }
            },
            axisLabel: {
                formatter: (value: number) => {
                    if (value < 1000) {
                        return Math.round(value).toLocaleString();
                    } else if (value < 1000000) {
                        return (Math.round(value / 100) / 10).toLocaleString() + 'K';
                    } else if (value < 1000000000) {
                        return (Math.round(value / 100000) / 10).toLocaleString() + 'M';
                    } else {
                        return (Math.round(value / 100000000) / 10).toLocaleString() + 'B';
                    }
                },
                fontSize: 11,
                margin: 12
            }
        },
        toolbox: {
            orient: 'horizontal',
            show: true,
            showTitle: true,
            itemSize: 14,
            right: 15,
            top: 5,
            feature: {
                dataZoom: { // Keep dataZoom functionality but hide default icons/titles
                    show: true,
                    yAxisIndex: 'none',
                    // title: { // Remove titles to mimic reference behavior
                    //     zoom: 'Area Zoom',
                    //     back: 'Reset Zoom'
                    // },
                    iconStyle: { // Keep styling for the button itself if needed
                        borderColor: 'hsl(var(--muted-foreground))',
                        borderWidth: 1,
                        // Optionally hide the default icons if the button is still desired
                        // icon: 'path://', // Example: replace with empty path or custom icon
                    },
                    emphasis: {
                        iconStyle: {
                            borderColor: 'hsl(var(--primary))'
                        }
                    }
                },
                // saveAsImage: { // Remove saveAsImage feature
                //     show: false,
                // }
            }
        },
        dataZoom: [
            {
                type: 'inside', // For mouse wheel/drag zooming INSIDE the chart area
                xAxisIndex: 0,
                start: 0,
                end: 100,
                zoomOnMouseWheel: true,
                moveOnMouseMove: true,
                moveOnMouseWheel: true
            },
            {
                type: 'slider', // The visual slider component (often at the bottom)
                show: false, // Hide the slider component
                xAxisIndex: 0,
                start: 0,
                end: 100,
                // Add brushStyle for better selection visibility
                brushStyle: {
                    borderWidth: 1,
                    borderColor: 'hsl(var(--primary))',
                    color: 'hsla(var(--primary-hsl) / 0.2)', // Use primary color with transparency
                },
                // Optional: Customize handle appearance if needed
                // handleIcon: 'path://...',
                // handleSize: '80%',
                // handleStyle: { ... },
                // dataBackground: { ... },
                // selectedDataBackground: { ... },
                height: 15, // Adjust height if needed
                bottom: 5, // Adjust position if needed
            },
            { // This corresponds to the toolbox dataZoom feature (area selection)
                type: 'select', // This enables the brush selection via the toolbox button
                xAxisIndex: 0,
                start: 0,
                end: 100,
                zoomOnMouseWheel: true,
                moveOnMouseMove: true,
                moveOnMouseWheel: true
            }
        ],
        series: seriesData,
        animation: true,
        animationDuration: 800,
        animationEasing: 'cubicOut' as const
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
            groupBy: props.groupBy
        });

        if (response.success && response.data) {
            histogramData.value = response.data.data || [];
            currentGranularity.value = response.data.granularity || ''; // Store granularity
            initialDataLoaded.value = true;
        } else {
            histogramData.value = [];
            currentGranularity.value = ''; // Reset on failure
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

        // Create chart instance with the current theme name
        const themeName = colorMode.value === 'dark' ? 'logchef-dark' : '';
        console.log(`Initializing chart with theme: ${colorMode.value === 'dark' ? 'logchef-dark' : 'default'}`);
        chart = init(chartRef.value, themeName);

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
        // Show loading state and fetch data when time range changes significantly

        // Skip if no valid source
        if (!hasValidSource.value || !currentSourceId.value) return;

        // Only trigger if time range actually changed
        if (newRange && oldRange &&
            (newRange.start?.toString() !== oldRange.start?.toString() ||
                newRange.end?.toString() !== oldRange.end?.toString())) {

            // Show loading state immediately to cover the data fetch delay
            isChartLoading.value = true;

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

            // Fetch new data for the updated time range
            debouncedFetchHistogramData();
        }
    },
    { deep: true }
);

// Watch for theme changes to re-initialize the chart
watch(colorMode, async (newMode, oldMode) => {
    // Add more detailed logging
    console.log(`Theme watcher triggered: newMode='${newMode}', oldMode='${oldMode}', isMounted=${isMounted.value}`);

    if (newMode !== oldMode && chartRef.value && isMounted.value) {
        console.log(`Actual theme change detected (${oldMode} -> ${newMode}), re-initializing chart.`);
        // Dispose existing chart and re-initialize with the new theme
        await initChart();
        // Re-fetch data if needed, or rely on existing data update logic
        // If data doesn't need re-fetching, ensure options are applied
        if (histogramData.value.length > 0) {
            updateChartOptions();
        } else if (hasValidSource.value && exploreStore.lastExecutionTimestamp) {
            // If no data, trigger a fetch if appropriate
            debouncedFetchHistogramData();
        }
    }
});

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

// Watch for changes in groupBy prop to refresh data
watch(
    () => props.groupBy,
    (newGroupBy, oldGroupBy) => {
        if (newGroupBy !== oldGroupBy && hasValidSource.value && exploreStore.lastExecutionTimestamp) {
            console.log('Histogram group by field changed, fetching data with new grouping');
            currentGroupBy.value = newGroupBy;
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
    border-radius: 0.5rem;
    background-color: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    box-shadow: 0 2px 4px 0 rgba(0, 0, 0, 0.05);
    overflow: hidden;
    margin-bottom: 1rem;
}

.chart-container {
    width: 100%;
    min-height: 180px;
    padding: 0.5rem 0.25rem;
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
    background-color: hsl(var(--card) / 0.9);
    z-index: 10;
    gap: 0.75rem;
    font-size: 0.875rem;
    color: hsl(var(--muted-foreground));
    backdrop-filter: blur(2px);
}

.loading-spinner {
    width: 1.75rem;
    height: 1.75rem;
    border: 3px solid hsl(var(--muted));
    border-top-color: hsl(var(--primary));
    border-radius: 50%;
    animation: spinner 0.9s ease-in-out infinite;
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
    gap: 0.5rem;
    opacity: 0.7;
}

@keyframes spinner {
    0% {
        transform: rotate(0deg);
    }

    100% {
        transform: rotate(360deg);
    }
}
</style>
