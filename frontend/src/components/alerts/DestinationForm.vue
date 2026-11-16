<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Loader2 } from "lucide-vue-next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { TeamMember } from "@/api/teams";
import {
  notificationDestinationsApi,
  type DestinationRequest,
  type DestinationScope,
  type DestinationTestResult,
  type DestinationType,
  type NotificationDestination,
} from "@/api/notificationDestinations";
import { getErrorMessage } from "@/api/types";

const props = defineProps<{
  teamMembers: TeamMember[];
  teamId: number | null;
}>();

const emit = defineEmits<{
  (e: "saved", destination: NotificationDestination): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();

const form = reactive({
  name: "",
  type: "webhook" as DestinationType,
  scope: "private" as DestinationScope,
  userIds: [] as number[],
  addresses: "",
  url: "",
});
const error = ref<string | null>(null);
const saving = ref(false);
const testing = ref(false);
const testResults = ref<DestinationTestResult[] | null>(null);

const scopes = computed(() => {
  const options: Array<{ value: DestinationScope; label: string }> = [
    { value: "private", label: t("destinations.scopePrivate") },
  ];
  if (props.teamId) options.push({ value: "team", label: t("destinations.scopeTeam") });
  options.push({ value: "global", label: t("destinations.scopeGlobal") });
  return options;
});

function request(): DestinationRequest {
  const base = {
    name: form.name.trim(),
    type: form.type,
    scope: form.scope,
    team_id: form.scope === "team" && props.teamId ? props.teamId : undefined,
  };
  if (form.type === "email") {
    const addresses = form.addresses.split(/[\s,]+/).map(a => a.trim()).filter(Boolean);
    return { ...base, config: { user_ids: [...form.userIds], addresses }, secret: "" };
  }
  return { ...base, config: { payload_version: 1 }, secret: form.url.trim() };
}

const canSubmit = computed(() => {
  if (!form.name.trim()) return false;
  if (form.type === "webhook") return form.url.trim() !== "";
  return form.userIds.length > 0 || form.addresses.trim() !== "";
});

function toggleUser(userId: number, checked: boolean) {
  form.userIds = checked ? [...form.userIds, userId] : form.userIds.filter(id => id !== userId);
}

async function sendTest() {
  testing.value = true;
  error.value = null;
  testResults.value = null;
  try {
    const response = await notificationDestinationsApi.test(request());
    if (response.status === "success" && response.data) testResults.value = response.data.results;
  } catch (err) {
    error.value = getErrorMessage(err) || t("destinations.testFailed");
  } finally {
    testing.value = false;
  }
}

async function save() {
  saving.value = true;
  error.value = null;
  try {
    const response = await notificationDestinationsApi.create(request());
    if (response.status === "success" && response.data) emit("saved", response.data);
  } catch (err) {
    error.value = getErrorMessage(err) || t("destinations.saveFailed");
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <form class="space-y-4" @submit.prevent="save">
    <div class="space-y-2">
      <Label for="destination-name">{{ t("destinations.name") }}</Label>
      <Input id="destination-name" v-model="form.name" />
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <div class="space-y-2">
        <Label>{{ t("destinations.type") }}</Label>
        <Select v-model="form.type">
          <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="webhook">{{ t("destinations.typeWebhook") }}</SelectItem>
            <SelectItem value="email">{{ t("destinations.typeEmail") }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-2">
        <Label>{{ t("destinations.scope") }}</Label>
        <Select v-model="form.scope">
          <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in scopes" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
    </div>

    <div v-if="form.type === 'webhook'" class="space-y-2">
      <Label for="destination-url">{{ t("destinations.webhookURL") }}</Label>
      <Input id="destination-url" v-model="form.url" type="url" placeholder="https://api.example.com/hooks/..." autocomplete="off" />
    </div>

    <template v-else>
      <div class="space-y-2">
        <Label>{{ t("destinations.recipients") }}</Label>
        <div class="max-h-40 space-y-2 overflow-y-auto rounded-md border p-2">
          <label v-for="member in teamMembers" :key="member.user_id" class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="form.userIds.includes(member.user_id)" @update:model-value="(value) => toggleUser(member.user_id, value === true)" />
            <span>{{ member.full_name || member.email }}</span>
          </label>
        </div>
      </div>
      <div class="space-y-2">
        <Label for="destination-addresses">{{ t("destinations.addresses") }}</Label>
        <Textarea id="destination-addresses" v-model="form.addresses" rows="2" placeholder="oncall@example.com" />
      </div>
    </template>

    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    <ul v-if="testResults" class="space-y-1 text-sm">
      <li v-for="(result, index) in testResults" :key="index" :class="result.ok ? 'text-muted-foreground' : 'text-destructive'">
        {{ result.target }}: {{ result.ok ? t("destinations.testSent") : result.error }}
      </li>
    </ul>

    <div class="flex justify-end gap-2">
      <Button type="button" variant="ghost" @click="emit('cancel')">{{ t("ui.cancel") }}</Button>
      <Button type="button" variant="outline" :disabled="!canSubmit || testing" @click="sendTest">
        <Loader2 v-if="testing" class="mr-2 h-4 w-4 animate-spin" />
        {{ t("destinations.test") }}
      </Button>
      <Button type="submit" :disabled="!canSubmit || saving">
        <Loader2 v-if="saving" class="mr-2 h-4 w-4 animate-spin" />
        {{ t("destinations.save") }}
      </Button>
    </div>
  </form>
</template>
