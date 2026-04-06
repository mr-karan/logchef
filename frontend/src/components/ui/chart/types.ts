import type { InjectionKey, Ref } from "vue";

export type ChartTheme = Record<"light" | "dark", string>;

export type ChartConfig = Record<
  string,
  {
    label?: string;
    color?: string;
    theme?: ChartTheme;
  }
>;

export interface ChartContextValue {
  id: string;
  config: Ref<ChartConfig>;
}

export const chartContextKey: InjectionKey<ChartContextValue> = Symbol("chart-context");
