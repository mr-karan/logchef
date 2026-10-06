import { expect, test } from 'bun:test'
import { loadConnection, localBackendUrl } from './config'

test('local host accepts loopback origins', () => {
  expect(localBackendUrl('http://127.0.0.1:8125')).toBe('http://127.0.0.1:8125')
  expect(localBackendUrl('http://localhost:8125/')).toBe('http://localhost:8125')
  expect(localBackendUrl('http://[::1]:8125')).toBe('http://[::1]:8125')
})

test('local host rejects external origins and URL credentials', () => {
  for (const url of ['https://demo.logchef.app', 'http://example.com', 'http://localhost.example.com', 'http://user:secret@localhost:8125', 'http://localhost:8125/backend', 'http://localhost:8125/?token=value', 'http://localhost:8125/#fragment']) {
    expect(() => localBackendUrl(url)).toThrow('loopback HTTP origin')
  }
})

test('connection needs a token file and targets the integrated /mcp endpoint', async () => {
  expect(loadConnection({})).rejects.toThrow('LOGCHEF_MCP_TOKEN_FILE')
  const path = `${process.env.TMPDIR ?? '/tmp'}/logchef-mcp-dev-token-${crypto.randomUUID()}`
  await Bun.write(path, 'token-value\n')
  try {
    expect(await loadConnection({ LOGCHEF_MCP_TOKEN_FILE: path })).toEqual({ mcpUrl: 'http://127.0.0.1:8125/mcp', token: 'token-value' })
    expect(loadConnection({ LOGCHEF_MCP_TOKEN_FILE: path, LOGCHEF_LOCAL_URL: 'https://logchef.example.com' })).rejects.toThrow('loopback HTTP origin')
    await Bun.write(path, 'a\nb')
    expect(loadConnection({ LOGCHEF_MCP_TOKEN_FILE: path })).rejects.toThrow('Invalid MCP token file')
  } finally {
    await Bun.file(path).delete()
  }
})
