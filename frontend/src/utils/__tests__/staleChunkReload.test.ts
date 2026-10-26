import { afterEach, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { reloadOnStaleChunk } from "../staleChunkReload";

const handlers: Array<(event: Event) => void> = [];

afterEach(() => {
  for (const handler of handlers.splice(0)) window.removeEventListener("vite:preloadError", handler);
  sessionStorage.clear();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const Page = defineComponent({ render: () => null });

// Mirrors Vite's preload helper: it dispatches the event, rethrows only when nobody prevented it, and otherwise resolves to undefined.
async function staleChunk() {
  const event = new Event("vite:preloadError", { cancelable: true });
  window.dispatchEvent(event);
  if (!event.defaultPrevented) throw new Error("Failed to fetch dynamically imported module");
  return undefined;
}

async function setup(href = "http://localhost/logchef/") {
  const router = createRouter({
    history: createMemoryHistory("/logchef/"),
    routes: [
      { path: "/", component: Page },
      { path: "/sources", component: staleChunk },
    ],
  });
  const location = { href, assign: vi.fn(), reload: vi.fn() };
  const handler = reloadOnStaleChunk(router, location);
  handlers.push(handler);
  window.addEventListener("vite:preloadError", handler);
  await router.push("/");
  return { router, location };
}

function preloadError() {
  return new Event("vite:preloadError", { cancelable: true });
}

it("loads the page the user was navigating to when its chunk is stale", async () => {
  const { router, location } = await setup();

  await router.push("/sources?team=1").catch(() => undefined);

  expect(location.assign).toHaveBeenCalledWith("/logchef/sources?team=1");
  expect(location.reload).not.toHaveBeenCalled();
});

it("reloads when the page loads at its own URL with a fragment", async () => {
  const { router, location } = await setup("http://localhost/logchef/sources?team=1#schema");

  await router.push("/sources?team=1#schema").catch(() => undefined);

  expect(location.reload).toHaveBeenCalledOnce();
  expect(location.assign).not.toHaveBeenCalled();
});

it("reloads when the target differs from the current page only in how its fragment is encoded", async () => {
  const { router, location } = await setup("http://localhost/logchef/sources?team=1#a\\b");

  await router.push("/sources?team=1#a\\b").catch(() => undefined);

  expect(location.reload).toHaveBeenCalledOnce();
  expect(location.assign).not.toHaveBeenCalled();
});

it("reloads the current page when no navigation is pending", async () => {
  const { location } = await setup();

  window.dispatchEvent(preloadError());

  expect(location.reload).toHaveBeenCalledOnce();
  expect(location.assign).not.toHaveBeenCalled();
});

it("leaves the error to the page while offline so a reload does not wipe the form", async () => {
  vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
  const { location } = await setup();

  const event = preloadError();
  window.dispatchEvent(event);

  expect(event.defaultPrevented).toBe(false);
  expect(location.reload).not.toHaveBeenCalled();
  expect(location.assign).not.toHaveBeenCalled();
});

it("reloads once for a stale chunk and not again in a loop", async () => {
  vi.useFakeTimers({ now: Date.parse("2026-10-26T10:00:00Z") });
  const { location } = await setup();

  const first = preloadError();
  window.dispatchEvent(first);
  expect(location.reload).toHaveBeenCalledOnce();
  expect(first.defaultPrevented).toBe(true);

  // The reloaded page still misses the chunk: the error surfaces instead of looping.
  vi.advanceTimersByTime(5_000);
  const second = preloadError();
  window.dispatchEvent(second);
  expect(location.reload).toHaveBeenCalledOnce();
  expect(second.defaultPrevented).toBe(false);
});

it("reloads again for a later deploy", async () => {
  vi.useFakeTimers({ now: Date.parse("2026-10-26T10:00:00Z") });
  const { location } = await setup();

  window.dispatchEvent(preloadError());
  vi.advanceTimersByTime(10 * 60_000);
  window.dispatchEvent(preloadError());

  expect(location.reload).toHaveBeenCalledTimes(2);
});
