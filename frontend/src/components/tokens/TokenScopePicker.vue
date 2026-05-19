<script setup lang="ts">
import { computed } from "vue";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import {
  TOKEN_SCOPE_OPTIONS,
  TOKEN_SCOPE_PRESETS,
  matchingPreset,
  type TokenScope,
  type TokenScopePreset,
} from "@/lib/tokenScopes";

const model = defineModel<TokenScope[]>({ required: true });

const groupedScopes = computed(() => {
  const groups = new Map<string, typeof TOKEN_SCOPE_OPTIONS>();
  for (const option of TOKEN_SCOPE_OPTIONS) {
    const current = groups.get(option.group);
    if (current) {
      current.push(option);
    } else {
      groups.set(option.group, [option]);
    }
  }
  return Array.from(groups, ([name, scopes]) => ({ name, scopes }));
});

const selectedSet = computed(() => new Set(model.value));
const isFullAccess = computed(() => selectedSet.value.has("*"));
const activePreset = computed(() => matchingPreset(model.value));

function applyPreset(preset: TokenScopePreset) {
  model.value = [...preset.scopes];
}

function clearScopes() {
  model.value = [];
}

function toggleScope(scope: TokenScope, checked: boolean) {
  if (checked) {
    model.value = [...model.value.filter((value) => value !== "*" && value !== scope), scope];
    return;
  }
  model.value = model.value.filter((value) => value !== scope);
}

function handleScopeChecked(scope: TokenScope, checked: boolean | "indeterminate") {
  toggleScope(scope, checked === true);
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <Button
        v-for="preset in TOKEN_SCOPE_PRESETS"
        :key="preset.id"
        type="button"
        size="sm"
        :variant="activePreset?.id === preset.id ? 'default' : 'outline'"
        :title="preset.description"
        @click="applyPreset(preset)"
      >
        {{ preset.label }}
      </Button>
      <Button type="button" size="sm" variant="ghost" @click="clearScopes">
        Clear
      </Button>
      <Badge v-if="isFullAccess" variant="destructive">All scopes</Badge>
      <Badge v-else-if="model.length === 0" variant="outline">No scopes selected</Badge>
    </div>

    <div class="rounded-md border divide-y">
      <section v-for="group in groupedScopes" :key="group.name" class="p-3 space-y-3">
        <h4 class="text-sm font-medium">{{ group.name }}</h4>
        <div class="grid gap-3 sm:grid-cols-2">
          <label
            v-for="scope in group.scopes"
            :key="scope.value"
            class="flex items-start gap-3 rounded-md border p-3 text-sm hover:bg-muted/50"
            :class="{ 'opacity-50': isFullAccess }"
          >
            <Checkbox
              :model-value="isFullAccess || selectedSet.has(scope.value)"
              :disabled="isFullAccess"
              @update:model-value="handleScopeChecked(scope.value, $event)"
            />
            <span class="space-y-1">
              <span class="block font-medium">{{ scope.label }}</span>
              <span class="block text-xs text-muted-foreground">{{ scope.description }}</span>
            </span>
          </label>
        </div>
      </section>
    </div>
  </div>
</template>
