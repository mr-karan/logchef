import type { RouteRecordRaw, Router } from "vue-router";
import { routeRequirements, type PaletteRouteMeta, type RouteRequirement } from "@/router/access";

export interface PalettePage {
  readonly name: string;
  readonly path: string;
  readonly titleKey: string;
  readonly palette: PaletteRouteMeta;
  readonly requirements: readonly RouteRequirement[];
}

/** Routes that opt in with meta.palette, in declaration order, gated by their whole matched chain. */
export function routePages(router: Pick<Router, "options" | "resolve">): PalettePage[] {
  const pages: PalettePage[] = [];
  const visit = (records: readonly RouteRecordRaw[]) => {
    for (const record of records) {
      const palette = record.meta?.palette;
      const titleKey = record.meta?.title;
      if (typeof record.name === "string" && palette && titleKey) {
        const resolved = router.resolve({ name: record.name });
        pages.push({ name: record.name, path: resolved.path, titleKey, palette, requirements: routeRequirements(resolved.matched) });
      }
      if (record.children) visit(record.children);
    }
  };
  visit(router.options.routes);
  return pages;
}
