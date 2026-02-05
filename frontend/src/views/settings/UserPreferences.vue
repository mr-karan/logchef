<script setup lang="ts">
import { computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { usePreferencesStore } from "@/stores/preferences";
import { useThemeStore, type ThemeMode } from "@/stores/theme";
import type { DisplayModePreference, TimezonePreference } from "@/api/preferences";

const preferencesStore = usePreferencesStore();
const themeStore = useThemeStore();
const { preferences } = storeToRefs(preferencesStore);

onMounted(() => {
  preferencesStore.loadPreferences();
});

const isSaving = computed(
  () =>
    preferencesStore.isLoadingOperation("updatePreferences") ||
    preferencesStore.isLoadingOperation("syncPreferences")
);

const themePreference = computed({
  get: () => themeStore.preference,
  set: (value: ThemeMode) => {
    themeStore.setTheme(value);
    preferencesStore.updatePreferences({ theme: value }, { syncTheme: false });
  },
});

const timezonePreference = computed({
  get: () => preferences.value.timezone,
  set: (value: TimezonePreference) => {
    preferencesStore.updatePreferences({ timezone: value });
  },
});

const displayModePreference = computed({
  get: () => preferences.value.display_mode,
  set: (value: DisplayModePreference) => {
    preferencesStore.updatePreferences({ display_mode: value });
  },
});

const fieldsPanelOpen = computed({
  get: () => preferences.value.fields_panel_open,
  set: (value: boolean) => {
    preferencesStore.updatePreferences({ fields_panel_open: value });
  },
});
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">Preferences</h1>
        <p class="text-muted-foreground mt-2">
          Tune the interface to match how you explore logs every day.
        </p>
      </div>
      <p class="text-xs text-muted-foreground">
        {{ isSaving ? "Saving changes…" : "Changes save automatically." }}
      </p>
    </div>

    <Card>
      <CardHeader>
        <CardTitle>Appearance</CardTitle>
        <CardDescription>Choose how LogChef looks across sessions.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-6">
        <div class="space-y-3">
          <Label class="text-sm font-medium">Theme</Label>
          <RadioGroup v-model="themePreference" class="grid gap-3 md:grid-cols-3">
            <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
              <RadioGroupItem value="light" class="mt-1" />
              <div>
                <p class="text-sm font-medium">Light</p>
                <p class="text-xs text-muted-foreground">Bright workspace with crisp contrast.</p>
              </div>
            </Label>
            <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
              <RadioGroupItem value="dark" class="mt-1" />
              <div>
                <p class="text-sm font-medium">Dark</p>
                <p class="text-xs text-muted-foreground">Reduce glare for long analysis sessions.</p>
              </div>
            </Label>
            <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
              <RadioGroupItem value="auto" class="mt-1" />
              <div>
                <p class="text-sm font-medium">System</p>
                <p class="text-xs text-muted-foreground">Match your operating system preference.</p>
              </div>
            </Label>
          </RadioGroup>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle>Log Explorer</CardTitle>
        <CardDescription>Set defaults for log viewing and navigation.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-6">
        <div class="grid gap-4 md:grid-cols-2">
          <div class="space-y-2">
            <Label for="timezone">Default Timezone</Label>
            <Select v-model="timezonePreference">
              <SelectTrigger id="timezone">
                <SelectValue placeholder="Select timezone" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">Local time</SelectItem>
                <SelectItem value="utc">UTC</SelectItem>
              </SelectContent>
            </Select>
            <p class="text-xs text-muted-foreground">Controls how timestamps are displayed.</p>
          </div>
        </div>

        <Separator />

        <div class="space-y-3">
          <Label class="text-sm font-medium">Default View Mode</Label>
          <RadioGroup v-model="displayModePreference" class="grid gap-3 md:grid-cols-2">
            <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
              <RadioGroupItem value="table" class="mt-1" />
              <div>
                <p class="text-sm font-medium">Table</p>
                <p class="text-xs text-muted-foreground">Columnar layout with full field visibility.</p>
              </div>
            </Label>
            <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
              <RadioGroupItem value="compact" class="mt-1" />
              <div>
                <p class="text-sm font-medium">Compact</p>
                <p class="text-xs text-muted-foreground">Dense, streaming-style logs for quick scans.</p>
              </div>
            </Label>
          </RadioGroup>
        </div>

        <Separator />

        <div class="flex items-center justify-between">
          <div class="space-y-0.5">
            <Label>Show Fields Panel by default</Label>
            <p class="text-sm text-muted-foreground">
              Keep the fields and filters panel open when exploring logs.
            </p>
          </div>
          <Switch v-model="fieldsPanelOpen" />
        </div>
      </CardContent>
    </Card>
  </div>
</template>
