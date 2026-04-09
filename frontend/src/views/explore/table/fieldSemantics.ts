const MESSAGE_FIELD_ALIASES = ['message', 'msg', 'log', 'text', 'body', 'content'] as const

export function normalizeFieldName(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '')
}

export function isPrimaryMessageField(name: string): boolean {
  return MESSAGE_FIELD_ALIASES.includes(normalizeFieldName(name) as (typeof MESSAGE_FIELD_ALIASES)[number])
}

export { MESSAGE_FIELD_ALIASES }
