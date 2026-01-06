import {useVariableStore} from "@/stores/variables.ts";
import {storeToRefs} from "pinia";
import type { TemplateVariable } from "@/api/explore";



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

    return {
        convertVariables,
        getVariablesForApi,
        allVariables
    };
}
