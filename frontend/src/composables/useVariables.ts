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

            const regex = new RegExp(`{{\\s*${key}\\s*}}`, 'g');
            sql = sql.replace(regex, formattedValue as string);
        }

        return sql;
    };

    return {
        convertVariables
    };
}
