<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { X, Bell } from "lucide-vue-next";
import type { TeamMember } from "@/api/teams";
import type { AlertFormState } from "@/composables/useAlertForm";
import DestinationPicker from "./DestinationPicker.vue";

const { t } = useI18n();

defineProps<{
  form: AlertFormState;
  disabled: boolean;
  teamMembers: TeamMember[];
  teamId: number | null;
  onAddLabel: () => void;
  onRemoveLabel: (id: number) => void;
  onAddAnnotation: () => void;
  onRemoveAnnotation: (id: number) => void;
}>();
</script>

<template>
  <section class="space-y-6 border-t pt-4">
    <div>
      <h3 class="text-sm font-semibold flex items-center gap-2">
        <Bell class="h-4 w-4" />
        {{ t('ui.notificationsRouting') }}
      </h3>
      <p class='text-xs text-muted-foreground mt-1'>{{ t('ui.configureWhereAlertsShouldBeSentWhenTriggered') }}</p>
    </div>

    <div class="space-y-3">
      <Label class='text-xs font-medium'>{{ t('alerts.destinations') }}</Label>
      <DestinationPicker v-model="form.destination_ids" :disabled="disabled" :team-members="teamMembers" :team-id="teamId" />
    </div>

    <!-- Metadata -->
    <div class="grid gap-4 md:grid-cols-2">
      <!-- Labels -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class='text-xs font-medium'>{{ t('ui.labels') }} <span class='font-normal text-muted-foreground ml-1'>{{ t('ui.grouping2') }}</span></Label>
          <Button type="button" variant="outline" size="sm" @click="onAddLabel" :disabled="disabled">
            {{ t('ui.addLabel') }}
          </Button>
        </div>
        <div class="space-y-2">
          <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
            <Input v-model="label.key" :placeholder="t('ui.key')" class="flex-1" :disabled="disabled" />
            <Input v-model="label.value" :placeholder="t('ui.value2')" class="flex-1" :disabled="disabled" />
            <Button type="button" variant="ghost" size="icon" @click="onRemoveLabel(label.id)" :disabled="disabled">
              <X class="h-4 w-4" />
            </Button>
          </div>
          <p v-if="form.labels.length === 0" class='text-xs text-muted-foreground'>{{ t('ui.noCustomLabels') }}</p>
        </div>
      </div>

      <!-- Annotations -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class='text-xs font-medium'>{{ t('ui.annotations') }} <span class='font-normal text-muted-foreground ml-1'>{{ t('ui.context') }}</span></Label>
          <Button type="button" variant="outline" size="sm" @click="onAddAnnotation" :disabled="disabled">
            {{ t('ui.addAnnotation') }}
          </Button>
        </div>
        <div class="space-y-2">
          <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
            <Input v-model="annotation.key" :placeholder="t('ui.key')" class="flex-1" :disabled="disabled" />
            <Input v-model="annotation.value" :placeholder="t('ui.value2')" class="flex-1" :disabled="disabled" />
            <Button type="button" variant="ghost" size="icon" @click="onRemoveAnnotation(annotation.id)" :disabled="disabled">
              <X class="h-4 w-4" />
            </Button>
          </div>
          <p v-if="form.annotations.length === 0" class='text-xs text-muted-foreground'>{{ t('ui.noCustomAnnotations') }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
