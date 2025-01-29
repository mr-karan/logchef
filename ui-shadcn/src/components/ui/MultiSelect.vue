<!-- MultiSelect.vue -->
<script setup lang="ts">
import { ref, onMounted } from "vue";
import { ChevronDown, Check } from "lucide-vue-next";
import { debounce } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    Command,
    CommandEmpty,
    CommandGroup,
    CommandInput,
    CommandItem,
    CommandList,
    CommandSeparator,
} from "@/components/ui/command";
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";
import { Label } from "@/components/ui/label";

const model = defineModel();

interface Option {
    value: string;
    label: string;
    type?: string;
}

const props = defineProps({
    title: {
        type: String,
        required: true,
    },
    options: {
        type: Array as () => Option[],
        default: () => [],
    },
    placeholder: {
        type: String,
        default: "Select items...",
    },
    searchPlaceholder: {
        type: String,
        default: "Search...",
    }
});

const selectedValues = ref(new Set(model.value));
const optionsList = ref<Option[]>([]);
const q = ref("");

onMounted(() => {
    // Convert the options array to the required format
    optionsList.value = props.options.map(opt => ({
        value: typeof opt === 'string' ? opt : opt.name,
        label: typeof opt === 'string' ? opt : opt.name,
        type: typeof opt === 'string' ? undefined : opt.type
    }));
});

const toggleSelection = (optionValue: string) => {
    if (selectedValues.value.has(optionValue)) {
        selectedValues.value.delete(optionValue);
        model.value = Array.from(selectedValues.value);
    } else {
        selectedValues.value.add(optionValue);
        model.value = Array.from(selectedValues.value);
    }
};

const clearSelections = () => {
    selectedValues.value.clear();
    model.value = [];
};
</script>

<template>
    <div class="flex flex-col gap-2">
        <Label v-if="title">{{ title }}</Label>
        <Popover>
            <PopoverTrigger as-child>
                <Button variant="outline"
                    class="h-10 w-full flex justify-between text-muted-foreground font-normal">
                    <span v-if="selectedValues.size < 1">{{ placeholder }}</span>
                    <template v-if="selectedValues.size > 0">
                        <Badge variant="secondary" class="rounded-sm px-1 font-normal lg:hidden">
                            {{ selectedValues.size }} selected
                        </Badge>
                        <div class="hidden space-x-1 lg:flex">
                            <Badge v-if="selectedValues.size > 3" variant="secondary"
                                class="rounded-sm px-1 font-normal">
                                {{ selectedValues.size }} selected
                            </Badge>
                            <template v-else>
                                <Badge v-for="option in optionsList.filter(
                                    (option) => selectedValues.has(option.value)
                                )" :key="option.value" variant="secondary" class="rounded-sm px-1 font-normal">
                                    {{ option.label }}
                                    <span v-if="option.type" class="ml-1 text-xs opacity-60">({{ option.type }})</span>
                                </Badge>
                            </template>
                        </div>
                    </template>
                    <ChevronDown class="ml-2 h-4 w-4" />
                </Button>
            </PopoverTrigger>
            <PopoverContent class="w-[300px] p-0" align="start">
                <Command>
                    <CommandInput :placeholder="searchPlaceholder" v-model="q" />
                    <CommandList>
                        <CommandEmpty>No results found.</CommandEmpty>
                        <CommandGroup>
                            <CommandItem v-for="option in optionsList.filter(opt => 
                                opt.label.toLowerCase().includes(q.toLowerCase())
                            )" :key="option.value" :value="option"
                                @select="toggleSelection(option.value)">
                                <div :class="[
                                    'mr-2 flex h-4 w-4 items-center justify-center rounded-sm border border-primary',
                                    selectedValues.has(option.value)
                                        ? 'bg-primary text-primary-foreground'
                                        : 'opacity-50 [&_svg]:invisible',
                                ]">
                                    <Check class="h-4 w-4" />
                                </div>
                                <span>{{ option.label }}</span>
                                <span v-if="option.type" class="ml-2 text-xs text-muted-foreground">({{ option.type
                                }})</span>
                            </CommandItem>
                        </CommandGroup>
                        <template v-if="selectedValues.size > 0">
                            <CommandSeparator />
                            <CommandGroup>
                                <CommandItem class="justify-center text-center" @select="clearSelections">
                                    Clear Selection
                                </CommandItem>
                            </CommandGroup>
                        </template>
                    </CommandList>
                </Command>
            </PopoverContent>
        </Popover>
    </div>
</template>
