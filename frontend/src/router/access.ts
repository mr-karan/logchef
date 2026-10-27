import type { RouteRecordNormalized } from "vue-router";
import type { PaletteIcon } from "@/components/command-palette/icons";

export interface PaletteRouteMeta {
  readonly icon: PaletteIcon;
  /** Carry the current team and source in the query, as the sidebar does. */
  readonly contextual?: boolean;
}

declare module "vue-router" {
  interface RouteMeta {
    title?: string;
    public?: boolean;
    layout?: "outer";
    requiresAuth?: boolean;
    requiresAdmin?: boolean;
    requiresAnyTeamAdmin?: boolean;
    requiresOAuth?: boolean;
    palette?: PaletteRouteMeta;
  }
}

export type RouteRequirement = "oauth" | "alerts" | "admin" | "anyTeamAdmin";

export interface Viewer {
  readonly isAdmin: boolean;
  readonly isAnyTeamAdmin: boolean;
  readonly alertsEnabled: boolean;
  readonly oauthEnabled: boolean;
}

const ALERTS_PATH = "/logs/alerts";

const MEETS: Record<RouteRequirement, (viewer: Viewer) => boolean> = {
  oauth: (viewer) => viewer.oauthEnabled,
  alerts: (viewer) => viewer.alertsEnabled,
  admin: (viewer) => viewer.isAdmin,
  anyTeamAdmin: (viewer) => viewer.isAdmin || viewer.isAnyTeamAdmin,
};

/** What a matched route chain needs, so parent meta counts too. */
export function routeRequirements(chain: readonly Pick<RouteRecordNormalized, "path" | "meta">[]): RouteRequirement[] {
  const requirements: RouteRequirement[] = [];
  if (chain.some((record) => record.meta.requiresOAuth)) requirements.push("oauth");
  if (chain.some((record) => record.path === ALERTS_PATH || record.path.startsWith(`${ALERTS_PATH}/`))) {
    requirements.push("alerts");
  }
  if (chain.some((record) => record.meta.requiresAdmin)) requirements.push("admin");
  if (chain.some((record) => record.meta.requiresAnyTeamAdmin)) requirements.push("anyTeamAdmin");
  return requirements;
}

export function meetsRequirements(requirements: readonly RouteRequirement[], viewer: Viewer): boolean {
  return requirements.every((requirement) => MEETS[requirement](viewer));
}
