/** Renders a group as "a=1, b=2" in key order, or "" for an ungrouped rule. */
export function formatAlertGroup(group: Record<string, string> | undefined | null): string {
  if (!group) return "";
  return Object.keys(group)
    .sort()
    .map((key) => `${key}=${group[key]}`)
    .join(", ");
}
