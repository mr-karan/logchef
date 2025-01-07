import type * as Monaco from 'monaco-editor'

// LogchefQL keywords based on the backend grammar
const LOGCHEF_KEYWORDS = [
    // Fields
    'timestamp', 'severity', 'severity_text', 'body',
    'resource_name', 'trace_id', 'span_id', 'commit',
    'service_name', 'status_code', 'p',

    // Common JSON paths
    'error.code', 'error.message', 'error.type',
    'response.latency', 'response.time',
    'query.duration', 'client.ip',
    'user.id', 'user.role',
]

// Operators from the lexer
const OPERATORS = ['=', '!=', '~', '!~', '>', '<', '>=', '<=', '+', '-', '*', '/', '%']

export function configureLogchefQLLanguage(monaco: typeof Monaco) {
    // Register language
    monaco.languages.register({ id: 'logchefql' })

    // Configure syntax highlighting
    monaco.languages.setMonarchTokensProvider('logchefql', {
        defaultToken: '',
        tokenPostfix: '.logchefql',
        ignoreCase: true,

        keywords: LOGCHEF_KEYWORDS,
        operators: OPERATORS,

        // Symbols and special characters
        symbols: /[=><!~?:&|+\-*\/\^%]+/,

        tokenizer: {
            root: [
                // Keywords
                [/[a-zA-Z_][\w$]*/, {
                    cases: {
                        '@keywords': 'keyword',
                        '@default': 'identifier'
                    }
                }],

                // Operators
                [/@symbols/, {
                    cases: {
                        '@operators': 'operator',
                        '@default': 'delimiter'
                    }
                }],

                // Numbers
                [/\d*\.\d+([eE][\-+]?\d+)?/, 'number.float'],
                [/\d+/, 'number'],

                // Strings
                [/"([^"\\]|\\.)*$/, 'string.invalid'],  // non-terminated string
                [/'([^'\\]|\\.)*$/, 'string.invalid'],  // non-terminated string
                [/"/, 'string', '@string_double'],
                [/'/, 'string', '@string_single'],

                // Whitespace
                [/[ \t\r\n]+/, 'white']
            ],

            string_double: [
                [/[^\\"]+/, 'string'],
                [/\\./, 'string.escape'],
                [/"/, 'string', '@pop']
            ],

            string_single: [
                [/[^\\']+/, 'string'],
                [/\\./, 'string.escape'],
                [/'/, 'string', '@pop']
            ]
        }
    })

    // Register completion provider
    monaco.languages.registerCompletionItemProvider('logchefql', {
        provideCompletionItems: (model, position) => {
            const suggestions = [
                ...LOGCHEF_KEYWORDS.map(keyword => ({
                    label: keyword,
                    kind: monaco.languages.CompletionItemKind.Keyword,
                    insertText: keyword,
                    detail: `LogchefQL keyword: ${keyword}`
                })),
                ...OPERATORS.map(op => ({
                    label: op,
                    kind: monaco.languages.CompletionItemKind.Operator,
                    insertText: op,
                    detail: `LogchefQL operator: ${op}`
                }))
            ]

            return { suggestions }
        }
    })
}