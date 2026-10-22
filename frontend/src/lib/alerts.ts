// Stores re-check alertsEnabled because a bookmarked URL or a direct call can bypass the router guard.
export function alertsDisabledResult() {
  return { success: false as const, data: null };
}
