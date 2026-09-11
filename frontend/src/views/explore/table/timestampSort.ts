function timestampKey(value: unknown): bigint | null {
  if (value instanceof Date) {
    const milliseconds = value.getTime()
    return Number.isFinite(milliseconds) ? BigInt(milliseconds) * 1_000_000n : null
  }
  if (typeof value !== 'string') return null
  const match = /^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value)
  if (!match) return null
  const seconds = Date.parse(`${match[1]}${match[3]}`)
  if (!Number.isFinite(seconds)) return null
  return BigInt(seconds) * 1_000_000n + BigInt((match[2] ?? '').padEnd(9, '0'))
}

export function compareTimestamps(left: unknown, right: unknown): number {
  const leftKey = timestampKey(left)
  const rightKey = timestampKey(right)
  if (leftKey === rightKey) return 0
  if (leftKey === null) return -1
  if (rightKey === null) return 1
  return leftKey < rightKey ? -1 : 1
}
