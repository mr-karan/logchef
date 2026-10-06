export interface HostConnection { mcpUrl: string; token: string }

export function localBackendUrl(value: string): string {
  const url = new URL(value)
  if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password || url.search || url.hash || url.pathname !== '/') {
    throw new Error('Local test backend must be a loopback HTTP origin.')
  }
  return url.origin
}

export async function loadConnection(env: Readonly<Record<string, string | undefined>>): Promise<HostConnection> {
  const tokenPath = env.LOGCHEF_MCP_TOKEN_FILE
  if (!tokenPath) throw new Error('Set LOGCHEF_MCP_TOKEN_FILE to a file that holds an MCP access token. Keep credentials outside the repository.')
  const backendUrl = localBackendUrl(env.LOGCHEF_LOCAL_URL ?? 'http://127.0.0.1:8125')
  const token = (await Bun.file(tokenPath).text()).trim()
  if (!token || token.length > 4096 || /[\r\n]/.test(token)) throw new Error('Invalid MCP token file.')
  return { mcpUrl: `${backendUrl}/mcp`, token }
}
