<script setup lang="ts">
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useI18n } from "vue-i18n";

const { t } = useI18n();


// Small reusable confirm dialog that wraps shadcn's AlertDialog with two-way
// `open` binding and a confirm-callback prop. Use for "are you sure?" prompts
// where the caller doesn't need extra body content beyond a description.
const props = withDefaults(
  defineProps<{
    open: boolean;
    title: string;
    description?: string;
    confirmText?: string;
    cancelText?: string;
    destructive?: boolean;
  }>(),
  {
    destructive: false,
  }
);

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

function handleConfirm() {
  emit("confirm");
  emit("update:open", false);
}

function handleCancel() {
  emit("cancel");
  emit("update:open", false);
}
</script>

<template>
  <AlertDialog :open="props.open" @update:open="emit('update:open', $event)">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription v-if="description">{{ description }}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel @click="handleCancel">{{ cancelText ?? t('ui.cancel') }}</AlertDialogCancel>
        <Button
          :variant="destructive ? 'destructive' : 'default'"
          @click="handleConfirm"
        >
          {{ confirmText ?? t('common.confirm') }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
