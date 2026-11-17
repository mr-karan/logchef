<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Loader2 } from "lucide-vue-next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { TeamMember } from "@/api/teams";
import type {
  DestinationRequest,
  DestinationScope,
  DestinationTestResult,
  DestinationType,
  NotificationDestination,
} from "@/api/notificationDestinations";
import { useNotificationDestinationsStore } from "@/stores/notificationDestinations";

const props = defineProps<{
  teamMembers: TeamMember[];
  /** The team a new team-scoped destination belongs to. */
  teamId: number | null;
  /** The destination to edit; absent creates one. */
  destination?: NotificationDestination | null;
  /** A destination to copy into a new one. Its secret is not copied. */
  copyOf?: NotificationDestination | null;
}>();

const emit = defineEmits<{
  (e: "saved", destination: NotificationDestination): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();
const store = useNotificationDestinationsStore();

const editing = computed(() => props.destination ?? null);
const base = props.destination ?? props.copyOf ?? null;
const form = reactive({
  name: props.copyOf ? t("common.copyName", { name: props.copyOf.name }) : base?.name ?? "",
  type: (base?.type ?? "webhook") as DestinationType,
  scope: (base?.scope ?? "private") as DestinationScope,
  userIds: [...(base?.config.user_ids ?? [])],
  addresses: (base?.config.addresses ?? []).join(", "),
  // The API never returns the URL; on edit a blank one keeps the stored URL.
  url: "",
  isActive: base?.is_active ?? true,
});
const error = ref<string | null>(null);
const saving = ref(false);
const testing = ref(false);
const testResults = ref<DestinationTestResult[] | null>(null);

// A team destination keeps its team; a new one joins the team the form was opened in.
const teamFor = computed(() => (base?.scope === "team" && base.team_id ? base.team_id : props.teamId));

const scopes = computed(() => {
  const options: Array<{ value: DestinationScope; label: string }> = [
    { value: "private", label: t("destinations.scopePrivate") },
  ];
  if (teamFor.value) options.push({ value: "team", label: t("destinations.scopeTeam") });
  options.push({ value: "global", label: t("destinations.scopeGlobal") });
  return options;
});

function request(): DestinationRequest {
  const base = {
    name: form.name.trim(),
    type: form.type,
    scope: form.scope,
    team_id: form.scope === "team" && teamFor.value ? teamFor.value : undefined,
    is_active: form.isActive,
  };
  if (form.type === "email") {
    const addresses = form.addresses.split(/[\s,]+/).map(a => a.trim()).filter(Boolean);
    return { ...base, config: { user_ids: [...form.userIds], addresses }, secret: "" };
  }
  return { ...base, config: { payload_version: 1 }, secret: form.url.trim() };
}

const canSubmit = computed(() => {
  if (!form.name.trim()) return false;
  if (form.type === "webhook") return !!editing.value || form.url.trim() !== "";
  return form.userIds.length > 0 || form.addresses.trim() !== "";
});

// Recipients the member list does not show, such as users of another team,
// stay listed so they can be removed.
const otherRecipients = computed(() => form.userIds.filter((id) => !props.teamMembers.some((m) => m.user_id === id)));

function toggleUser(userId: number, checked: boolean) {
  form.userIds = checked ? [...form.userIds, userId] : form.userIds.filter(id => id !== userId);
}

// A saved webhook with no new URL is tested by id, so the stored URL is used.
function testPayload(): { id: number } | DestinationRequest {
  if (editing.value && form.type === "webhook" && !form.url.trim()) return { id: editing.value.id };
  return request();
}

async function sendTest() {
  testing.value = true;
  error.value = null;
  testResults.value = null;
  const result = await store.testDestination(testPayload());
  if (result.success && result.data) testResults.value = result.data.results;
  else error.value = result.error?.message || t("destinations.testFailed");
  testing.value = false;
}

async function save() {
  saving.value = true;
  error.value = null;
  const result = editing.value
    ? await store.updateDestination(editing.value.id, request())
    : await store.createDestination(request());
  if (result.success && result.data) emit("saved", result.data);
  else error.value = result.error?.message || t("destinations.saveFailed");
  saving.value = false;
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
        <Select v-model="form.type" :disabled="!!editing">
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
      <Input
        id="destination-url"
        v-model="form.url"
        type="url"
        :placeholder="editing?.secret_hint ? t('destinations.keepURL', { host: editing.secret_hint }) : 'https://api.example.com/hooks/...'"
        autocomplete="off"
      />
    </div>

    <template v-else>
      <div class="space-y-2">
        <Label>{{ t("destinations.recipients") }}</Label>
        <div class="max-h-40 space-y-2 overflow-y-auto rounded-md border p-2">
          <label v-for="member in teamMembers" :key="member.user_id" class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="form.userIds.includes(member.user_id)" @update:model-value="(value) => toggleUser(member.user_id, value === true)" />
            <span>{{ member.full_name || member.email }}</span>
          </label>
          <label v-for="id in otherRecipients" :key="`other-${id}`" class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="true" @update:model-value="(value) => toggleUser(id, value === true)" />
            <span class="text-muted-foreground">{{ t("destinations.unknownUser", { id }) }}</span>
          </label>
        </div>
      </div>
      <div class="space-y-2">
        <Label for="destination-addresses">{{ t("destinations.addresses") }}</Label>
        <Textarea id="destination-addresses" v-model="form.addresses" rows="2" placeholder="oncall@example.com" />
      </div>
    </template>

    <label v-if="editing" class="flex items-center gap-2 text-sm">
      <Switch v-model="form.isActive" />
      {{ t("ui.active") }}
    </label>

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
        {{ editing ? t("destinations.update") : t("destinations.save") }}
      </Button>
    </div>
  </form>
</template>
