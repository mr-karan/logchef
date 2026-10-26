<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed, ref, watch } from 'vue'
import { useContextStore } from '@/stores/context'
import type { ExploreLocation } from '@/lib/exploreLocation'
import { useExploreStore } from '@/stores/explore'
import { useExploreRuns } from '@/stores/exploreRuns'
import { useAskChefStore } from '@/stores/askChef'
import { useMetaStore } from '@/stores/meta'
import { usePreferencesStore } from '@/stores/preferences'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { DateTimePicker } from '@/components/date-time-picker'
import { ChevronDown, Link, Share2, Timer, Terminal } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { useLimitOptions } from '@/composables/useLimitOptions'
import { TOAST_DURATION } from '@/lib/constants'
import { copyText } from '../copyText'
import { generateCliCommand } from '@/utils/cliCommand'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getNativeQueryLanguageForSource, getSourceTypeLabel } from '@/lib/queryMetadata'
import TeamSourceSelector from './TeamSourceSelector.vue'
import type { Source } from '@/api/sources'
import type { TeamWithMemberCount, UserTeamMembership } from '@/api/teams'
import type { TimeWindow } from '@/utils/timeWindow'

const { t } = useI18n();

const { toast } = useToast()
const exploreStore = useExploreStore()
const runs = useExploreRuns()
const contextStore = useContextStore()
const askChef = useAskChefStore()
const metaStore = useMetaStore()
const preferencesStore = usePreferencesStore()

type TeamOption = UserTeamMembership | TeamWithMemberCount

interface Props {
  selection: Pick<ExploreLocation, 'teamId' | 'sourceId'>
  availableTeams: TeamOption[]
  availableSources: Source[]
  selectedSource: Source | null
}

const props = defineProps<Props>()

const emit = defineEmits<{
  (e: 'update:team', teamId: number): void
  (e: 'update:source', sourceId: number): void
  (e: 'update:window', window: TimeWindow): void
  (e: 'update:limit', limit: number): void
  (e: 'share'): void
}>()

const { limitOptions } = useLimitOptions()
const selectedSourceTypeLabel = computed(() =>
  props.selectedSource ? getSourceTypeLabel(props.selectedSource) : null
)

// Time range display
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null)

// Limit options
const currentLimit = computed(() => exploreStore.limit)
// The picker shows the editor's window, resolved when it changes.
const resolved = computed(() => exploreStore.resolveNow())

// ClickHouse native SQL owns its own time/LIMIT clauses. VictoriaLogs native mode does not.
const isNativeSqlMode = computed(() =>
  exploreStore.activeMode === 'native' && getNativeQueryLanguageForSource(props.selectedSource) === 'clickhouse-sql'
)

// Live tail pins the stream to "now"; time range and limit don't apply.
const isLive = computed(() => exploreStore.isLive)
const timeControlsDisabled = computed(() => isNativeSqlMode.value || isLive.value)
const timeControlsDisabledReason = computed(() =>
  isLive.value ? t('ui.pausedDuringLiveTail') : t('ui.timeRangeIsControlledInYourSQLQuery')
)
const limitControlsDisabledReason = computed(() =>
  isLive.value ? t('ui.pausedDuringLiveTail') : t('ui.limitIsControlledByLIMITClauseInYourSQLQuery')
)

const baseTimeoutOptions = [
  { label: '10s', value: 10 },
  { label: '30s', value: 30 },
  { label: '1m', value: 60 },
  { label: '2m', value: 120 },
  { label: '5m', value: 300 },
]
const maxQueryTimeoutSeconds = computed(() => metaStore.maxQueryTimeoutSeconds || 120)

function formatTimeoutLabel(seconds: number): string {
  return seconds % 60 === 0 ? `${seconds / 60}m` : `${seconds}s`
}

const timeoutOptions = computed(() => {
  const maxTimeout = Math.max(maxQueryTimeoutSeconds.value, 10)
  const options = baseTimeoutOptions.filter((option) => option.value <= maxTimeout)
  if (options.length === 0 || options[options.length - 1]?.value !== maxTimeout) {
    options.push({ label: formatTimeoutLabel(maxTimeout), value: maxTimeout })
  }
  return options
})

const selectedTimeout = computed(() => Math.min(exploreStore.queryTimeout || 30, maxQueryTimeoutSeconds.value))

watch(
  () => [exploreStore.queryTimeout, maxQueryTimeoutSeconds.value] as const,
  ([queryTimeout, maxTimeout]) => {
    const requestedTimeout = queryTimeout || 30
    if (requestedTimeout > maxTimeout) {
      exploreStore.setQueryTimeout(maxTimeout)
    }
  },
  { immediate: true }
)

function handleZonePreferenceChange(timezone: 'utc' | 'local') {
  void preferencesStore.updatePreferences({ timezone })
}

async function copyPageLink() {
  try {
    await copyText(window.location.href)
    toast({
      get title() { return t('ui.linkCopied'); },
      duration: TOAST_DURATION.SUCCESS
    })
  } catch {
    toast({
      get title() { return t('ui.copyFailed'); },
      get description() { return t('ui.failedToCopyURL'); },
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
  }
}

async function copyCliCommand() {
  const { teamId, sourceId } = contextStore
  if (!teamId || !sourceId) {
    toast({
      get title() { return t('ui.cannotCopyCLICommand'); },
      get description() { return t('ui.teamAndSourceMustBeSelected'); },
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
    return
  }

  const command = generateCliCommand({
    teamId,
    sourceId,
    language: exploreStore.activeMode === 'logchefql' ? 'logchefql' : getNativeQueryLanguageForSource(props.selectedSource),
    query:
      exploreStore.activeMode === 'logchefql'
        ? exploreStore.logchefqlCode
        : exploreStore.nativeQuery,
    window: exploreStore.window,
    resolved: exploreStore.resolveNow(),
    limit: exploreStore.limit,
    timeout: exploreStore.queryTimeout,
  })

  try {
    await copyText(command)
    toast({
      get title() { return t('ui.cLICommandCopied2'); },
      get description() { return t('ui.pasteInYourTerminalToRunTheSameQuery'); },
      duration: TOAST_DURATION.SUCCESS
    })
  } catch {
    toast({
      get title() { return t('ui.copyFailed'); },
      get description() { return t('ui.failedToCopyCLICommand'); },
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
  }
}

// Expose method to parent
defineExpose({
  openDatePicker: () => dateTimePickerRef.value?.openDatePicker(),
})
</script>

<template>
  <div class="flex items-center justify-between h-11 px-4 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
    <!-- Left: Team/Source + Time Range + Limit (all grouped together) -->
    <div class="flex items-center gap-3">
      <TeamSourceSelector
        variant="toolbar"
        :current-team-id="selection.teamId"
        :current-source-id="selection.sourceId"
        :available-teams="availableTeams"
        :available-sources="availableSources"
        @update:team="emit('update:team', $event)"
        @update:source="emit('update:source', $event)"
      />

      <Badge
        v-if="selectedSourceTypeLabel"
        variant="outline"
        class="h-5 rounded-md px-1.5 text-[10px] font-medium text-muted-foreground"
      >
        {{ selectedSourceTypeLabel }}
      </Badge>

      <!-- Divider -->
      <div class="h-5 w-px bg-border" />

      <!-- The time range is locked for ClickHouse native SQL mode and during live tail -->
      <DateTimePicker
        ref="dateTimePickerRef"
        :model-value="exploreStore.window"
        :resolved="resolved"
        :zone-preference="preferencesStore.preferences.timezone"
        :disabled-reason="timeControlsDisabled ? timeControlsDisabledReason : undefined"
        @update:model-value="emit('update:window', $event)"
        @update:zone-preference="handleZonePreferenceChange"
      />

      <!-- Limit Dropdown -->
      <!-- Disabled for ClickHouse native SQL mode and during live tail -->
      <TooltipProvider v-if="timeControlsDisabled">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" class="h-7 text-xs px-2 gap-1 opacity-50 cursor-not-allowed" disabled>
              <span class='text-muted-foreground'>{{ t('ui.limit') }}</span>
              <span class='font-medium'>{{ isLive ? t('ui.live') : 'SQL' }}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">{{ limitControlsDisabledReason }}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <DropdownMenu v-else>
        <DropdownMenuTrigger asChild>
          <Button variant='ghost' size="sm" class='h-7 text-xs px-2 bg-muted/60 hover:bg-muted text-muted-foreground hover:text-foreground font-normal' :title="t('ui.resultLimit')">
            {{ currentLimit.toLocaleString() }} {{ t('ui.rows') }}
            <ChevronDown class="h-3 w-3 ml-0.5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" class="w-36">
          <DropdownMenuLabel class='text-xs'>{{ t('ui.resultsLimit') }}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuRadioGroup :model-value="currentLimit">
            <DropdownMenuRadioItem
              v-for="limit in limitOptions"
              :key="limit"
              :value="limit"
              @select="emit('update:limit', limit)"
            >
              {{ limit.toLocaleString() }} {{ t('ui.rows') }}
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      <!-- Live tail streams without a query timeout -->
      <DropdownMenu v-if="!isLive">
        <DropdownMenuTrigger asChild>
          <Button variant='ghost' size="sm" class='h-7 gap-1 text-xs px-2 bg-muted/60 hover:bg-muted text-muted-foreground hover:text-foreground font-normal' :title="t('ui.queryTimeout')" :disabled="runs.isRunning">
            <Timer class="h-3.5 w-3.5" />
            {{ t('explore.timeoutChip', { value: formatTimeoutLabel(selectedTimeout) }) }}
            <ChevronDown class="h-3 w-3 ml-0.5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" class="w-32">
          <DropdownMenuLabel class='text-xs'>{{ t('ui.queryTimeout') }}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuRadioGroup :model-value="selectedTimeout">
            <DropdownMenuRadioItem
              v-for="option in timeoutOptions"
              :key="option.value"
              :value="option.value"
              @select="exploreStore.setQueryTimeout(option.value)"
            >
              {{ option.label }}
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <!-- Right: Actions -->
    <div class="flex items-center gap-1">
      <Button
        v-if="metaStore.askChefEnabled"
        variant="ghost"
        size="sm"
        class="h-7 gap-1.5 px-2"
        :class="{ 'bg-accent': askChef.open }"
        :title="t('askChef.toggle')"
        :aria-pressed="askChef.open"
        @click="askChef.open ? askChef.close() : askChef.openForExplorer()"
      >
        <span class="text-violet-600" aria-hidden="true">✦</span>
        <span class="text-xs font-medium hidden sm:inline">{{ t('askChef.title') }}</span>
      </Button>

      <!-- Copy CLI Command Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" class="h-7 w-7 p-0" :aria-label="t('ui.copyCLICommand')" @click="copyCliCommand">
              <Terminal class="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class='text-xs'>{{ t('ui.copyCLICommand') }}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" class="h-7 w-7 p-0" :title="t('explore.share')" :aria-label="t('explore.share')">
            <Share2 class="h-3.5 w-3.5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-52">
          <DropdownMenuItem class="text-xs" @click="copyPageLink">
            <Link class="h-3.5 w-3.5" />
            {{ t('ui.copyShareableLink') }}
          </DropdownMenuItem>
          <DropdownMenuItem class="text-xs" :disabled="runs.isRunning" @click="emit('share')">
            <Share2 class="h-3.5 w-3.5" />
            {{ t('ui.shareQuery') }}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

    </div>

  </div>
</template>
