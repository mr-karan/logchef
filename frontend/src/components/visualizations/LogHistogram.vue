<script setup lang="ts">
import {
    ref,
    onMounted,
    onBeforeUnmount,
    watch,
    computed,
    nextTick,
    onActivated,
    onDeactivated,
} from "vue";
import { useColorMode } from "@vueuse/core";
import { CalendarDateTime } from "@internationalized/date";
import type { DateValue } from "@internationalized/date";
import { debounce } from "lodash-es";

// Store imports
import { useExploreStore } from "@/stores/explore";
import { type HistogramData } from "@/services/HistogramService";

// ECharts imports with tree-shaking
import { init, use, registerTheme } from "echarts/core";
import type { ECharts } from "echarts/core";
import { BarChart } from "echarts/charts";
import {
    TitleComponent,
    TooltipComponent,
    GridComponent,
    DataZoomComponent,
    ToolboxComponent,
    LegendComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";

// Register necessary echarts components
use([
    BarChart,
    TitleComponent,
    TooltipComponent,
    GridComponent,
    DataZoomComponent,
    ToolboxComponent,
    LegendComponent,
    CanvasRenderer,
]);

// Import the dark theme
import { logchefDarkTheme } from "@/utils/echarts-theme-dark";

// Register the dark theme only once
let themeRegistered = false;
if (!themeRegistered) {
    registerTheme("logchef-dark", logchefDarkTheme);
    themeRegistered = true;
    console.log("LogChef dark theme registered for ECharts.");
}

// Define component props
interface Props {
    timeRange?: {
        start: DateValue | any;
        end: DateValue | any;
    };
    isLoading?: boolean;
    height?: string;
    groupBy?: string | null;
}

// Define props with defaults
const props = withDefaults(defineProps<Props>(), {
    height: "180px",
    isLoading: false,
    groupBy: null,
});

// Define emits
const emit = defineEmits<{
    (e: "zoom-time-range", range: { start: Date; end: Date }): void;
    (e: "update:timeRange", range: { start: DateValue; end: DateValue }): void;
}>();

// Component state
const isMounted = ref(true);
const chartRef = ref<HTMLElement | null>(null);
let chart: ECharts | null = null;
const pendingFetchTimeoutId = ref<number | null>(null);

// Access stores
const exploreStore = useExploreStore();

// Theme state
const colorMode = useColorMode();
const histogramData = computed(() => exploreStore.histogramData);
const isChartLoading = computed(() => props.isLoading || exploreStore.isLoadingHistogram);
const histogramError = computed(() => exploreStore.histogramError);
const currentGranularity = computed(() => exploreStore.histogramGranularity || "");

// Window resize handler with debounce
const windowResizeEventCallback = debounce(async () => {
    try {
        await nextTick();
        chart?.resize();
    } catch (e) {
        console.error("Error during chart resize:", e);
    }
}, 100);

// Define severity-based color mapping for more consistent coloring
const severityColorMapping: Record<string, string> = {
    // Error levels
    error: "#EE6666", // Red
    err: "#EE6666", // Red
    fatal: "#CC3333", // Darker Red
    critical: "#CC3333", // Darker Red
    crit: "#CC3333", // Darker Red
    alert: "#CC3333", // Darker Red
    emerg: "#990000", // Even Darker Red
    emergency: "#990000", // Even Darker Red

    // Warning levels
    warn: "#FAC858", // Yellow
    warning: "#FAC858", // Yellow

    // Info levels
    info: "#5470C6", // Blue
    information: "#5470C6", // Blue
    notice: "#73C0DE", // Light Blue

    // Debug levels
    debug: "#91CC75", // Green
    trace: "#B5C334", // Olive Green
    verbose: "#C6E579", // Lime Green

    // HTTP methods
    get: "#73C0DE", // Light Blue
    post: "#91CC75", // Green
    put: "#FAC858", // Yellow
    delete: "#EE6666", // Red
    patch: "#9A60B4", // Purple
    options: "#6E7074", // Gray
    head: "#5D9B9B", // Grayish Cyan

    // Default/fallback color palette for other values
    default0: "#5470C6", // Blue
    default1: "#91CC75", // Green
    default2: "#FAC858", // Yellow
    default3: "#EE6666", // Red
    default4: "#73C0DE", // Light Blue
    default5: "#FC8452", // Orange
    default6: "#9A60B4", // Purple
    default7: "#ea7ccc", // Pink (more muted)
    default8: "#3BA272", // Darker Green
    default9: "#27727B", // Teal Blue
    default10: "#E062AE", // Magenta
    default11: "#FFB980", // Light Orange
    default12: "#5D9B9B", // Grayish Cyan
    default13: "#D48265", // Brownish Orange
    default14: "#C6E579", // Lime Green (more muted)
    default15: "#8378EA", // Lavender
};

// Helper function to get color for a group value based on severity level mapping
const getColorForGroupValue = (value: string): string => {
    if (!value) return severityColorMapping["default0"];

    // Convert to lowercase for case-insensitive matching
    const lowerValue = value.toLowerCase();

    // Check if we have a direct mapping
    if (severityColorMapping[lowerValue]) {
        return severityColorMapping[lowerValue];
    }

    // Check for partial matches in common log level words
    for (const [key, color] of Object.entries(severityColorMapping)) {
        // Skip default colors
        if (key.startsWith("default")) continue;

        // Check if the value contains a known severity word
        if (lowerValue.includes(key)) {
            return color;
        }
    }

    // Calculate a deterministic index for consistent coloring
    // This ensures the same unknown value always gets the same color
    let hash = 0;
    for (let i = 0; i < value.length; i++) {
        hash = (hash << 5) - hash + value.charCodeAt(i);
        hash |= 0; // Convert to 32bit integer
    }

    // Get absolute value and mod with number of default colors
    const index = Math.abs(hash) % 16;
    return severityColorMapping[`default${index}`];
};

// Helper function to update time range and emit events
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

    // Emit event with date objects for the parent component
    emit("zoom-time-range", { start: startDate, end: endDate });

    // Log details for debugging
    console.log("Emitted updated time range:", {
        start: startDateTime,
        end: endDateTime,
    });
}

// Convert the histogram data to chart options with Kibana-like styling
const convertHistogramData = (buckets: HistogramData[]) => {
    if (!buckets || buckets.length === 0) {
        return {
            title: {
                text: "Log Distribution",
                left: "center",
                textStyle: {
                    fontSize: 14,
                    fontWeight: "500",
                },
            },
            backgroundColor: "transparent",
            grid: {
                containLabel: true,
                left: 25,
                right: 25,
                bottom: 20,
                top: 35,
            },
            xAxis: {
                type: "category",
                data: [],
                silent: false,
                splitLine: { show: false },
                axisLine: {
                    lineStyle: {
                        color:
                            colorMode.value === "dark" ? "#71708A" : "hsl(var(--border))",
                    },
                },
                axisTick: {
                    lineStyle: {
                        color:
                            colorMode.value === "dark" ? "#71708A" : "hsl(var(--border))",
                    },
                },
            },
            yAxis: {
                type: "value",
                axisLine: { show: false },
                splitLine: {
                    show: true,
                    lineStyle: {
                        type: "dashed",
                        color:
                            colorMode.value === "dark"
                                ? "rgba(120, 120, 140, 0.3)"
                                : "hsl(var(--border))",
                        opacity: 0.5,
                    },
                },
                axisTick: { show: false },
                axisLabel: {
                    fontSize: 11,
                },
            },
            series: [
                {
                    type: "bar",
                    data: [],
                    itemStyle: {
                        color: "#5794F2",
                    },
                },
            ],
        };
    }

    // Check if data is grouped
    const isGrouped = buckets.some(
        (item) => item.group_value && item.group_value !== ""
    );

    // Format data for echart
    const categoryData: string[] = [];
    const timestamps: number[] = []; // Store actual timestamps for calculation
    let seriesData: any[] = [];

    // --- Determine Optimal Date/Time Format ---
    // Default: HH:MM in 24-hour format
    let timeFormatOptions: Intl.DateTimeFormatOptions = {
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    };
    const showSeconds = currentGranularity.value.endsWith("s");
    if (showSeconds) {
        timeFormatOptions.second = "2-digit"; // Add seconds if granularity requires it
    }

    if (buckets.length > 0) {
        const firstTimestamp = new Date(buckets[0].bucket).getTime();
        const lastTimestamp = new Date(
            buckets[buckets.length - 1].bucket
        ).getTime();
        const spanMs = lastTimestamp - firstTimestamp;
        const spanHours = spanMs / (1000 * 60 * 60);

        const firstDate = new Date(firstTimestamp);
        const lastDate = new Date(lastTimestamp);
        const isSameDay = firstDate.toDateString() === lastDate.toDateString();

        // If span is >= 24 hours OR data spans multiple days, include the date
        if (spanHours >= 24 || !isSameDay) {
            timeFormatOptions = {
                month: "numeric", // MM
                day: "numeric", // DD
                ...timeFormatOptions, // Include HH:MM or HH:MM:SS
            };
        }
    }
    // --- End Determine Format ---

    if (isGrouped) {
        // Group data by bucket and group_value
        const groupedByBucket: Record<string, Record<string, number>> = {};
        const uniqueGroups: string[] = [];

        buckets.forEach((item) => {
            const date = new Date(item.bucket);
            const bucketKey = date.toISOString(); // Use ISO string as key for consistency
            if (!groupedByBucket[bucketKey]) {
                groupedByBucket[bucketKey] = {};
                // Use the determined intelligent format
                categoryData.push(date.toLocaleString([], timeFormatOptions));
                timestamps.push(date.getTime());
            }
            const groupVal = item.group_value || "Other";
            if (!uniqueGroups.includes(groupVal)) {
                uniqueGroups.push(groupVal);
            }
            groupedByBucket[bucketKey][groupVal] = item.log_count;
        });

        // Create series for each group
        seriesData = uniqueGroups.map((group) => {
            const dataValues = Object.values(groupedByBucket).map(
                (bucketData) => bucketData[group] || 0
            );
            return {
                name: group,
                type: "bar",
                stack: "group",
                data: dataValues,
                barMaxWidth: "80%",
                barMinWidth: 2,
                large: true,
                largeThreshold: 100,
                itemStyle: {
                    color: getColorForGroupValue(group),
                    borderRadius: [2, 2, 0, 0],
                    opacity: 0.85,
                },
                emphasis: {
                    itemStyle: {
                        color: getColorForGroupValue(group),
                        opacity: 1,
                        shadowBlur: 4,
                        shadowColor: "rgba(0, 0, 0, 0.2)",
                    },
                },
            };
        });
    } else {
        const valueData: number[] = [];
        buckets.forEach((item) => {
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

        seriesData = [
            {
                name: "Log Count",
                type: "bar",
                barMaxWidth: "80%",
                barMinWidth: 2,
                data: valueData,
                large: true,
                largeThreshold: 100,
                itemStyle: {
                    color: "#5794F2", // Professional-style blue for log analytics
                    borderRadius: [2, 2, 0, 0],
                    opacity: 0.85,
                },
                emphasis: {
                    itemStyle: {
                        color: "#44A2F3", // Slightly darker blue for hover/selection
                        opacity: 1,
                        shadowBlur: 4,
                        shadowColor: "rgba(0, 0, 0, 0.2)",
                    },
                },
            },
        ];
    }

    return {
        title: {
            show: false, // Hide the title
        },
        backgroundColor: "transparent",
        grid: {
            containLabel: true,
            left: 25,
            right: 25,
            bottom: 20,
            top: 35,
        },
        tooltip: {
            show: true,
            trigger: "axis",
            confine: false,
            z: 60,
            position: function (point: any, _params: any, _dom: any, _rect: any, size: any) {
                // Position the tooltip slightly below the cursor
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
            backgroundColor:
                colorMode.value === "dark" ? "#1c1c25" : "hsl(var(--popover))",
            borderColor:
                colorMode.value === "dark" ? "#302f3d" : "hsl(var(--border))",
            borderWidth: 1,
            padding: 10,
            textStyle: {
                fontSize: 12,
                color:
                    colorMode.value === "dark"
                        ? "#FFFFFF"
                        : "hsl(var(--popover-foreground))",
            },
            axisPointer: {
                type: "shadow",
                shadowStyle: {
                    color: "rgba(0, 0, 0, 0.1)",
                },
            },
            formatter: function (params: any) {
                // Add null checks for DOM element existence
                if (!params || !params.length || params[0].dataIndex === undefined)
                    return "";

                const index = params[0].dataIndex;
                // Add bounds checking for data arrays
                if (index < 0 || index >= categoryData.length) return "";

                // Use the pre-formatted time string directly from categoryData
                const displayTimeStr = categoryData[index];

                let total = 0;
                const details = params
                    .map((p: any) => {
                        const value = p.data || 0;
                        total += value;
                        return `<div style="display: flex; align-items: center; margin-bottom: 4px;">
                        <span style="display: inline-block; width: 10px; height: 10px; background-color: ${p.color
                            }; margin-right: 6px; border-radius: 2px;"></span>
                        <span>${p.seriesName
                            }: <strong>${value.toLocaleString()}</strong></span>
                    </div>`;
                    })
                    .join("");

                return `<div style="font-size: 12px;">
                    <div style="font-weight: 500; margin-bottom: 4px;">${displayTimeStr}</div>
                    <div style="margin-bottom: 4px;">Total: <strong>${total.toLocaleString()}</strong></div>
                    ${details}
                </div>`;
            },
        },
        legend: isGrouped
            ? {
                show: true,
                bottom: 0,
                textStyle: {
                    fontSize: 11,
                },
            }
            : {
                show: false,
            },
        xAxis: {
            type: "category",
            data: categoryData,
            silent: false,
            axisLabel: {
                interval: function (index: number) {
                    // Show fewer labels when there are many data points
                    // Adjust the divisor (e.g., 15) based on desired density
                    return index % Math.max(Math.ceil(categoryData.length / 15), 1) === 0;
                },
                formatter: function (value: string) {
                    // Value is already pre-formatted intelligently
                    return value;
                },
                fontSize: 11,
                margin: 10,
            },
            axisLine: {
                lineStyle: {
                    color: colorMode.value === "dark" ? "#71708A" : "hsl(var(--border))",
                },
            },
            axisTick: {
                alignWithLabel: true,
            },
            splitLine: { show: false },
        },
        yAxis: {
            type: "value",
            axisLine: { show: false },
            axisTick: { show: false },
            axisPointer: {
                label: { precision: 0 },
            },
            splitLine: {
                show: true,
                lineStyle: {
                    type: "dashed",
                    color:
                        colorMode.value === "dark"
                            ? "rgba(120, 120, 140, 0.3)"
                            : "hsl(var(--border))",
                    opacity: 0.5,
                },
            },
            axisLabel: {
                formatter: (value: number) => {
                    if (value < 1000) {
                        return Math.round(value).toLocaleString();
                    } else if (value < 1000000) {
                        return (Math.round(value / 100) / 10).toLocaleString() + "K";
                    } else if (value < 1000000000) {
                        return (Math.round(value / 100000) / 10).toLocaleString() + "M";
                    } else {
                        return (Math.round(value / 100000000) / 10).toLocaleString() + "B";
                    }
                },
                fontSize: 11,
                margin: 12,
            },
        },
        toolbox: {
            orient: "horizontal",
            show: true, // Keep toolbox visible for functionality
            showTitle: false, // Hide titles
            itemSize: 1, // Make icons tiny
            right: 15,
            top: 5,
            feature: {
                dataZoom: {
                    show: true, // Keep feature enabled
                    showTitle: false, // Hide titles
                    yAxisIndex: "none",
                    icon: {
                        zoom: "path://", // Empty path to hide icon but keep functionality
                        back: "path://", // Empty path to hide icon but keep functionality
                    },
                    iconStyle: {
                        borderWidth: 0, // Hide border
                        opacity: 0, // Make completely transparent
                    },
                    emphasis: {
                        iconStyle: {
                            borderWidth: 0,
                            opacity: 0,
                        },
                    },
                },
            },
        },
        dataZoom: [
            {
                type: "inside", // For mouse wheel/drag zooming
                xAxisIndex: 0,
                start: 0,
                end: 100,
                zoomOnMouseWheel: false, // Disable mouse wheel zoom
                moveOnMouseMove: true,
                moveOnMouseWheel: false, // Disable mouse wheel move
            },
            {
                type: "slider", // The visual slider component
                show: false, // Hide the slider component
                xAxisIndex: 0,
                start: 0,
                end: 100,
                brushStyle: {
                    borderWidth: 1,
                    borderColor:
                        colorMode.value === "dark" ? "#6871F1" : "hsl(var(--primary))",
                    color:
                        colorMode.value === "dark"
                            ? "rgba(104, 113, 241, 0.2)"
                            : "hsla(var(--primary-hsl) / 0.2)",
                },
                height: 15, // Adjust height if needed
                bottom: 5, // Adjust position if needed
            },
            {
                // This corresponds to the toolbox dataZoom feature (area selection)
                type: "select", // This enables the brush selection via the toolbox button
                xAxisIndex: 0,
                brushMode: "single",
                brushStyle: {
                    borderWidth: 1,
                    borderColor:
                        colorMode.value === "dark" ? "#6871F1" : "hsl(var(--primary))",
                    color:
                        colorMode.value === "dark"
                            ? "rgba(104, 113, 241, 0.2)"
                            : "hsla(var(--primary-hsl) / 0.2)",
                },
                transformable: true,
                throttle: 100,
            },
        ],
        series: seriesData,
        animation: true,
        animationDuration: 800,
        animationEasing: "cubicOut" as const,
    };
};

// Handle chart zoom events with proper date parsing
function handleZoomAction(zoomParams: any) {
    if (!histogramData.value || histogramData.value.length === 0) return;

    try {
        console.log("DataZoom triggered with params:", zoomParams);

        // Extract the buckets from the histogramData
        const buckets = histogramData.value;
        const totalPoints = buckets.length;

        // Special case: Very small datasets (1-3 points)
        if (totalPoints <= 3) {
            // Just use the original time range from the data
            const startDate = new Date(buckets[0].bucket);

            // For the end date, use the last bucket plus a small increment
            const lastIndex = totalPoints - 1;
            const lastBucketTime = new Date(buckets[lastIndex].bucket).getTime();
            // Add 1 minute if only one bucket, or use bucket width if more than one
            const increment =
                lastIndex > 0
                    ? lastBucketTime - new Date(buckets[0].bucket).getTime()
                    : 60000; // 1 minute default

            const endDate = new Date(lastBucketTime + increment);

            console.log("Small dataset zoom: Using full range", {
                startDate,
                endDate,
            });

            // Emit native Date event
            emit("zoom-time-range", { start: startDate, end: endDate });

            return;
        }

        let startIndex: number;
        let endIndex: number;

        // Handle different zoom parameter formats
        if (
            zoomParams.startValue !== undefined &&
            zoomParams.endValue !== undefined
        ) {
            // Direct index values from toolbox dataZoom
            startIndex = Math.max(
                0,
                Math.min(parseInt(zoomParams.startValue), totalPoints - 1)
            );
            endIndex = Math.max(
                startIndex,
                Math.min(parseInt(zoomParams.endValue), totalPoints - 1)
            );

            console.log("Using direct index values:", { startIndex, endIndex });
        } else if (zoomParams.start !== undefined && zoomParams.end !== undefined) {
            // Percentage values from inside zoom
            const startPercent = zoomParams.start || 0;
            const endPercent = zoomParams.end || 100;

            // Calculate indices based on percentages
            startIndex = Math.floor((totalPoints * startPercent) / 100);
            endIndex = Math.ceil((totalPoints * endPercent) / 100) - 1;

            console.log("Using percentage values:", {
                startPercent,
                endPercent,
                startIndex,
                endIndex,
            });
        } else {
            console.error("Invalid zoom parameters:", zoomParams);
            return;
        }

        // Ensure indices are within bounds
        const validStartIndex = Math.max(0, Math.min(startIndex, totalPoints - 1));
        const validEndIndex = Math.max(
            validStartIndex,
            Math.min(endIndex, totalPoints - 1)
        );

        // Get the actual timestamps from the data
        const startDate = new Date(buckets[validStartIndex].bucket);

        // For the end date, we need to include the entire bucket
        // Calculate the bucket width (assume uniform width)
        let endDate: Date;

        if (validEndIndex >= totalPoints - 1) {
            // If at the end of the data, add a bucket width to include the entire last bucket
            const lastBucketTime = new Date(
                buckets[validEndIndex].bucket
            ).getTime();

            // Calculate bucket width if possible
            let bucketWidth = 60000; // Default 1 minute
            if (totalPoints > 1) {
                // Get average bucket width
                const firstTime = new Date(buckets[0].bucket).getTime();
                const lastTime = new Date(
                    buckets[totalPoints - 1].bucket
                ).getTime();
                bucketWidth = (lastTime - firstTime) / (totalPoints - 1);
            }

            endDate = new Date(lastBucketTime + bucketWidth);
        } else {
            // Use next bucket's start time as the end time
            endDate = new Date(buckets[validEndIndex + 1].bucket);
        }

        console.log("Selected time range:", {
            startDate,
            endDate,
            startIndex: validStartIndex,
            endIndex: validEndIndex,
        });

        // Emit native Date event
        emit("zoom-time-range", { start: startDate, end: endDate });

        // Also update the date picker
        updateTimeRangeForDatePicker(startDate, endDate);
    } catch (e) {
        console.error("Error handling zoom event:", e);

        // Fallback to using the full range
        if (histogramData.value.length > 0) {
            try {
                const startDate = new Date(histogramData.value[0].bucket);
                const lastBucket =
                    histogramData.value[histogramData.value.length - 1].bucket;
                const endDate = new Date(new Date(lastBucket).getTime() + 60000);

                console.log("Fallback: Using full time range after error");

                // Emit native Date event
                emit("zoom-time-range", { start: startDate, end: endDate });

                // Also update the date picker
                updateTimeRangeForDatePicker(startDate, endDate);
            } catch (fallbackError) {
                console.error("Even fallback failed:", fallbackError);
            }
        }
    }
}

// Reset and set the global cursor for datazoom
const restoreChart = () => {
    if (!chart) return;

    chart.dispatchAction({
        type: "restore",
    });

    // Set toolbox datazoom button state
    chart.dispatchAction({
        type: "takeGlobalCursor",
        key: "dataZoomSelect",
        dataZoomSelectActive: true,
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
    chart.on(
        "datazoom",
        safeHandler((params: any) => {
            // Either handle batch or direct parameters
            if (params?.batch && params.batch.length > 0) {
                // For the inside zoom with batch
                const batch = params.batch[0];
                handleZoomAction(batch);
            } else if (
                params?.startValue !== undefined &&
                params?.endValue !== undefined
            ) {
                // For direct toolbox dataZoom
                handleZoomAction(params);
            }

            // Always ensure the dataZoom selection mode is active
            chart?.dispatchAction({
                type: "takeGlobalCursor",
                key: "dataZoomSelect",
                dataZoomSelectActive: true,
            });
        })
    );

    // Restore event
    chart.on(
        "restore",
        safeHandler(() => {
            try {
                // Reset to full time range
                if (exploreStore.timeRange?.start && exploreStore.timeRange?.end) {
                    const startDate = new Date(
                        exploreStore.timeRange.start.year,
                        exploreStore.timeRange.start.month - 1,
                        exploreStore.timeRange.start.day,
                        'hour' in exploreStore.timeRange.start ? exploreStore.timeRange.start.hour : 0,
                        'minute' in exploreStore.timeRange.start ? exploreStore.timeRange.start.minute : 0,
                        'second' in exploreStore.timeRange.start ? exploreStore.timeRange.start.second : 0
                    );

                    const endDate = new Date(
                        exploreStore.timeRange.end.year,
                        exploreStore.timeRange.end.month - 1,
                        exploreStore.timeRange.end.day,
                        'hour' in exploreStore.timeRange.end ? exploreStore.timeRange.end.hour : 0,
                        'minute' in exploreStore.timeRange.end ? exploreStore.timeRange.end.minute : 0,
                        'second' in exploreStore.timeRange.end ? exploreStore.timeRange.end.second : 0
                    );

                    // Emit native Date event
                    emit("zoom-time-range", {
                        start: startDate,
                        end: endDate,
                    });
                }

                // Ensure dataZoom selection mode is active
                chart?.dispatchAction({
                    type: "takeGlobalCursor",
                    key: "dataZoomSelect",
                    dataZoomSelectActive: true,
                });
            } catch (e) {
                console.error("Error handling restore event:", e);
            }
        })
    );

    // Add handlers for brush events
    chart.on(
        "brush",
        safeHandler(() => {
            // Ensure dataZoom selection mode is active
            nextTick(() => {
                chart?.dispatchAction({
                    type: "takeGlobalCursor",
                    key: "dataZoomSelect",
                    dataZoomSelectActive: true,
                });
            });
        })
    );

    chart.on(
        "brushend",
        safeHandler(() => {
            // Ensure dataZoom selection mode is active
            nextTick(() => {
                chart?.dispatchAction({
                    type: "takeGlobalCursor",
                    key: "dataZoomSelect",
                    dataZoomSelectActive: true,
                });
            });
        })
    );

    // Handle window resize
    window.addEventListener("resize", windowResizeEventCallback);
};

// Update chart with new options
const updateChartOptions = () => {
    if (!chart) return;

    try {
        // Get options based on data
        const options = convertHistogramData(histogramData.value);

        // Add axis overrides for proper dark mode display
        if (colorMode.value === "dark") {
            // Update styling for dark mode
            options.yAxis.axisLine = { show: false };
            options.yAxis.axisTick = { show: false };
            options.xAxis.splitLine = { show: false };
            options.yAxis.splitLine = {
                show: true,
                lineStyle: {
                    type: "dashed",
                    color: "rgba(120, 120, 140, 0.3)",
                    opacity: 0.5,
                },
            };
        }

        // Set options
        chart.setOption(options, true);

        // Update loading state
        if (isChartLoading.value) {
            chart.showLoading({
                text: "Loading data...",
                maskColor: colorMode.value === "dark" ? "rgba(0, 0, 0, 0.5)" : "rgba(255, 255, 255, 0.8)",
                fontSize: 14,
            });
        } else {
            chart.hideLoading();
        }

        // Make sure dataZoom tool is selected
        chart.dispatchAction({
            type: "takeGlobalCursor",
            key: "dataZoomSelect",
            dataZoomSelectActive: true,
        });
    } catch (e) {
        console.error("Error updating chart options:", e);
    }
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
        const themeName = colorMode.value === "dark" ? "logchef-dark" : "";
        console.log(
            `Initializing chart with theme: ${colorMode.value === "dark" ? "logchef-dark" : "default"}`
        );
        chart = init(chartRef.value, themeName);

        // Set initial options
        updateChartOptions();

        // Setup event handlers
        setupChartEvents();

        // Ensure the dataZoom button is active
        restoreChart();

        // Explicitly set dataZoom to be active on initialization
        chart.dispatchAction({
            type: "takeGlobalCursor",
            key: "dataZoomSelect",
            dataZoomSelectActive: true,
        });
    } catch (e) {
        console.error("Error initializing chart:", e);
    }
};

// Watch for changes in histogram data to update the chart
watch(
    () => histogramData.value,
    () => {
        updateChartOptions();
    },
    { deep: true }
);

// Watch for changes in loading state
watch(
    () => isChartLoading.value,
    () => {
        updateChartOptions();
    }
);

// Watch theme changes to re-initialize the chart
watch(
    () => colorMode.value,
    async (newMode, oldMode) => {
        if (newMode !== oldMode && chartRef.value && isMounted.value) {
            console.log(
                `Theme changed from ${oldMode} to ${newMode}, re-initializing chart.`
            );
            await initChart();
        }
    }
);

// Watch for changes in group by to update chart
watch(
    () => props.groupBy,
    (newGroupBy) => {
        if (newGroupBy !== exploreStore.groupByField) {
            // Update the store with the new group by value
            exploreStore.setGroupByField(newGroupBy);

            // If we already have query execution, fetch histogram data with new groupBy
            if (exploreStore.lastExecutionTimestamp) {
                exploreStore.fetchHistogramData().catch(error => {
                    console.error("Error fetching histogram data after groupBy change:", error);
                });
            }
        }
    }
);

// Component lifecycle
onMounted(async () => {
    isMounted.value = true;
    console.log("LogHistogram: Component mounted");

    // Wait for DOM to be fully rendered
    await nextTick();

    // Initialize chart
    await initChart();
    console.log("LogHistogram: Chart initialized");

    // If we already have histogram data, update the chart
    if (histogramData.value && histogramData.value.length > 0) {
        updateChartOptions();
    }
});

onBeforeUnmount(() => {
    // Mark component as unmounted
    isMounted.value = false;

    // Clean up resources
    window.removeEventListener("resize", windowResizeEventCallback);

    if (chart) {
        // Remove event listeners first
        chart.off("datazoom");
        chart.off("restore");
        chart.off("brush");
        chart.off("brushend");
        // Then dispose the chart
        chart.dispose();
        chart = null;
    }

    // Clear any pending timeouts
    if (pendingFetchTimeoutId.value !== null) {
        clearTimeout(pendingFetchTimeoutId.value);
        pendingFetchTimeoutId.value = null;
    }
});

// Add onActivated and onDeactivated lifecycle hooks for keep-alive
onActivated(async () => {
    console.log("LogHistogram: Component activated");
    isMounted.value = true;

    // Wait for DOM to be fully rendered before reinitializing
    await nextTick();

    try {
        if (!chart && chartRef.value) {
            // If chart was disposed, reinitialize it
            console.log("LogHistogram: Initializing chart on activation");
            await initChart();
        } else if (chart) {
            // If chart still exists, resize it to fit container and refresh
            console.log("LogHistogram: Resizing existing chart on activation");
            chart.resize();

            // Force redraw with current data
            updateChartOptions();
        }
    } catch (e) {
        console.error("Error during histogram reactivation:", e);
    }
});

onDeactivated(() => {
    console.log("LogHistogram: Component deactivated");
    // Don't dispose the chart or set isMounted to false
    // We want to keep the chart instance alive for faster reactivation
});
</script>

<template>
    <div class="log-histogram">
        <!-- Loading overlay -->
        <div v-if="isChartLoading" class="histogram-loading-overlay">
            <div class="loading-spinner"></div>
            <span>Loading histogram data...</span>
        </div>

        <!-- Empty state -->
        <div v-else-if="!histogramData.length" class="histogram-empty-state">
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mb-2">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <line x1="9" y1="9" x2="9" y2="15"></line>
                <line x1="15" y1="9" x2="15" y2="15"></line>
            </svg>
            <span v-if="histogramError">{{ histogramError }}</span>
            <span v-else>No histogram data available</span>
        </div>

        <!-- Chart container -->
        <div ref="chartRef" class="chart-container" :style="{ height: props.height }"></div>
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
