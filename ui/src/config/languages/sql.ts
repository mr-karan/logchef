import type * as Monaco from 'monaco-editor'

export const SQL_KEYWORDS = [
    // SQL Keywords
    'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'LIMIT',
    'HAVING', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER', 'ON',
    'WITH', 'UNION', 'ALL', 'AS', 'DISTINCT', 'CASE', 'WHEN',
    'THEN', 'ELSE', 'END', 'AND', 'OR', 'NOT', 'IN', 'EXISTS',
    'BETWEEN', 'LIKE', 'IS', 'NULL', 'ASC', 'DESC', 'VALUES',
    'INSERT', 'INTO', 'UPDATE', 'DELETE', 'CREATE', 'ALTER', 'DROP',
    'TABLE', 'INDEX', 'VIEW', 'TRIGGER', 'PROCEDURE', 'FUNCTION',

    // ClickHouse specific
    'PREWHERE', 'FINAL', 'SAMPLE', 'ARRAY', 'TUPLE', 'MAP', 'SETTINGS',
    'FORMAT', 'OPTIMIZE', 'ILIKE', 'GLOBAL'
]

export function configureSQLLanguage(monaco: typeof Monaco) {
    console.log('🔧 Starting SQL language configuration')

    // Register completion provider
    monaco.languages.registerCompletionItemProvider('sql', {
        provideCompletionItems: () => {
            const suggestions = SQL_KEYWORDS.map(keyword => ({
                label: keyword,
                kind: monaco.languages.CompletionItemKind.Keyword,
                insertText: keyword
            }))
            return { suggestions }
        }
    })

    console.log('✅ SQL language configured')
}