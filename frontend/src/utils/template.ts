export type TemplateDialect = 'clickhouse-sql' | 'logsql' | 'logchefql';

export interface ExtractedVariable {
    name: string;
    isOptional: boolean;
}

const variablePattern = () => /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g;

function quotedOrCommentEnd(query: string, start: number, dialect: TemplateDialect): number {
    const rest = query.slice(start);
    if (dialect === 'clickhouse-sql' && rest.startsWith('$')) {
        const delimiter = rest.match(/^\$[A-Za-z0-9_]*\$/)?.[0];
        if (delimiter) {
            const end = query.indexOf(delimiter, start + delimiter.length);
            return end < 0 ? query.length : end + delimiter.length;
        }
    }
    if ((dialect === 'clickhouse-sql' && rest.startsWith('--')) || (dialect !== 'logchefql' && rest.startsWith('#'))) {
        const end = rest.search(/[\r\n]/);
        return end < 0 ? query.length : start + end;
    }
    if (dialect === 'clickhouse-sql' && rest.startsWith('/*')) {
        let depth = 1;
        for (let i = start + 2; i < query.length; i++) {
            if (query.startsWith('/*', i)) { depth++; i++; }
            else if (query.startsWith('*/', i)) {
                depth--; i++;
                if (depth === 0) return i + 1;
            }
        }
        return query.length;
    }
    const quote = query[start];
    if (quote !== "'" && quote !== '"' && (quote !== '`' || dialect === 'logchefql')) return start;
    for (let i = start + 1; i < query.length; i++) {
        if (query[i] === '\\' && (quote !== '`' || dialect === 'clickhouse-sql')) { i++; continue; }
        if (query[i] === quote) {
            if (dialect === 'clickhouse-sql' && query[i + 1] === quote) { i++; continue; }
            return i + 1;
        }
    }
    return query.length;
}

function optionalClauseEnd(query: string, start: number, dialect: TemplateDialect): number {
    let depth = 0;
    for (let i = start; i < query.length; i++) {
        const end = quotedOrCommentEnd(query, i, dialect);
        if (end > i) { i = end - 1; continue; }
        if (query[i] === '[') depth++;
        else if (query[i] === ']') {
            if (depth > 0) { depth--; continue; }
            return query.startsWith(']]', i) ? i : -1;
        }
    }
    return -1;
}

function hasVariable(content: string, dialect: TemplateDialect): boolean {
    for (let i = 0; i < content.length; i++) {
        const end = quotedOrCommentEnd(content, i, dialect);
        if (end > i) {
            if ("'\"`$".includes(content[i]) && variablePattern().test(content.slice(i, end))) return true;
            i = end - 1;
            continue;
        }
        if (content.startsWith('{{', i) && variablePattern().exec(content.slice(i))?.index === 0) return true;
    }
    return false;
}

export function extractTemplateVariables(query: string, dialect: TemplateDialect = 'clickhouse-sql'): ExtractedVariable[] {
    const variables = new Map<string, boolean>();
    const collect = (content: string, optional: boolean) => {
        for (const match of content.matchAll(variablePattern())) {
            const name = match[1];
            variables.set(name, optional && (variables.get(name) ?? true));
        }
    };
    const scan = (content: string, optional: boolean) => {
        let arrayDepth = 0;
        for (let i = 0; i < content.length;) {
            const end = quotedOrCommentEnd(content, i, dialect);
            if (end > i) {
                if ("'\"`$".includes(content[i])) collect(content.slice(i, end), optional);
                i = end;
                continue;
            }
            if ((dialect !== 'clickhouse-sql' || arrayDepth === 0) && content.startsWith('[[', i)) {
                const blockEnd = optionalClauseEnd(content, i + 2, dialect);
                if (blockEnd >= 0 && hasVariable(content.slice(i + 2, blockEnd), dialect)) {
                    scan(content.slice(i + 2, blockEnd), true);
                    i = blockEnd + 2;
                    continue;
                }
            }
            if (content.startsWith('{{', i)) {
                const match = variablePattern().exec(content.slice(i));
                if (match?.index === 0) {
                    collect(match[0], optional);
                    i += match[0].length;
                    continue;
                }
            }
            if (content[i] === '[') arrayDepth++;
            else if (content[i] === ']' && arrayDepth > 0) arrayDepth--;
            i++;
        }
    };
    scan(query, false);
    return Array.from(variables, ([name, isOptional]) => ({ name, isOptional }));
}
