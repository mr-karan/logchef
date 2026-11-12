<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Copy, Download, Upload } from "lucide-vue-next";
import type { Dashboard } from "@/api/dashboards";
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone } from "@/utils/timeWindow";

// Without `latest` the dialog offers the three ways out of a refused save. With it,
// the dialog confirms overwriting that version.
const props = defineProps<{
  open: boolean;
  latest: Dashboard | null;
  busy: boolean;
}>();

const emit = defineEmits<{
  (e: "keepEditing"): void;
  (e: "saveCopy"): void;
  (e: "reviewOverwrite"): void;
  (e: "confirmOverwrite"): void;
  (e: "back"): void;
  (e: "discard"): void;
}>();

const { t, locale } = useI18n();
const preferences = usePreferencesStore();

const savedAt = computed(() => {
  const ts = props.latest?.updated_at;
  const date = ts ? new Date(ts) : null;
  if (!date || Number.isNaN(date.getTime())) return "";
  return formatInZone(epochMs(date), preferences.zone, { dateStyle: "medium", timeStyle: "medium" }, locale.value);
});
</script>

<template>
  <AlertDialog :open="props.open" @update:open="(open) => { if (!open && !props.busy) emit('keepEditing'); }">
    <AlertDialogContent>
      <template v-if="props.latest">
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t('dashboards.conflict.overwriteTitle') }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ savedAt
              ? t('dashboards.conflict.overwriteDescription', { revision: props.latest.revision, savedAt })
              : t('dashboards.conflict.overwriteDescriptionNoTime', { revision: props.latest.revision }) }}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button variant="outline" :disabled="props.busy" @click="emit('back')">{{ t('dashboards.conflict.back') }}</Button>
          <Button variant="destructive" :disabled="props.busy" @click="emit('confirmOverwrite')">
            {{ t('dashboards.conflict.confirmOverwrite') }}
          </Button>
        </AlertDialogFooter>
      </template>
      <template v-else>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t('dashboards.conflict.title') }}</AlertDialogTitle>
          <AlertDialogDescription>{{ t('dashboards.conflict.description') }}</AlertDialogDescription>
        </AlertDialogHeader>
        <div class="grid gap-2">
          <Button variant="outline" class="justify-start gap-2" :disabled="props.busy" @click="emit('saveCopy')">
            <Copy class="h-4 w-4" />
            {{ t('dashboards.conflict.saveCopy') }}
          </Button>
          <Button variant="outline" class="justify-start gap-2" :disabled="props.busy" @click="emit('reviewOverwrite')">
            <Upload class="h-4 w-4" />
            {{ t('dashboards.conflict.overwrite') }}
          </Button>
          <Button variant="outline" class="justify-start gap-2" :disabled="props.busy" @click="emit('discard')">
            <Download class="h-4 w-4" />
            {{ t('dashboards.conflict.discard') }}
          </Button>
        </div>
        <AlertDialogFooter>
          <Button variant="ghost" :disabled="props.busy" @click="emit('keepEditing')">{{ t('dashboards.conflict.keepEditing') }}</Button>
        </AlertDialogFooter>
      </template>
    </AlertDialogContent>
  </AlertDialog>
</template>
