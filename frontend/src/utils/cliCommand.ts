import type { QueryLanguage } from '@/lib/queryMetadata'
import { parseRelativeSpec, toRfc3339, type ResolvedWindow, type TimeWindow } from '@/utils/timeWindow'

interface CliCommandOptions {
  teamId: number
  sourceId: number
  language: QueryLanguage
  query: string
  window: TimeWindow
  resolved: ResolvedWindow
  limit?: number
  timeout?: number
}

/**
 * Generates a CLI command string that can be used to run the same query from the terminal
 */
export function generateCliCommand(options: CliCommandOptions): string {
  const { teamId, sourceId, language, query, window, resolved, limit, timeout } = options

  // Escape query for shell
  const escapedQuery = escapeShellArg(query)

  // Native ClickHouse SQL owns its time predicates, so it is copied as executed.
  if (language === 'clickhouse-sql') {
    let cmd = `logchef sql ${escapedQuery} -t ${teamId} -S ${sourceId}`
    if (timeout) cmd += ` --timeout ${timeout}`
    return cmd
  }

  if (language === 'logsql') {
    let cmd = `logchef sql ${escapedQuery} -t ${teamId} -S ${sourceId}${timeFlags(window, resolved)}`
    if (timeout) cmd += ` --timeout ${timeout}`
    return cmd
  }

  let cmd = `logchef query ${escapedQuery} -t ${teamId} -S ${sourceId}${timeFlags(window, resolved)}`

  if (limit && limit !== 100) {
    cmd += ` -l ${limit}`
  }

  return cmd
}

// A duration stays relative. Calendar days and absolute windows copy the instants
// the Explorer resolved, so the CLI's own timezone setting cannot shift them.
function timeFlags(window: TimeWindow, resolved: ResolvedWindow): string {
  if (window.kind === 'relative' && parseRelativeSpec(window.spec)?.kind === 'duration') {
    return ` -s ${window.spec}`
  }
  return ` --from "${toRfc3339(resolved.start)}" --to "${toRfc3339(resolved.end)}"`
}

/**
 * Escapes a string for use as a shell argument using single quotes
 */
function escapeShellArg(arg: string): string {
  if (!arg) return "''"
  // Use single quotes and escape any single quotes within
  return "'" + arg.replace(/'/g, "'\\''") + "'"
}
