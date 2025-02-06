import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";
import type { Updater } from "@tanstack/vue-table";
import type { Ref } from "vue";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function debounce<T extends (...args: any[]) => any>(
  fn: T,
  delay: number
) {
  let timeoutId: ReturnType<typeof setTimeout>;
  return function (this: any, ...args: Parameters<T>) {
    clearTimeout(timeoutId);
    timeoutId = setTimeout(() => fn.apply(this, args), delay);
  };
}

export function formatTimestamp(timestamp: string): string {
  try {
    const date = new Date(timestamp);
    // Check if the date is valid
    if (isNaN(date.getTime())) {
      return timestamp;
    }
    // Format as RFC3339 / ISO8601 with milliseconds
    return date.toISOString();
  } catch (e) {
    return timestamp;
  }
}

export function valueUpdater<T extends Updater<any>>(
  updaterOrValue: T,
  ref: Ref
) {
  ref.value =
    typeof updaterOrValue === "function"
      ? updaterOrValue(ref.value)
      : updaterOrValue;
}
