import { App, applyDocumentTheme, applyHostStyleVariables } from '@modelcontextprotocol/ext-apps'
import type { McpUiHostContext } from '@modelcontextprotocol/ext-apps'
import { record, text, items } from './data'

export const app = new App({ name: 'Logchef investigation', version: '0.1.0' }, { availableDisplayModes: ['inline', 'fullscreen'] })

export async function callTool(name: string, args: Record<string, unknown>): Promise<unknown> {
  const result = await app.callServerTool({ name, arguments: args })
  if (result.isError) {
    const message = result.content.filter((block) => block.type === 'text').map((block) => block.text).join('\n')
    throw new Error(message || `${name} failed.`)
  }
  if (result.structuredContent !== undefined) return result.structuredContent
  for (const block of result.content) {
    if (block.type === 'text') {
      const decoded: unknown = JSON.parse(block.text)
      return decoded
    }
  }
  throw new Error(`${name} returned no data.`)
}

export function applyTheme(value: McpUiHostContext | undefined): void {
  if (!value) return
  if (value.theme) applyDocumentTheme(value.theme)
  if (value.styles?.variables) applyHostStyleVariables(value.styles.variables)
  if (value.displayMode) document.documentElement.dataset.displayMode = value.displayMode
  if (value.safeAreaInsets) {
    for (const [edge, inset] of Object.entries(value.safeAreaInsets)) {
      document.documentElement.style.setProperty(`--safe-${edge}`, `${inset}px`)
    }
  }
}

export function toolError(value: unknown): string {
  if (value instanceof Error) return value.message
  if (record(value)) return items(value.content).filter(record).map((block) => text(block.text)).join('\n') || 'Request failed.'
  return 'Request failed.'
}
