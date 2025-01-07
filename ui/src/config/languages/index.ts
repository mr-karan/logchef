import type * as Monaco from 'monaco-editor'
import { configureSQLLanguage } from './sql'
import { configureLogchefQLLanguage } from './logchefql'

// Track configured languages to prevent duplicate registration
const configuredLanguages = new Set<string>()

export function configureLanguages(monaco: typeof Monaco) {
    console.log('🌟 Configuring languages...')
    const disposers: { sql?: () => void, logchefql?: () => void } = {}

    // Only configure each language once
    if (!configuredLanguages.has('clickhouse-sql')) {
        console.log('🔄 Configuring clickhouse-sql...')
        disposers.sql = configureSQLLanguage(monaco)
        configuredLanguages.add('clickhouse-sql')
        console.log('✅ clickhouse-sql configured')
    } else {
        console.log('⏭️ clickhouse-sql already configured')
    }

    if (!configuredLanguages.has('logchefql')) {
        console.log('🔄 Configuring logchefql...')
        disposers.logchefql = configureLogchefQLLanguage(monaco)
        configuredLanguages.add('logchefql')
        console.log('✅ logchefql configured')
    } else {
        console.log('⏭️ logchefql already configured')
    }

    console.log('✨ Languages configuration complete')
    return disposers
}