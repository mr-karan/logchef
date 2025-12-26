import { now, getLocalTimeZone, CalendarDateTime, type DateValue } from '@internationalized/date';

export function timestampToCalendarDateTime(timestamp: number): CalendarDateTime {
  const date = new Date(timestamp);
  return new CalendarDateTime(
    date.getFullYear(),
    date.getMonth() + 1,
    date.getDate(),
    date.getHours(),
    date.getMinutes(),
    date.getSeconds()
  );
}

export function calendarDateTimeToTimestamp(dt: DateValue): number {
  return new Date(
    dt.year,
    dt.month - 1,
    dt.day,
    'hour' in dt ? dt.hour : 0,
    'minute' in dt ? dt.minute : 0,
    'second' in dt ? dt.second : 0
  ).getTime();
}

/**
 * Parses a relative time string like "15m", "1h", "1d" and returns start/end DateValues
 * @param relativeTimeString The relative time string (e.g., "15m", "1h", "1d")
 * @returns An object with start and end DateValues
 */
export function parseRelativeTimeString(relativeTimeString: string): { start: DateValue; end: DateValue } {
  // Default to "now" for the end time
  const end = now(getLocalTimeZone());

  // For specific presets like "today" or "yesterday"
  if (relativeTimeString === 'today') {
    const todayStart = new CalendarDateTime(
      end.year,
      end.month,
      end.day,
      0, // midnight
      0,
      0
    );
    return { start: todayStart, end };
  }

  if (relativeTimeString === 'yesterday') {
    const yesterdayStart = new CalendarDateTime(
      end.year,
      end.month,
      end.day - 1,
      0, // midnight
      0,
      0
    );
    const yesterdayEnd = new CalendarDateTime(
      end.year,
      end.month,
      end.day,
      0, // midnight
      0,
      0
    );
    return { start: yesterdayStart, end: yesterdayEnd };
  }

  // Parse duration strings like "15m", "1h", "7d", etc.
  const match = relativeTimeString.match(/^(\d+)([mhdw])$/);
  if (!match) {
    throw new Error(`Invalid relative time format: ${relativeTimeString}`);
  }

  const value = parseInt(match[1], 10);
  const unit = match[2];

  let start = end.copy();

  switch (unit) {
    case 'm': // minutes
      start = start.subtract({ minutes: value });
      break;
    case 'h': // hours
      start = start.subtract({ hours: value });
      break;
    case 'd': // days
      start = start.subtract({ days: value });
      break;
    case 'w': // weeks
      start = start.subtract({ weeks: value });
      break;
    default:
      throw new Error(`Unsupported time unit: ${unit}`);
  }

  return { start, end };
}

/**
 * Converts a relative time string to a human-readable label
 * @param relativeTimeString The relative time string (e.g., "15m", "1h", "1d")
 * @returns A human-readable label (e.g., "Last 15 minutes")
 */
export function relativeTimeToLabel(relativeTimeString: string): string {
  if (relativeTimeString === 'today') {
    return 'Today';
  }

  if (relativeTimeString === 'yesterday') {
    return 'Yesterday';
  }

  const match = relativeTimeString.match(/^(\d+)([mhdw])$/);
  if (!match) {
    return relativeTimeString; // return as-is if not parseable
  }

  const value = parseInt(match[1], 10);
  const unit = match[2];

  switch (unit) {
    case 'm':
      return `Last ${value} minute${value === 1 ? '' : 's'}`;
    case 'h':
      return `Last ${value} hour${value === 1 ? '' : 's'}`;
    case 'd':
      return `Last ${value} day${value === 1 ? '' : 's'}`;
    case 'w':
      return `Last ${value} week${value === 1 ? '' : 's'}`;
    default:
      return relativeTimeString;
  }
}

/**
 * Common relative time presets for the dropdown
 */
export const RELATIVE_TIME_PRESETS = [
  { value: '5m', label: 'Last 5 minutes' },
  { value: '15m', label: 'Last 15 minutes' },
  { value: '30m', label: 'Last 30 minutes' },
  { value: '1h', label: 'Last 1 hour' },
  { value: '3h', label: 'Last 3 hours' },
  { value: '6h', label: 'Last 6 hours' },
  { value: '12h', label: 'Last 12 hours' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '2d', label: 'Last 2 days' },
  { value: '7d', label: 'Last 7 days' },
  { value: 'today', label: 'Today' },
  { value: 'yesterday', label: 'Yesterday' },
];