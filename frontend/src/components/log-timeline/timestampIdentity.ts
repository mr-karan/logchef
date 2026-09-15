const RFC3339Nano = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/

export function timestampIdentity(value: string | number): string | null {
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return null
    return (BigInt(Math.trunc(value)) * 1_000_000n).toString()
  }

  const match = RFC3339Nano.exec(value)
  if (!match) {
    const milliseconds = Date.parse(value)
    return Number.isNaN(milliseconds) ? null : (BigInt(milliseconds) * 1_000_000n).toString()
  }

  const milliseconds = Date.parse(`${match[1]}${match[3]}`)
  if (Number.isNaN(milliseconds)) return null
  const fraction = BigInt((match[2] ?? '').padEnd(9, '0').slice(0, 9))
  return (BigInt(milliseconds) * 1_000_000n + fraction).toString()
}
