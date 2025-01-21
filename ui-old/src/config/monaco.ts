import * as monaco from 'monaco-editor'

export function initMonaco() {
    // Register SQL language with Clickhouse keywords
    monaco.languages.register({ id: 'clickhouse' })
    monaco.languages.setMonarchTokensProvider('clickhouse', {
        defaultToken: '',
        tokenPostfix: '.sql',
        keywords: [
            'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'LIMIT',
            'HAVING', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER', 'ON',
            'WITH', 'UNION', 'ALL', 'AS', 'DISTINCT', 'PREWHERE',
            'FINAL', 'SAMPLE', 'ARRAY', 'TUPLE', 'MAP', 'SETTINGS',
            'FORMAT', 'USE', 'DESCRIBE', 'DESC', 'EXPLAIN', 'OPTIMIZE',
            'AND', 'OR', 'NOT', 'IN', 'LIKE', 'ILIKE', 'GLOBAL'
        ],
        operators: [
            '=', '>', '<', '!', '~', '?', ':',
            '==', '<=', '>=', '!=', '&&', '||', '++',
            '--', '+', '-', '*', '/', '%'
        ],
        symbols: /[=><!~?:&|+\-*\/\^%]+/,

        tokenizer: {
            root: [
                { include: '@whitespace' },
                { include: '@numbers' },
                { include: '@strings' },
                { include: '@comments' },

                [/[a-zA-Z_]\w*/, {
                    cases: {
                        '@keywords': 'keyword',
                        '@default': 'identifier'
                    }
                }],
                [/@symbols/, {
                    cases: {
                        '@operators': 'operator',
                        '@default': ''
                    }
                }]
            ],

            whitespace: [[/\s+/, 'white']],
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
                [/\d*\.\d+([eE][-+]?\d+)?/, 'number.float'],
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
}