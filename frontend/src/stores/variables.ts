import { defineStore } from "pinia";
import { ref, computed } from "vue";

export interface VariableOption {
    value: string;
    label?: string;
}

export interface VariableState {
    name: string;
    type: 'text' | 'number' | 'date';
    label: string;
    inputType: 'input' | 'dropdown' | 'multiselect' | 'search';
    value: string | number | string[];
    defaultValue?: string | number | string[];
    isOptional?: boolean;
    isRequired?: boolean;
    options?: VariableOption[];
}

export const useVariableStore = defineStore("variable", () => {

    const variables = ref<VariableState[]>([]);

    // get all list of variable
    const allVariables = computed(() => variables.value);

    // search on variable
    function getVariableByName(name: string): VariableState | undefined {
        return variables.value.find(v => v.name === name);
    }

    function setAllVariable(newVars: VariableState[]) {
        variables.value = newVars;
    }

    // add or update
    function upsertVariable(newVariable: VariableState) {
        const index = variables.value.findIndex(v => v.name === newVariable.name);
        if (index !== -1) {
            variables.value[index] = newVariable;
        } else {
            variables.value.push(newVariable);
        }
    }

    // remove
    function removeVariable(name: string) {
        variables.value = variables.value.filter(v => v.name !== name);
    }

    return {
        allVariables,
        getVariableByName,
        setAllVariable,
        upsertVariable,
        removeVariable,
    };
});
