import { AppBridge, PostMessageTransport } from '@modelcontextprotocol/ext-apps/app-bridge'
import { CallToolResultSchema } from '@modelcontextprotocol/core'

const frame = document.querySelector('iframe')
const status = document.querySelector('#status')
const context = document.querySelector('#context')
if (!frame || !status || !context) throw new Error('Host elements missing.')
const bridge = new AppBridge(null, { name: 'Logchef local test host', version: '0.1.0' }, {
  serverTools: {}, updateModelContext: {}, message: {},
}, { hostContext: { theme: 'light', displayMode: 'inline', availableDisplayModes: ['inline', 'fullscreen'] } })
bridge.oncalltool = async (params) => {
  const response = await fetch('/rpc', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: crypto.randomUUID(), method: 'tools/call', params }) })
  const body: unknown = await response.json()
  if (typeof body !== 'object' || body === null || !('result' in body)) throw new Error('MCP request failed.')
  const result = CallToolResultSchema.parse(body.result)
  status.textContent = `${params.name}: ${result.isError ? 'error' : 'complete'}`
  return result
}
bridge.onupdatemodelcontext = async (params) => { context.textContent = JSON.stringify(params, null, 2); return {} }
bridge.onmessage = async (params) => { status.textContent = `Conversation message: ${JSON.stringify(params)}`; return {} }
bridge.onrequestdisplaymode = async ({ mode }) => {
  bridge.setHostContext({ displayMode: mode })
  status.textContent = `Display mode: ${mode}`
  return { mode }
}
bridge.onsizechange = ({ height }) => { if (height) frame.style.height = `${height}px` }
let theme: 'dark' | 'light' = 'light'
document.querySelector('#theme')?.addEventListener('click', () => {
  theme = theme === 'dark' ? 'light' : 'dark'
  bridge.setHostContext({ theme })
})
document.querySelector('#width')?.addEventListener('click', () => {
  frame.style.maxWidth = frame.style.maxWidth === '390px' ? '' : '390px'
})
bridge.oninitialized = () => { status.textContent = 'Apps bridge connected'; void bridge.sendToolInput({ arguments: {} }) }
frame.addEventListener('load', () => {
  if (!frame.contentWindow) throw new Error('Frame unavailable.')
  void bridge.connect(new PostMessageTransport(frame.contentWindow, frame.contentWindow))
}, { once: true })
frame.src = '/panel'
