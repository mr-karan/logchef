import { ref, computed, onMounted, onUnmounted, type Ref } from "vue";

const MIN_DRAG_PX = 6;

export interface BrushTimeRange {
  start: Date;
  end: Date;
}

/**
 * Custom brush selection for the histogram chart.
 *
 * Instead of blocking all pointer events with an overlay, this composable
 * listens for pointerdown on the chart container element itself, then
 * activates a visual selection overlay only during active dragging.
 * This allows the Unovis crosshair/tooltip to work normally on hover.
 */
export function useHistogramBrush(
  /** Ref to the chart wrapper element (contains both the SVG and the selection overlay) */
  containerRef: Ref<HTMLElement | null>,
  /** Time domain of the chart's x-axis */
  xDomain: Ref<{ start: number; end: number }>,
  /** Plot area insets matching VisXYContainer margin: { top, right, bottom, left } */
  plotInsets: { top: number; right: number; bottom: number; left: number },
) {
  const isDragging = ref(false);
  const startX = ref(0);
  const currentX = ref(0);

  // Selection box position in pixels relative to the plot area
  const selectionStyle = computed(() => {
    if (!isDragging.value) return null;
    const left = Math.min(startX.value, currentX.value);
    const width = Math.abs(currentX.value - startX.value);
    return {
      left: `${left}px`,
      width: `${width}px`,
    };
  });

  function getPlotBounds() {
    const el = containerRef.value;
    if (!el) return null;
    const rect = el.getBoundingClientRect();
    return {
      plotLeft: rect.left + plotInsets.left,
      plotWidth: rect.width - plotInsets.left - plotInsets.right,
    };
  }

  function clientXToPlotX(clientX: number): number {
    const bounds = getPlotBounds();
    if (!bounds) return 0;
    return Math.max(0, Math.min(bounds.plotWidth, clientX - bounds.plotLeft));
  }

  function plotXToTimestamp(plotX: number): number {
    const bounds = getPlotBounds();
    if (!bounds || bounds.plotWidth <= 0) return 0;
    const ratio = plotX / bounds.plotWidth;
    const { start, end } = xDomain.value;
    return start + ratio * (end - start);
  }

  function onPointerDown(e: PointerEvent) {
    // Only left button, only inside plot area
    if (e.button !== 0) return;
    const bounds = getPlotBounds();
    if (!bounds) return;

    const plotX = e.clientX - bounds.plotLeft;
    // Ignore clicks outside the plot area (on axes/labels)
    if (plotX < 0 || plotX > bounds.plotWidth) return;
    const plotY = e.clientY - (containerRef.value!.getBoundingClientRect().top + plotInsets.top);
    const plotHeight = containerRef.value!.getBoundingClientRect().height - plotInsets.top - plotInsets.bottom;
    if (plotY < 0 || plotY > plotHeight) return;

    startX.value = plotX;
    currentX.value = plotX;
    isDragging.value = true;

    // Capture pointer to track moves even outside the element
    containerRef.value!.setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: PointerEvent) {
    if (!isDragging.value) return;
    currentX.value = clientXToPlotX(e.clientX);
  }

  function onPointerUp(e: PointerEvent): BrushTimeRange | null {
    if (!isDragging.value) return null;
    isDragging.value = false;

    if (containerRef.value) {
      containerRef.value.releasePointerCapture(e.pointerId);
    }

    const dragPx = Math.abs(currentX.value - startX.value);
    if (dragPx < MIN_DRAG_PX) {
      return null; // Too small — let it be a click
    }

    const t1 = plotXToTimestamp(startX.value);
    const t2 = plotXToTimestamp(currentX.value);

    return {
      start: new Date(Math.min(t1, t2)),
      end: new Date(Math.max(t1, t2)),
    };
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && isDragging.value) {
      isDragging.value = false;
    }
  }

  function onContextMenu(e: Event) {
    if (isDragging.value) {
      isDragging.value = false;
      e.preventDefault();
    }
  }

  // Register/cleanup global escape listener
  onMounted(() => {
    document.addEventListener("keydown", onKeyDown);
  });
  onUnmounted(() => {
    document.removeEventListener("keydown", onKeyDown);
  });

  return {
    isDragging,
    selectionStyle,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onContextMenu,
  };
}
