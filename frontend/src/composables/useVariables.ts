import {useVariableStore, type VariableState} from "@/stores/variables.ts";
import {storeToRefs} from "pinia";
import type { TemplateVariable } from "@/api/logchefql";
import { extractTemplateVariables, type TemplateDialect } from "@/utils/template";
export type { ExtractedVariable } from "@/utils/template";

export const extractVariablesWithOptional = extractTemplateVariables;

export function useVariables() {
    const variableStore = useVariableStore();
    const { allVariables } = storeToRefs(variableStore);

    const resolveVariableValue = (variable: VariableState) => {
        const value = variable.value;
        
        // Handle array values (multi-select)
        if (Array.isArray(value)) {
            if (value.length > 0) return value;
            // Fall back to defaultValue if current value is empty array
            if (Array.isArray(variable.defaultValue) && variable.defaultValue.length > 0) {
                return variable.defaultValue;
            }
            return value;
        }
        
        if (value !== '' && value !== null && value !== undefined) {
            return value;
        }
        return variable.defaultValue ?? value;
    };

    const escapeSqlString = (value: string): string => {
        return value.replace(/'/g, "''");
    };

    const convertVariables = (sql: string): string => {
        for (const variable of allVariables.value) {
            const key = variable.name;
            const value = resolveVariableValue(variable);

            let formattedValue: string;
            if (variable.type === 'number') {
                formattedValue = String(value);
            } else if (variable.type === 'date') {
                const dateStr = value ? new Date(value as string).toISOString() : '';
                formattedValue = `'${escapeSqlString(dateStr)}'`;
            } else {
                formattedValue = `'${escapeSqlString(String(value ?? ''))}'`;
            }

            // Fixed regex: use \\s for whitespace matching in template literals
            const originalRegex = new RegExp(`\\{\\{\\s*${key}\\s*\\}\\}`, 'g');
            const placeholderRegex = new RegExp(`'__VAR_${key}__'`, 'g');
            const unquotedPlaceholderRegex = new RegExp(`__VAR_${key}__`, 'g');

            sql = sql.replace(originalRegex, formattedValue);
            sql = sql.replace(placeholderRegex, formattedValue);
            sql = sql.replace(unquotedPlaceholderRegex, formattedValue);
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
            value: resolveVariableValue(v)
        }));
    };

    const ensureVariablesFromSql = (sql: string, dialect: TemplateDialect = 'clickhouse-sql'): string[] => {
        const extractedVars = extractVariablesWithOptional(sql, dialect);
        const newlyCreated: string[] = [];

        for (const { name, isOptional } of extractedVars) {
            const existing = variableStore.getVariableByName(name);
            if (!existing) {
                const newVar: VariableState = {
                    name,
                    type: 'text',
                    label: name,
                    inputType: 'input',
                    value: '',
                    isOptional
                };
                variableStore.upsertVariable(newVar);
                newlyCreated.push(name);
            } else if (existing.isOptional !== isOptional) {
                variableStore.upsertVariable({ ...existing, isOptional });
            }
        }

        return newlyCreated;
    };

    const validateVariablesForSql = (sql: string, dialect: TemplateDialect = 'clickhouse-sql'): { hasVariables: boolean; missingValues: string[] } => {
        const extractedVars = extractVariablesWithOptional(sql, dialect);
        if (extractedVars.length === 0) {
            return { hasVariables: false, missingValues: [] };
        }

        const missingValues: string[] = [];
        for (const { name, isOptional } of extractedVars) {
            if (isOptional) continue;
            
            const variable = variableStore.getVariableByName(name);
            if (!variable) {
                missingValues.push(name);
                continue;
            }
            const resolvedValue = resolveVariableValue(variable);
            
            const isEmpty = Array.isArray(resolvedValue) 
                ? resolvedValue.length === 0
                : resolvedValue === '' || resolvedValue === null || resolvedValue === undefined;
            
            if (isEmpty) {
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
        extractVariablesWithOptional,
        allVariables
    };
}
