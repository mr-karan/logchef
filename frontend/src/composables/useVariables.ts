import {useVariableStore} from "@/stores/variables.ts";
import {storeToRefs} from "pinia";



export function useVariables() {
    const variableStore = useVariableStore();
    const { allVariables } = storeToRefs(variableStore);

    /**
     * convert {{variable}} format to user input
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
            
            sql = sql.replace(originalRegex, formattedValue as string);
            sql = sql.replace(placeholderRegex, formattedValue as string);
        }

        return sql;
    };

    return {
        convertVariables
    };
}
