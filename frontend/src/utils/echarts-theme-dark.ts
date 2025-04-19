/**
 * ECharts Dark Theme for LogChef
 * Adapted from official ECharts dark theme example.
 */
const contrastColor = '#B9B8CE'; // Lighter gray for text against dark background
const backgroundColor = 'transparent'; // Use transparent to inherit container background
const axisLineColor = 'rgba(185, 184, 206, 0.5)'; // Explicit light color for axis lines with transparency
const splitLineColor = 'rgba(120, 120, 140, 0.3)'; // Explicit split line color
const labelColor = '#D1D1DB'; // Explicit light gray for labels
const titleColor = '#FFFFFF'; // White for titles
const legendColor = '#FFFFFF'; // White for legend text
const toolboxColor = '#B9B8CE'; // Light gray for toolbox icons

const axisCommon = () => ({
    axisLine: {
        lineStyle: {
            color: axisLineColor
        }
    },
    splitLine: {
        lineStyle: {
            color: splitLineColor,
            type: 'dashed',
        }
    },
    splitArea: {
        areaStyle: {
            color: ['rgba(40, 40, 50, 0.02)', 'rgba(40, 40, 50, 0.05)']
        }
    },
    minorSplitLine: {
        lineStyle: {
            color: 'rgba(120, 120, 140, 0.2)'
        }
    },
    axisLabel: {
        color: labelColor,
        fontSize: 11,
    },
    axisTick: {
        lineStyle: {
            color: axisLineColor
        }
    },
    axisPointer: {
        label: {
            backgroundColor: '#6a7985' // Default ECharts dark pointer label bg
        }
    }
});

// Use the professional color palette defined in LogHistogram for consistency
const colorPalette = [
    '#5470C6', '#EE6666', '#FAC858', '#91CC75', '#73C0DE', '#FC8452',
    '#9A60B4', '#ea7ccc', '#3BA272', '#27727B', '#E062AE', '#FFB980',
    '#5D9B9B', '#D48265', '#C6E579', '#F4E001', '#B5C334', '#6E7074',
    '#8378EA', '#7A455D'
];

export const logchefDarkTheme = {
    darkMode: true, // Indicate this is a dark theme

    color: colorPalette,
    backgroundColor: backgroundColor,

    axisPointer: {
        lineStyle: {
            color: '#817f91'
        },
        crossStyle: {
            color: '#817f91'
        },
        label: {
            color: '#fff' // Keep labels white on dark background
        }
    },
    legend: {
        textStyle: {
            color: legendColor
        }
    },
    textStyle: {
        color: contrastColor // General text color
    },
    title: {
        textStyle: {
            color: titleColor
        },
        subtextStyle: {
            color: contrastColor
        }
    },
    toolbox: {
        iconStyle: {
            borderColor: toolboxColor
        },
        emphasis: {
            iconStyle: {
                borderColor: '#6871F1' // Explicit color for primary on emphasis
            }
        }
    },
    dataZoom: {
        borderColor: '#71708A',
        textStyle: {
            color: contrastColor
        },
        brushStyle: {
            color: 'rgba(104, 113, 241, 0.3)' // Explicit color for brush
        },
        handleStyle: {
            color: '#474752',
            borderColor: '#71708A'
        },
        moveHandleStyle: {
            color: '#B9B8CE',
            opacity: 0.5
        },
        fillerColor: 'rgba(104, 113, 241, 0.2)', // Explicit color for filler
        emphasis: {
            handleStyle: {
                borderColor: '#6871F1',
                color: 'rgba(104, 113, 241, 0.5)'
            },
            moveHandleStyle: {
                color: '#6871F1',
                opacity: 0.7
            }
        },
        dataBackground: {
            lineStyle: {
                color: '#71708A',
                width: 1
            },
            areaStyle: {
                color: '#474752'
            }
        },
        selectedDataBackground: {
            lineStyle: {
                color: '#6871F1'
            },
            areaStyle: {
                color: 'rgba(104, 113, 241, 0.3)'
            }
        }
    },
    tooltip: {
        backgroundColor: '#1c1c25',
        borderColor: '#302f3d',
        textStyle: {
            color: '#FFFFFF'
        },
        axisPointer: {
            type: 'shadow',
            shadowStyle: {
                color: 'rgba(220, 220, 240, 0.05)' // Subtle shadow
            }
        }
    },
    visualMap: {
        textStyle: {
            color: contrastColor
        }
    },
    timeline: {
        lineStyle: {
            color: contrastColor
        },
        label: {
            color: contrastColor
        },
        controlStyle: {
            color: contrastColor,
            borderColor: contrastColor
        }
    },
    calendar: {
        itemStyle: {
            color: backgroundColor
        },
        dayLabel: {
            color: contrastColor
        },
        monthLabel: {
            color: contrastColor
        },
        yearLabel: {
            color: contrastColor
        }
    },

    // Apply common axis styles
    timeAxis: axisCommon(),
    logAxis: axisCommon(),
    valueAxis: axisCommon(),
    categoryAxis: axisCommon(),

    line: {
        symbol: 'circle'
    },
    graph: {
        color: colorPalette
    },
    gauge: {
        title: {
            color: contrastColor
        }
    },
    candlestick: {
        itemStyle: {
            color: '#FD1050',
            color0: '#0CF49B',
            borderColor: '#FD1050',
            borderColor0: '#0CF49B'
        }
    }
};
