/** Quote a raw ClickHouse identifier as one backtick-delimited token. */
export function quoteClickHouseIdentifier(name: string): string {
  return "`" + name.replace(/\\/g, "\\\\").replace(/`/g, "\\x60") + "`";
}
