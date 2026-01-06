import {useVariableStore, type VariableState} from "@/stores/variables.ts";
import {storeToRefs} from "pinia";
import type { TemplateVariable } from "@/api/explore";

// Regex to match {{variable_name}} with optional whitespace
const VARIABLE_PATTERN = /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g;

/**
 * Extract unique variable names from SQL string
 */
export function extractVariableNames(sql: string): string[] {
    const matches = sql.matchAll(VARIABLE_PATTERN);
    const seen = new Set<string>();
    const names: string[] = [];

    for (const match of matches) {
        const name = match[1];
        if (!seen.has(name)) {
            names.push(name);
            seen.add(name);
        }
    }
    return names;
}

export function useVariables() {
    const variableStore = useVariableStore();
    const { allVariables } = storeToRefs(variableStore);

    /**
     * convert {{variable}} format to user input (for local/display purposes)
     * @param sql origin query
     * @returns converted query
     */
    const convertVariables = (sql: string): string => {
        for (const variable of allVariables.value) {
            const key = variable.name;
            const value = variable.value;

            const formattedValue =
                variable.type === 'number'
                    ? value
                    : variable.type === 'date'
                        ? `'${new Date(value).toISOString()}'`
                        : `'${value}'`;

            // Replace both original {{variable}} syntax and translated __VAR_variable__ placeholders
            const originalRegex = new RegExp(`{{\\s*${key}\\s*}}`, 'g');
            const placeholderRegex = new RegExp(`'__VAR_${key}__'`, 'g');
            const unquotedPlaceholderRegex = new RegExp(`__VAR_${key}__`, 'g');

            sql = sql.replace(originalRegex, formattedValue as string);
            sql = sql.replace(placeholderRegex, formattedValue as string);
            sql = sql.replace(unquotedPlaceholderRegex, formattedValue as string);
        }

        return sql;
    };

    /**
     * Get variables in the format expected by the API for backend substitution.
     * This allows the backend to safely substitute variables with proper escaping.
     * @returns Array of template variables for API use
     */
    const getVariablesForApi = (): TemplateVariable[] => {
        return allVariables.value.map(v => ({
            name: v.name,
            type: v.type as 'text' | 'number' | 'date' | 'string',
            value: v.value
        }));
    };

    /**
     * Ensure that all variables referenced in the SQL exist in the store.
     * Creates placeholder variables with empty values for any missing ones.
     * @param sql SQL string to extract variables from
     * @returns Names of variables that were newly created (had no value)
     */
    const ensureVariablesFromSql = (sql: string): string[] => {
        const requiredNames = extractVariableNames(sql);
        const newlyCreated: string[] = [];

        for (const name of requiredNames) {
            const existing = variableStore.getVariableByName(name);
            if (!existing) {
                // Create a placeholder variable with empty value
                const newVar: VariableState = {
                    name,
                    type: 'text',
                    label: name,
                    inputType: 'input',
                    value: ''
                };
                variableStore.upsertVariable(newVar);
                newlyCreated.push(name);
            }
        }

        return newlyCreated;
    };

    /**
     * Check if SQL has variables and if all required variables have non-empty values.
     * @param sql SQL to check
     * @returns Object with hasVariables flag and list of missing variable names
     */
    const validateVariablesForSql = (sql: string): { hasVariables: boolean; missingValues: string[] } => {
        const requiredNames = extractVariableNames(sql);
        if (requiredNames.length === 0) {
            return { hasVariables: false, missingValues: [] };
        }

        const missingValues: string[] = [];
        for (const name of requiredNames) {
            const variable = variableStore.getVariableByName(name);
            if (!variable || variable.value === '' || variable.value === null || variable.value === undefined) {
                missingValues.push(name);
            }
        }

        return { hasVariables: true, missingValues };
    };

    return {
        convertVariables,
        getVariablesForApi,
        ensureVariablesFromSql,
        validateVariablesForSql,
        allVariables
    };
}
