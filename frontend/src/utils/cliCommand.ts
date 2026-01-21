interface CliCommandOptions {
  teamId: number
  sourceId: number
  mode: 'logchefql' | 'sql'
  query: string
  relativeTime?: string
  absoluteStart?: Date
  absoluteEnd?: Date
  limit?: number
  timeout?: number
}

/**
 * Generates a CLI command string that can be used to run the same query from the terminal
 */
export function generateCliCommand(options: CliCommandOptions): string {
  const {
    teamId,
    sourceId,
    mode,
    query,
    relativeTime,
    absoluteStart,
    absoluteEnd,
    limit,
    timeout,
  } = options

  // Escape query for shell
  const escapedQuery = escapeShellArg(query)

  if (mode === 'sql') {
    let cmd = `logchef sql ${escapedQuery} -t ${teamId} -S ${sourceId}`
    if (timeout) cmd += ` --timeout ${timeout}`
    return cmd
  }

  // LogChefQL mode
  let cmd = `logchef query ${escapedQuery} -t ${teamId} -S ${sourceId}`

  if (relativeTime) {
    cmd += ` -s ${relativeTime}`
  } else if (absoluteStart && absoluteEnd) {
    cmd += ` --from "${formatDateTime(absoluteStart)}" --to "${formatDateTime(absoluteEnd)}"`
  }

  if (limit && limit !== 100) {
    cmd += ` -l ${limit}`
  }

  return cmd
}

/**
 * Escapes a string for use as a shell argument using single quotes
 */
function escapeShellArg(arg: string): string {
  if (!arg) return "''"
  // Use single quotes and escape any single quotes within
  return "'" + arg.replace(/'/g, "'\\''") + "'"
}

/**
 * Formats a Date object to the format expected by the CLI (YYYY-MM-DD HH:MM:SS)
 */
function formatDateTime(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}
