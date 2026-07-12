<script setup lang="ts">
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { X, Plus, User, Bell } from "lucide-vue-next";
import type { AcceptableValue } from "reka-ui";
import type { TeamMember } from "@/api/teams";
import type { AlertFormState } from "@/composables/useAlertForm";

defineProps<{
  form: AlertFormState;
  disabled: boolean;
  teamMembers: TeamMember[];
  onAddRecipient: (value: AcceptableValue) => void;
  onRemoveRecipient: (userId: number) => void;
  onAddWebhook: () => void;
  onRemoveWebhook: (url: string) => void;
  onAddLabel: () => void;
  onRemoveLabel: (id: number) => void;
  onAddAnnotation: () => void;
  onRemoveAnnotation: (id: number) => void;
}>();

const newWebhookUrl = defineModel<string>("newWebhookUrl", { required: true });
</script>

<template>
  <section class="space-y-6 border-t pt-4">
    <div>
      <h3 class="text-sm font-semibold flex items-center gap-2">
        <Bell class="h-4 w-4" />
        Notifications & Routing
      </h3>
      <p class="text-xs text-muted-foreground mt-1">Configure where alerts should be sent when triggered.</p>
    </div>

    <!-- Recipients -->
    <div class="space-y-3">
       <Label class="text-xs font-medium">Team Members <span class="font-normal text-muted-foreground ml-1">· Notify via email</span></Label>
       <div class="flex gap-2">
          <Select @update:model-value="onAddRecipient">
            <SelectTrigger class="w-full">
              <SelectValue placeholder="Select team member to notify..." />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="member in teamMembers" :key="member.user_id" :value="String(member.user_id)">
                <div class="flex items-center gap-2">
                  <User class="h-3 w-3" />
                  <span>{{ member.full_name || member.email }}</span>
                  <span class="text-xs text-muted-foreground ml-1">({{ member.role }})</span>
                </div>
              </SelectItem>
            </SelectContent>
          </Select>
       </div>

       <!-- Selected Recipients List -->
       <div v-if="form.recipient_user_ids.length > 0" class="flex flex-wrap gap-2">
          <Badge v-for="userId in form.recipient_user_ids" :key="userId" variant="secondary" class="flex items-center gap-1 font-normal">
            <User class="h-3 w-3 opacity-50" />
            <span>
              {{ teamMembers.find(m => m.user_id === userId)?.full_name || teamMembers.find(m => m.user_id === userId)?.email || `User ${userId}` }}
            </span>
            <button type="button" @click="onRemoveRecipient(userId)" class="ml-1 hover:text-destructive">
              <X class="h-3 w-3" />
            </button>
          </Badge>
       </div>
    </div>

    <!-- Webhooks -->
    <div class="space-y-3">
      <Label class="text-xs font-medium">Webhook URLs <span class="font-normal text-muted-foreground ml-1">· Send JSON payload</span></Label>
      <div class="flex gap-2">
        <Input v-model="newWebhookUrl" placeholder="https://api.example.com/hooks/..." @keydown.enter.prevent="onAddWebhook" />
        <Button type="button" variant="secondary" @click="onAddWebhook">
          <Plus class="h-4 w-4" />
        </Button>
      </div>

      <!-- Added Webhooks List -->
      <div v-if="form.webhook_urls.length > 0" class="space-y-2">
        <div v-for="url in form.webhook_urls" :key="url" class="flex items-center justify-between gap-2 border rounded-md px-3 py-2 text-sm">
          <span class="truncate font-mono text-xs">{{ url }}</span>
          <button type="button" @click="onRemoveWebhook(url)" class="text-muted-foreground hover:text-destructive">
            <X class="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>

    <!-- Metadata -->
    <div class="grid gap-4 md:grid-cols-2">
      <!-- Labels -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class="text-xs font-medium">Labels <span class="font-normal text-muted-foreground ml-1">· Grouping</span></Label>
          <Button type="button" variant="outline" size="sm" @click="onAddLabel" :disabled="disabled">
            + Add Label
          </Button>
        </div>
        <div class="space-y-2">
          <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
            <Input v-model="label.key" placeholder="Key" class="flex-1" :disabled="disabled" />
            <Input v-model="label.value" placeholder="Value" class="flex-1" :disabled="disabled" />
            <Button type="button" variant="ghost" size="icon" @click="onRemoveLabel(label.id)" :disabled="disabled">
              <X class="h-4 w-4" />
            </Button>
          </div>
          <p v-if="form.labels.length === 0" class="text-xs text-muted-foreground">No custom labels.</p>
        </div>
      </div>

      <!-- Annotations -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class="text-xs font-medium">Annotations <span class="font-normal text-muted-foreground ml-1">· Context</span></Label>
          <Button type="button" variant="outline" size="sm" @click="onAddAnnotation" :disabled="disabled">
            + Add Annotation
          </Button>
        </div>
        <div class="space-y-2">
          <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
            <Input v-model="annotation.key" placeholder="Key" class="flex-1" :disabled="disabled" />
            <Input v-model="annotation.value" placeholder="Value" class="flex-1" :disabled="disabled" />
            <Button type="button" variant="ghost" size="icon" @click="onRemoveAnnotation(annotation.id)" :disabled="disabled">
              <X class="h-4 w-4" />
            </Button>
          </div>
          <p v-if="form.annotations.length === 0" class="text-xs text-muted-foreground">No custom annotations.</p>
        </div>
      </div>
    </div>
  </section>
</template>
