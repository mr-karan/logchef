<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed, onMounted, onScopeDispose, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { PageHeader, PageSection } from "@/components/layout";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { usePreferencesStore } from "@/stores/preferences";
import { useThemeStore, type ThemeMode } from "@/stores/theme";
import { useMetaStore } from "@/stores/meta";
import type { DisplayModePreference, TimezonePreference } from "@/api/preferences";
import { locales, isLocalePreference } from "@/i18n/locales";
import { setLocale } from "@/i18n";
import { useAuthStore } from "@/stores/auth";

const { t } = useI18n();

const preferencesStore = usePreferencesStore();
const themeStore = useThemeStore();
const metaStore = useMetaStore();
const { preferences } = storeToRefs(preferencesStore);
const languageError = ref("");
const changingLanguage = ref(false);
const authStore = useAuthStore();
let languageRequest: AbortController | undefined;
watch(() => authStore.user?.id, () => languageRequest?.abort());
onScopeDispose(() => languageRequest?.abort());

async function changeLanguage(value: unknown) {
  if (!isLocalePreference(value) || changingLanguage.value) return;
  const userId = authStore.user?.id;
  const controller = new AbortController();
  languageRequest = controller;
  languageError.value = "";
  changingLanguage.value = true;
  try {
    await setLocale(value, controller.signal);
    if (controller.signal.aborted || authStore.user?.id !== userId) return;
    const result = await preferencesStore.updatePreferences({ locale: value });
    if (!controller.signal.aborted && !result.success) languageError.value = t("language.saveFailed");
  } catch {
    if (!controller.signal.aborted) languageError.value = t("language.loadFailed");
  } finally {
    changingLanguage.value = false;
  }
}

onMounted(() => {
  preferencesStore.loadPreferences();
});

const isSaving = computed(
  () =>
    preferencesStore.isLoadingOperation("updatePreferences") ||
    preferencesStore.isLoadingOperation("syncPreferences")
);

const saveStatus = computed(() => {
  if (metaStore.demoReadOnly) return t('ui.savedInThisBrowser');
  return isSaving.value ? t('ui.savingChanges') : t('ui.changesSaveAutomatically');
});

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
    <PageHeader
      :title="t('ui.preferences')"
      :description="t('ui.tuneTheInterfaceToMatchHowYouExploreLogsEveryDay')"
    >
      <template #actions>
        <p class="text-xs text-muted-foreground">
          {{ saveStatus }}
        </p>
      </template>
    </PageHeader>

    <PageSection :title="t('language.title')" :description="t('language.description')">
      <Label for="interface-language">{{ t('language.title') }}</Label>
      <Select :model-value="preferences.locale ?? 'auto'" :disabled="changingLanguage" @update:model-value="changeLanguage">
        <SelectTrigger id="interface-language" class="mt-2 max-w-sm" :aria-invalid="Boolean(languageError)" aria-describedby="language-error">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="auto">{{ t('language.auto') }}</SelectItem>
            <SelectItem v-for="language in locales" :key="language.code" :value="language.code">
              <span :lang="language.code">{{ language.name }}</span>
            </SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      <p v-if="languageError" id="language-error" role="alert" class="mt-2 text-sm text-destructive">{{ languageError }}</p>
    </PageSection>

    <PageSection :title="t('ui.appearance')" :description="t('ui.chooseHowLogChefLooksAcrossSessions')">
      <div class="space-y-3">
        <Label class='text-sm font-medium'>{{ t('ui.theme') }}</Label>
        <RadioGroup v-model="themePreference" class="grid gap-3 md:grid-cols-3">
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="light" class="mt-1" />
            <div>
              <p class='text-sm font-medium'>{{ t('ui.light') }}</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.brightWorkspaceWithCrispContrast') }}</p>
            </div>
          </Label>
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="dark" class="mt-1" />
            <div>
              <p class='text-sm font-medium'>{{ t('ui.dark') }}</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.reduceGlareForLongAnalysisSessions') }}</p>
            </div>
          </Label>
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="auto" class="mt-1" />
            <div>
              <p class='text-sm font-medium'>{{ t('ui.system') }}</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.matchYourOperatingSystemPreference') }}</p>
            </div>
          </Label>
        </RadioGroup>
      </div>
    </PageSection>

    <PageSection :title="t('ui.logExplorer')" :description="t('ui.setDefaultsForLogViewingAndNavigation')" content-class="space-y-6">
      <div class="grid gap-4 md:grid-cols-2">
        <div class="space-y-2">
          <Label for="timezone">{{ t('ui.defaultTimezone') }}</Label>
          <Select v-model="timezonePreference">
            <SelectTrigger id="timezone">
              <SelectValue :placeholder="t('ui.selectTimezone')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="local">{{ t('ui.localTime2') }}</SelectItem>
              <SelectItem value="utc">UTC</SelectItem>
            </SelectContent>
          </Select>
          <p class='text-xs text-muted-foreground'>{{ t('ui.controlsHowTimestampsAreDisplayed') }}</p>
        </div>
      </div>

      <Separator />

      <div class="space-y-3">
        <Label class='text-sm font-medium'>{{ t('ui.defaultViewMode') }}</Label>
        <RadioGroup v-model="displayModePreference" class="grid gap-3 md:grid-cols-3">
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="table" class="mt-1" />
            <div>
              <p class='text-sm font-medium'>{{ t('ui.table') }}</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.columnarLayoutWithFullFieldVisibility') }}</p>
            </div>
          </Label>
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="compact" class="mt-1" />
            <div>
              <p class='text-sm font-medium'>{{ t('ui.compact') }}</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.denseStreamingStyleLogsForQuickScans') }}</p>
            </div>
          </Label>
          <Label class="flex items-start gap-3 rounded-md border p-3 hover:bg-muted/40">
            <RadioGroupItem value="json" class="mt-1" />
            <div>
              <p class="text-sm font-medium">JSON</p>
              <p class='text-xs text-muted-foreground'>{{ t('ui.inspectRawStructuredEventsWithoutTableFormatting') }}</p>
            </div>
          </Label>
        </RadioGroup>
      </div>

      <Separator />

      <div class="flex items-center justify-between">
        <div class="space-y-0.5">
          <Label>{{ t('ui.showFieldsPanelByDefault') }}</Label>
          <p class="text-sm text-muted-foreground">
            {{ t('ui.keepTheFieldsAndFiltersPanelOpenWhenExploringLogs') }}
          </p>
        </div>
        <Switch v-model="fieldsPanelOpen" />
      </div>
    </PageSection>
  </div>
</template>
