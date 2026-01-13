<script setup lang="ts">
import { ref } from "vue";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { settingsApi } from "@/api/settings";
import type { AlertmanagerRoutingInfo, LabelExample } from "@/api/settings";

defineProps<{
  disabled?: boolean;
}>();

const emit = defineEmits<{
  (e: "select-labels", payload: { labels: Record<string, string> }): void;
}>();

const isOpen = ref(false);
const isLoading = ref(false);
const error = ref<string | null>(null);
const routingInfo = ref<AlertmanagerRoutingInfo | null>(null);

async function fetchRoutingInfo() {
  if (routingInfo.value) return; // Already loaded

  isLoading.value = true;
  error.value = null;
  try {
    const response = await settingsApi.getAlertmanagerRouting();
    routingInfo.value = response.data;
  } catch (err: any) {
    error.value = err.message || "Failed to load routing information";
  } finally {
    isLoading.value = false;
  }
}

function handleOpenChange(open: boolean) {
  isOpen.value = open;
  if (open) {
    fetchRoutingInfo();
  }
}

function handleSelect(example: LabelExample) {
  emit("select-labels", { labels: { ...example.labels } });
}

// Group examples or just list them? The API returns a flat list of label_examples.
// We'll display them as provided.
</script>

<template>
  <div class="space-y-2">
    <Collapsible
      v-model:open="isOpen"
      @update:open="handleOpenChange"
      class="w-full"
    >
      <div class="flex items-center justify-between">
        <CollapsibleTrigger as-child>
          <Button
            variant="ghost"
            size="sm"
            class="p-0 h-auto font-normal text-muted-foreground hover:text-foreground flex items-center gap-1"
            :disabled="disabled"
          >
            <span v-if="isOpen">Hide routing guide</span>
            <span v-else>Show routing guide</span>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              class="transition-transform duration-200"
              :class="{ 'rotate-180': isOpen }"
            >
              <path d="m6 9 6 6 6-6" />
            </svg>
          </Button>
        </CollapsibleTrigger>
      </div>

      <CollapsibleContent class="mt-4">
        <div class="rounded-lg border bg-muted/20 p-4">
          <div class="space-y-4">
            <div>
              <h4 class="text-sm font-medium">Alertmanager Routing</h4>
              <p class="text-xs text-muted-foreground mt-1">
                Select a receiver to auto-fill the correct routing labels.
              </p>
            </div>

            <!-- Loading State -->
            <div v-if="isLoading" class="space-y-2">
              <Skeleton class="h-10 w-full" />
              <Skeleton class="h-10 w-full" />
              <Skeleton class="h-10 w-full" />
            </div>

            <!-- Error State -->
            <Alert v-else-if="error" variant="destructive">
              <AlertDescription class="flex items-center justify-between">
                <span>{{ error }}</span>
                <Button
                  variant="outline"
                  size="sm"
                  class="h-7 bg-background text-foreground border-destructive/50 hover:bg-destructive/10"
                  @click="fetchRoutingInfo"
                >
                  Retry
                </Button>
              </AlertDescription>
            </Alert>

            <!-- Content -->
            <Accordion
              v-else-if="routingInfo && routingInfo.label_examples.length > 0"
              type="single"
              collapsible
              class="w-full bg-background rounded-md border"
            >
              <AccordionItem
                v-for="(example, index) in routingInfo.label_examples"
                :key="index"
                :value="`item-${index}`"
                class="px-3"
              >
                <AccordionTrigger class="text-sm py-3 hover:no-underline">
                  <div class="flex items-center gap-2">
                    <span class="font-medium">{{ example.receiver }}</span>
                    <span
                      v-if="example.receiver === routingInfo.default_route"
                      class="text-[10px] uppercase tracking-wider bg-muted px-1.5 py-0.5 rounded text-muted-foreground"
                    >
                      Default
                    </span>
                  </div>
                </AccordionTrigger>
                <AccordionContent class="pb-3 pt-1">
                  <div class="space-y-3">
                    <p v-if="example.description" class="text-xs text-muted-foreground">
                      {{ example.description }}
                    </p>

                    <div class="flex flex-wrap gap-1.5">
                      <Badge
                        v-for="(val, key) in example.labels"
                        :key="key"
                        variant="secondary"
                        class="text-xs font-mono font-normal"
                      >
                        {{ key }}={{ val }}
                      </Badge>
                    </div>

                    <div class="pt-1">
                      <Button
                        size="sm"
                        variant="outline"
                        class="h-7 text-xs"
                        @click="handleSelect(example)"
                        :disabled="disabled"
                      >
                        Use these labels
                      </Button>
                    </div>
                  </div>
                </AccordionContent>
              </AccordionItem>
            </Accordion>

            <div
              v-else-if="routingInfo"
              class="text-sm text-muted-foreground py-2 text-center italic"
            >
              No routing examples available.
            </div>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  </div>
</template>
