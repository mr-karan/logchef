import { createVNode, render, type Component } from "vue";
import type { ChartConfig } from "./types";

export function componentToString<TProps extends Record<string, unknown>>(
  config: ChartConfig,
  component: Component,
  baseProps?: TProps,
) {
  const cache = new Map<string, string>();

  return (
    payload: unknown,
    x: number | Date,
    data?: unknown[],
    leftNearestDatumIndex?: number,
  ) => {
    if (typeof document === "undefined") {
      return "";
    }

    const cacheKey = JSON.stringify({ payload, x, leftNearestDatumIndex });
    const cached = cache.get(cacheKey);
    if (cached) {
      return cached;
    }

    const container = document.createElement("div");
    const vnode = createVNode(component as never, {
      ...(baseProps ?? {}),
      config,
      payload,
      x,
      data,
      leftNearestDatumIndex,
    });

    render(vnode, container);
    const html = container.innerHTML;
    render(null, container);

    if (cache.size > 200) {
      cache.clear();
    }

    cache.set(cacheKey, html);
    return html;
  };
}
