import type { Router } from "vue-router";
import { usePendingNavigation } from "@/router/navigation";

const RELOADED_AT_KEY = "logchef_stale_chunk_reload_at";
// A page that still misses the chunk right after reloading would otherwise reload forever.
const RELOAD_COOLDOWN_MS = 60_000;

type PageLocation = Pick<Location, "href" | "assign" | "reload">;

// After a deploy, a tab opened earlier asks for chunks that no longer exist.
// One page load fetches the new index.html and with it the new chunk names.
export function reloadOnStaleChunk(router: Router, location: PageLocation): (event: Event) => void {
  const pendingNavigation = usePendingNavigation(router);
  return (event) => {
    // Offline, a reload would fail anyway and wipe a half-filled form; the page shows its own load error.
    if (!navigator.onLine) return;
    const now = Date.now();
    const last = Number(sessionStorage.getItem(RELOADED_AT_KEY));
    if (Number.isFinite(last) && now - last < RELOAD_COOLDOWN_MS) return;
    sessionStorage.setItem(RELOADED_AT_KEY, String(now));
    event.preventDefault();
    // The router fails before it updates the URL, so a plain reload would drop the user's click.
    const target = pendingNavigation.value;
    if (!target) return location.reload();
    const href = router.resolve(target).href;
    // A URL that differs from the current one only after "#" scrolls instead of loading, so it needs a real reload.
    if (withoutFragment(new URL(href, location.href)) === withoutFragment(new URL(location.href))) location.reload();
    else location.assign(href);
  };
}

function withoutFragment(url: URL): string {
  url.hash = "";
  return url.href;
}
