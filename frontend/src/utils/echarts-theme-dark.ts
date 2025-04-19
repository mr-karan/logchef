/**
 * ECharts Dark Theme for LogChef
 * Adapted from official ECharts dark theme example.
 */
const contrastColor = '#B9B8CE'; // Lighter gray for text against dark background
const backgroundColor = 'hsl(var(--card))'; // Use card background from CSS vars
const axisLineColor = 'hsl(var(--muted-foreground) / 0.5)'; // Muted foreground for axis lines
const splitLineColor = 'hsl(var(--border) / 0.5)'; // Border color for split lines
const labelColor = 'hsl(var(--muted-foreground))'; // Muted foreground for labels
const titleColor = 'hsl(var(--foreground))'; // Default foreground for titles
const legendColor = 'hsl(var(--foreground))'; // Default foreground for legends
const toolboxColor = 'hsl(var(--muted-foreground))'; // Muted foreground for toolbox icons

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
            color: ['hsla(var(--card-hsl) / 0.02)', 'hsla(var(--card-hsl) / 0.05)']
        }
    },
    minorSplitLine: {
        lineStyle: {
            color: 'hsl(var(--border) / 0.2)'
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
    backgroundColor: backgroundColor, // Use CSS variable color

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
            color: legendColor // Use CSS variable color
        }
    },
    textStyle: {
        color: contrastColor // General text color
    },
    title: {
        textStyle: {
            color: titleColor // Use CSS variable color
        },
        subtextStyle: {
            color: contrastColor
        }
    },
    toolbox: {
        iconStyle: {
            borderColor: toolboxColor // Use CSS variable color
        },
        emphasis: {
            iconStyle: {
                borderColor: 'hsl(var(--primary))' // Use primary color on emphasis
            }
        }
    },
    dataZoom: {
        borderColor: 'hsl(var(--border))',
        textStyle: {
            color: contrastColor
        },
        brushStyle: {
            color: 'hsla(var(--primary-hsl) / 0.3)' // Use primary color for brush
        },
        handleStyle: {
            color: 'hsl(var(--muted))',
            borderColor: 'hsl(var(--border))'
        },
        moveHandleStyle: {
            color: 'hsl(var(--muted-foreground))',
            opacity: 0.5
        },
        fillerColor: 'hsla(var(--primary-hsl) / 0.2)', // Use primary color for filler
        emphasis: {
            handleStyle: {
                borderColor: 'hsl(var(--primary))',
                color: 'hsl(var(--primary) / 0.5)'
            },
            moveHandleStyle: {
                color: 'hsl(var(--primary))',
                opacity: 0.7
            }
        },
        dataBackground: {
            lineStyle: {
                color: 'hsl(var(--border))',
                width: 1
            },
            areaStyle: {
                color: 'hsl(var(--muted))'
            }
        },
        selectedDataBackground: {
            lineStyle: {
                color: 'hsl(var(--primary))'
            },
            areaStyle: {
                color: 'hsla(var(--primary-hsl) / 0.3)'
            }
        }
    },
    tooltip: {
        backgroundColor: 'hsl(var(--popover))',
        borderColor: 'hsl(var(--border))',
        textStyle: {
            color: 'hsl(var(--popover-foreground))'
        },
        axisPointer: {
            type: 'shadow',
            shadowStyle: {
                color: 'hsla(var(--foreground-hsl) / 0.05)' // Subtle shadow
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

// Specific overrides
logchefDarkTheme.categoryAxis.splitLine.show = false;
logchefDarkTheme.valueAxis.splitLine.show = true;
logchefDarkTheme.valueAxis.axisLine.show = false; // Hide Y axis line itself
logchefDarkTheme.valueAxis.axisTick.show = false; // Hide Y axis ticks
logchefDarkTheme.timeAxis.splitLine.show = false;
