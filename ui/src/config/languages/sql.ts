import type * as Monaco from 'monaco-editor'

// SQL Keywords by category for better organization and suggestions
const SQL_KEYWORDS = {
    // Basic clauses
    clauses: [
        'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'LIMIT',
        'HAVING', 'UNION ALL', 'PREWHERE'
    ],
    
    // Table operations
    joins: [
        'JOIN', 'LEFT JOIN', 'INNER JOIN', 'ANY LEFT JOIN',
        'ALL LEFT JOIN', 'CROSS JOIN', 'RIGHT JOIN', 'FULL JOIN'
    ],

    // Logical operators
    operators: [
        'AND', 'OR', 'NOT', 'IN', 'LIKE', 'ILIKE', 'BETWEEN',
        'IS NULL', 'IS NOT NULL'
    ],

    // Control flow
    controlFlow: [
        'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'IF', 'IIF'
    ],

    // Common table aliases
    aliases: ['AS', 'ALIAS', 'WITH'],

    // Time functions
    timeFunctions: [
        'toDateTime', 'toDate', 'now', 'today', 'yesterday',
        'toStartOfHour', 'toStartOfMinute', 'toStartOfFiveMinute',
        'toStartOfDay', 'toStartOfMonth', 'toStartOfYear',
        'dateDiff', 'timeSlots', 'formatDateTime'
    ],

    // Aggregation functions
    aggFunctions: [
        'count', 'uniq', 'topK', 'avg', 'sum', 'min', 'max',
        'quantile', 'median', 'stddev', 'varPop'
    ],

    // String functions
    stringFunctions: [
        'match', 'extract', 'extractAll', 'indexOf', 'position',
        'lower', 'upper', 'trim', 'replaceAll', 'replaceRegexpAll',
        'splitByChar', 'splitByString'
    ],

    // JSON functions
    jsonFunctions: [
        'JSONExtract', 'JSONExtractString', 'JSONExtractFloat',
        'JSONExtractInt', 'JSONHas', 'JSONLength'
    ],

    // Array functions
    arrayFunctions: [
        'array', 'arrayMap', 'arrayFilter', 'arrayExists',
        'arrayCount', 'arrayDistinct', 'arrayJoin'
    ],

    // Window functions
    windowFunctions: [
        'ROW_NUMBER', 'RANK', 'DENSE_RANK', 'LAG', 'LEAD',
        'OVER', 'PARTITION BY'
    ]
}

// Flatten keywords for tokenizer while preserving categories for suggestions
const ALL_KEYWORDS = Object.values(SQL_KEYWORDS).flat()

// Common log fields for suggestions
const LOG_FIELDS = [
    'timestamp', 'severity', 'body', 'resource_name',
    'trace_id', 'span_id', 'commit', 'service_name',
    'status_code', 'error.code', 'error.message'
]

export function configureSQLLanguage(monaco: typeof Monaco) {
    // Register language
    monaco.languages.register({ id: 'clickhouse-sql' })

    // Configure syntax highlighting
    const tokenProvider = monaco.languages.setMonarchTokensProvider('clickhouse-sql', {
        defaultToken: '',
        ignoreCase: true,
        includeLF: true,

        brackets: [
            { open: '[', close: ']', token: 'delimiter.square' },
            { open: '(', close: ')', token: 'delimiter.parenthesis' },
            { open: '{', close: '}', token: 'delimiter.curly' }
        ],

        keywords: ALL_KEYWORDS,
        operators: [
            '=', '>', '<', '!', '~', '?', ':', '==', '<=', '>=',
            '!=', '&&', '||', '++', '--', '+', '-', '*', '/',
            '&', '|', '^', '%', '<<', '>>', '>>>', '+=', '-=',
            '*=', '/=', '&=', '|=', '^=', '%=', '<<=', '>>=', '>>>='
        ],

        symbols: /[=><!~?:&|+\-*\/\^%]+/,

        tokenizer: {
            root: [
                { include: '@whitespace' },
                { include: '@numbers' },
                { include: '@strings' },
                { include: '@comments' },

                // Identifiers and keywords
                [/[a-zA-Z_]\w*/, {
                    cases: {
                        '@keywords': { token: 'keyword.$0' },
                        '@operators': { token: 'operator.$0' },
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

                // Brackets
                [/[{}()\[\]]/, '@brackets'],

                // Delimiters
                [/[;,.]/, 'delimiter']
            ],

            whitespace: [
                [/\s+/, 'white']
            ],

            comments: [
                [/--.*$/, 'comment'],
                [/\/\*/, 'comment', '@comment'],
            ],

            comment: [
                [/[^/*]+/, 'comment'],
                [/\*\//, 'comment', '@pop'],
                [/[/*]/, 'comment']
            ],

            numbers: [
                [/\d*\.\d+([eE][\-+]?\d+)?/, 'number.float'],
                [/\d+/, 'number']
            ],

            strings: [
                [/'/, 'string', '@string'],
                [/"/, 'string', '@string_double']
            ],

            string: [
                [/[^']+/, 'string'],
                [/''/, 'string'],
                [/'/, 'string', '@pop']
            ],

            string_double: [
                [/[^"]+/, 'string'],
                [/""/, 'string'],
                [/"/, 'string', '@pop']
            ]
        }
    })

    // Enhanced completion provider with context awareness
    const completionProvider = monaco.languages.registerCompletionItemProvider('clickhouse-sql', {
        triggerCharacters: [' ', '.', '(', ','],

        provideCompletionItems: (model, position) => {
            const word = model.getWordUntilPosition(position)
            const lineContent = model.getLineContent(position.lineNumber)
            const beforeCursor = lineContent.substring(0, position.column - 1)
            
            const range = {
                startLineNumber: position.lineNumber,
                endLineNumber: position.lineNumber,
                startColumn: word.startColumn,
                endColumn: word.endColumn
            }

            const suggestions: Monaco.languages.CompletionItem[] = []

            // Context-aware suggestions
            if (beforeCursor.trim() === '') {
                // Start of line or after semicolon - suggest SELECT
                suggestions.push(...createCompletionItems(['SELECT'], range, monaco, 'keyword'))
            } else if (/SELECT\s+$/i.test(beforeCursor)) {
                // After SELECT - suggest fields and functions
                suggestions.push(
                    ...createCompletionItems(['*', 'DISTINCT'], range, monaco, 'keyword'),
                    ...createCompletionItems(LOG_FIELDS, range, monaco, 'field'),
                    ...createCompletionItems(SQL_KEYWORDS.aggFunctions, range, monaco, 'function')
                )
            } else if (/FROM\s+$/i.test(beforeCursor)) {
                // After FROM - suggest table names (you can add your table names here)
                suggestions.push(...createCompletionItems(['logs', 'system.logs'], range, monaco, 'class'))
            } else if (/WHERE\s+$/i.test(beforeCursor)) {
                // After WHERE - suggest fields and functions
                suggestions.push(
                    ...createCompletionItems(LOG_FIELDS, range, monaco, 'field'),
                    ...createCompletionItems(SQL_KEYWORDS.timeFunctions, range, monaco, 'function')
                )
            } else if (/GROUP\s+BY\s+$/i.test(beforeCursor)) {
                // After GROUP BY - suggest fields
                suggestions.push(...createCompletionItems(LOG_FIELDS, range, monaco, 'field'))
            } else if (/ORDER\s+BY\s+$/i.test(beforeCursor)) {
                // After ORDER BY - suggest fields
                suggestions.push(
                    ...createCompletionItems(LOG_FIELDS, range, monaco, 'field'),
                    ...createCompletionItems(['ASC', 'DESC'], range, monaco, 'keyword')
                )
            } else {
                // Default suggestions based on the current word
                const word = model.getWordUntilPosition(position).word.toLowerCase()
                
                // Suggest based on word prefix
                if (word.startsWith('to')) {
                    suggestions.push(...createCompletionItems(SQL_KEYWORDS.timeFunctions, range, monaco, 'function'))
                } else if (word.startsWith('json')) {
                    suggestions.push(...createCompletionItems(SQL_KEYWORDS.jsonFunctions, range, monaco, 'function'))
                } else if (word.startsWith('array')) {
                    suggestions.push(...createCompletionItems(SQL_KEYWORDS.arrayFunctions, range, monaco, 'function'))
                } else {
                    // Add all possible suggestions
                    Object.entries(SQL_KEYWORDS).forEach(([category, keywords]) => {
                        const kind = getCompletionItemKind(category, monaco)
                        suggestions.push(...createCompletionItems(keywords, range, monaco, kind))
                    })
                }
            }

            return { suggestions }
        }
    })

    // Return dispose function
    return () => {
        tokenProvider?.dispose()
        completionProvider?.dispose()
    }
}

// Helper function to create completion items
function createCompletionItems(
    items: string[],
    range: Monaco.IRange,
    monaco: typeof Monaco,
    kind: string
): Monaco.languages.CompletionItem[] {
    return items.map(item => ({
        label: item,
        kind: getCompletionItemKind(kind, monaco),
        insertText: item,
        range,
        sortText: getSortText(kind, item),
        detail: getItemDetail(kind, item)
    }))
}

// Helper function to get completion item kind
function getCompletionItemKind(category: string, monaco: typeof Monaco): Monaco.languages.CompletionItemKind {
    switch (category) {
        case 'keyword':
        case 'clauses':
            return monaco.languages.CompletionItemKind.Keyword
        case 'function':
        case 'timeFunctions':
        case 'aggFunctions':
        case 'stringFunctions':
        case 'jsonFunctions':
        case 'arrayFunctions':
            return monaco.languages.CompletionItemKind.Function
        case 'field':
            return monaco.languages.CompletionItemKind.Field
        case 'class':
            return monaco.languages.CompletionItemKind.Class
        default:
            return monaco.languages.CompletionItemKind.Text
    }
}

// Helper function to get sort text (for ordering suggestions)
function getSortText(kind: string, item: string): string {
    const prefix = kind === 'keyword' ? '0' :
                  kind === 'function' ? '1' :
                  kind === 'field' ? '2' : '9'
    return prefix + item.toLowerCase()
}

// Helper function to get item detail
function getItemDetail(kind: string, item: string): string {
    switch (kind) {
        case 'keyword':
            return 'SQL Keyword'
        case 'function':
            return 'Function'
        case 'field':
            return 'Log Field'
        case 'class':
            return 'Table'
        default:
            return ''
    }
}