// Relative time ranges offered as one-click choices in time pickers. Each is a
// spec that relativeWindow accepts; its label comes from windowLabel.
export const QUICK_RANGES = ["5m", "15m", "30m", "1h", "3h", "6h", "12h", "24h", "2d", "7d", "30d", "90d"] as const;

export type QuickRange = (typeof QUICK_RANGES)[number];
