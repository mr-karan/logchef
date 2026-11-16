<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Mail, Plus, Webhook, X } from "lucide-vue-next";
import type { AcceptableValue } from "reka-ui";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { TeamMember } from "@/api/teams";
import { notificationDestinationsApi, type NotificationDestination } from "@/api/notificationDestinations";
import { getErrorMessage } from "@/api/types";
import DestinationForm from "./DestinationForm.vue";

defineProps<{
  disabled: boolean;
  teamMembers: TeamMember[];
  teamId: number | null;
}>();

const selected = defineModel<number[]>({ required: true });

const { t } = useI18n();
const destinations = ref<NotificationDestination[]>([]);
const loadError = ref<string | null>(null);
const creating = ref(false);

const byId = computed(() => new Map(destinations.value.map(d => [d.id, d])));
const available = computed(() => destinations.value.filter(d => !selected.value.includes(d.id)));

async function load() {
  loadError.value = null;
  try {
    const response = await notificationDestinationsApi.list();
    if (response.status === "success") destinations.value = response.data ?? [];
  } catch (err) {
    loadError.value = getErrorMessage(err) || t("alerts.loadDestinationsFailed");
  }
}

function add(value: AcceptableValue) {
  const id = Number(value);
  if (id && !selected.value.includes(id)) selected.value = [...selected.value, id];
}

function remove(id: number) {
  selected.value = selected.value.filter(existing => existing !== id);
}

async function onCreated(destination: NotificationDestination) {
  creating.value = false;
  await load();
  add(destination.id);
}

onMounted(load);
</script>

<template>
  <div class="space-y-3">
    <div class="flex gap-2">
      <Select :model-value="''" :disabled="disabled || available.length === 0" @update:model-value="add">
        <SelectTrigger class="w-full">
          <SelectValue :placeholder="t('alerts.addDestination')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="destination in available" :key="destination.id" :value="String(destination.id)">
            <div class="flex items-center gap-2">
              <Mail v-if="destination.type === 'email'" class="h-3 w-3" />
              <Webhook v-else class="h-3 w-3" />
              <span>{{ destination.name }}</span>
              <span v-if="destination.secret_hint" class="text-xs text-muted-foreground">{{ destination.secret_hint }}</span>
            </div>
          </SelectItem>
        </SelectContent>
      </Select>
      <Button type="button" variant="secondary" :disabled="disabled" @click="creating = true">
        <Plus class="mr-1 h-4 w-4" />
        {{ t("alerts.newDestination") }}
      </Button>
    </div>

    <p v-if="loadError" class="text-xs text-destructive">{{ loadError }}</p>

    <div v-if="selected.length > 0" class="flex flex-wrap gap-2">
      <Badge v-for="id in selected" :key="id" variant="secondary" class="flex items-center gap-1 font-normal">
        <Mail v-if="byId.get(id)?.type === 'email'" class="h-3 w-3 opacity-50" />
        <Webhook v-else class="h-3 w-3 opacity-50" />
        <span>{{ byId.get(id)?.name ?? t("alerts.destinationUnknown", { id }) }}</span>
        <span v-if="byId.get(id) && !byId.get(id)?.is_active" class="text-xs text-muted-foreground">{{ t("alerts.destinationInactive") }}</span>
        <button type="button" class="ml-1 hover:text-destructive" :aria-label="t('alerts.removeDestination')" :disabled="disabled" @click="remove(id)">
          <X class="h-3 w-3" />
        </button>
      </Badge>
    </div>
    <p v-else class="text-xs text-muted-foreground">{{ t("alerts.noDestinations") }}</p>

    <Dialog :open="creating" @update:open="(open) => (creating = open)">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("alerts.newDestination") }}</DialogTitle>
        </DialogHeader>
        <DestinationForm :team-members="teamMembers" :team-id="teamId" @saved="onCreated" @cancel="creating = false" />
      </DialogContent>
    </Dialog>
  </div>
</template>
