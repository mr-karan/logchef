<script setup lang="ts">
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AlertFormState } from "@/composables/useAlertForm";

defineProps<{
  form: AlertFormState;
  disabled: boolean;
}>();
</script>

<template>
  <section class="space-y-4">
    <div class="grid gap-4 lg:grid-cols-3">
      <div class="space-y-2 lg:col-span-2">
        <Label for="alert-name">Alert name</Label>
        <Input id="alert-name" v-model="form.name" placeholder="High error rate alert" :disabled="disabled" />
      </div>
      <div class="space-y-2">
        <Label for="alert-severity">Severity</Label>
        <Select :model-value="form.severity" :disabled="disabled" @update:model-value="(value: any) => (form.severity = value)">
          <SelectTrigger id="alert-severity">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Severity</SelectLabel>
              <SelectItem value="info">Info</SelectItem>
              <SelectItem value="warning">Warning</SelectItem>
              <SelectItem value="critical">Critical</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </div>
    <div class="space-y-2">
      <Label for="alert-description">Description <span class="text-xs text-muted-foreground">(optional)</span></Label>
      <Textarea id="alert-description" v-model="form.description" placeholder="Provide context about when this alert should fire and what action to take" :rows="2" :disabled="disabled" />
    </div>
  </section>
</template>
