<script setup lang="ts">
import { computed, ref } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useTimeRange } from '@/composables/useTimeRange'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { DateTimePicker } from '@/components/date-time-picker'
import { Share2, Settings, Clock, Terminal } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { useLimitOptions } from '@/composables/useLimitOptions'
import { TOAST_DURATION } from '@/lib/constants'
import { generateCliCommand } from '@/utils/cliCommand'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
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

const { toast } = useToast()
const exploreStore = useExploreStore()

type TeamOption = UserTeamMembership | TeamWithMemberCount

interface Props {
  currentTeamId: number | null
  currentSourceId: number | null
  availableTeams: TeamOption[]
  availableSources: Source[]
  selectedSource: Source | null
}

const props = defineProps<Props>()

const emit = defineEmits<{
  (e: 'update:team', teamId: number): void
  (e: 'update:source', sourceId: number): void
}>()

const { timeRange, quickRangeLabelToRelativeTime } = useTimeRange()
const { limitOptions } = useLimitOptions()
const selectedSourceTypeLabel = computed(() =>
  props.selectedSource ? getSourceTypeLabel(props.selectedSource) : null
)

// Time range display
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null)

const quickRangeLabelFromRelativeTime = (relativeTime: string): string | null => {
  const map: Record<string, string> = {
    '5m': 'Last 5m', '15m': 'Last 15m', '30m': 'Last 30m',
    '1h': 'Last 1h', '3h': 'Last 3h', '6h': 'Last 6h',
    '12h': 'Last 12h', '24h': 'Last 24h', '2d': 'Last 2d',
    '7d': 'Last 7d', '30d': 'Last 30d', '90d': 'Last 90d',
  }
  return map[relativeTime] || null
}

// Limit options
const currentLimit = computed(() => exploreStore.limit)

// ClickHouse native SQL owns its own time/LIMIT clauses. VictoriaLogs native mode does not.
const isNativeSqlMode = computed(() =>
  exploreStore.activeMode === 'sql' && getNativeQueryLanguageForSource(props.selectedSource) === 'clickhouse-sql'
)

// Query timeout
const timeoutOptions = [
  { label: '10s', value: 10 },
  { label: '30s', value: 30 },
  { label: '1m', value: 60 },
  { label: '2m', value: 120 },
  { label: '5m', value: 300 },
]

const selectedTimeout = computed({
  get: () => (exploreStore.queryTimeout || 30).toString(),
  set: (value: string) => exploreStore.setQueryTimeout(parseInt(value, 10))
})

function handleDateRangeChange(value: any) {
  if (dateTimePickerRef.value?.selectedQuickRange) {
    const relativeTime = quickRangeLabelToRelativeTime(dateTimePickerRef.value.selectedQuickRange)
    if (relativeTime) {
      exploreStore.setRelativeTimeRange(relativeTime)
      return
    }
  }
  timeRange.value = value
}

function handleTimezoneChange(timezoneId: string) {
  exploreStore.setTimezoneIdentifier(timezoneId)
}

function handleLimitChange(limit: number) {
  exploreStore.setLimit(limit)
}

function copyUrlToClipboard() {
  try {
    navigator.clipboard.writeText(window.location.href)
  } catch (error) {
    toast({
      title: "Copy Failed",
      description: "Failed to copy URL.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
  }
}

function copyCliCommand() {
  if (!props.currentTeamId || !props.currentSourceId) {
    toast({
      title: "Cannot copy CLI command",
      description: "Team and source must be selected.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
    return
  }

  const tr = exploreStore.timeRange
  const command = generateCliCommand({
    teamId: props.currentTeamId,
    sourceId: props.currentSourceId,
    mode: exploreStore.activeMode,
    query:
      exploreStore.activeMode === 'logchefql'
        ? exploreStore.logchefqlCode
        : exploreStore.rawSql,
    relativeTime: exploreStore.selectedRelativeTime || undefined,
    absoluteStart: tr?.start
      ? new Date(
          tr.start.year,
          tr.start.month - 1,
          tr.start.day,
          'hour' in tr.start ? tr.start.hour : 0,
          'minute' in tr.start ? tr.start.minute : 0,
          'second' in tr.start ? tr.start.second : 0
        )
      : undefined,
    absoluteEnd: tr?.end
      ? new Date(
          tr.end.year,
          tr.end.month - 1,
          tr.end.day,
          'hour' in tr.end ? tr.end.hour : 0,
          'minute' in tr.end ? tr.end.minute : 0,
          'second' in tr.end ? tr.end.second : 0
        )
      : undefined,
    limit: exploreStore.limit,
    timeout: exploreStore.queryTimeout,
  })

  try {
    navigator.clipboard.writeText(command)
    toast({
      title: "CLI command copied",
      description: "Paste in your terminal to run the same query.",
      duration: TOAST_DURATION.SUCCESS
    })
  } catch {
    toast({
      title: "Copy Failed",
      description: "Failed to copy CLI command.",
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
        :current-team-id="currentTeamId"
        :current-source-id="currentSourceId"
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

      <!-- Date/Time Picker -->
      <!-- Disabled only for ClickHouse native SQL mode -->
      <TooltipProvider v-if="isNativeSqlMode">
        <Tooltip>
          <TooltipTrigger asChild>
            <div class="cursor-not-allowed">
              <DateTimePicker 
                ref="dateTimePickerRef" 
                :model-value="timeRange" 
                :selectedQuickRange="exploreStore.selectedRelativeTime ? quickRangeLabelFromRelativeTime(exploreStore.selectedRelativeTime) : null"
                :disabled="true"
                class="opacity-50 pointer-events-none"
              />
            </div>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">Time range is controlled in your SQL query</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <DateTimePicker 
        v-else
        ref="dateTimePickerRef" 
        :model-value="timeRange" 
        :selectedQuickRange="exploreStore.selectedRelativeTime ? quickRangeLabelFromRelativeTime(exploreStore.selectedRelativeTime) : null"
        @update:model-value="handleDateRangeChange" 
        @update:timezone="handleTimezoneChange"
      />

      <!-- Limit Dropdown -->
      <!-- Disabled only for ClickHouse native SQL mode -->
      <TooltipProvider v-if="isNativeSqlMode">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" class="h-7 text-xs px-2 gap-1 opacity-50 cursor-not-allowed" disabled>
              <span class="text-muted-foreground">Limit:</span>
              <span class="font-medium">SQL</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">Limit is controlled by LIMIT clause in your SQL query</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <DropdownMenu v-else>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" class="h-7 text-xs px-2 gap-1">
            <span class="text-muted-foreground">Limit:</span>
            <span class="font-medium">{{ currentLimit.toLocaleString() }}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" class="w-32">
          <DropdownMenuLabel class="text-xs">Results Limit</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem 
            v-for="limit in limitOptions" 
            :key="limit"
            @click="handleLimitChange(limit)" 
            :class="{ 'bg-muted': currentLimit === limit }"
          >
            {{ limit.toLocaleString() }} rows
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <!-- Right: Actions -->
    <div class="flex items-center gap-1">
      <!-- Last run indicator -->
      <div v-if="exploreStore.lastExecutionTimestamp" class="text-xs text-muted-foreground mr-2 hidden sm:block">
        {{ new Date(exploreStore.lastExecutionTimestamp).toLocaleTimeString() }}
      </div>

      <!-- Copy CLI Command Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" class="h-7 w-7 p-0" @click="copyCliCommand">
              <Terminal class="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">Copy CLI command</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <!-- Share Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" class="h-7 w-7 p-0" @click="copyUrlToClipboard">
              <Share2 class="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">Copy shareable link</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <!-- Settings Dropdown -->
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="sm" class="h-7 w-7 p-0">
            <Settings class="h-3.5 w-3.5" />
          </Button>
        </PopoverTrigger>
        <PopoverContent class="w-48 p-2" align="end">
          <div class="space-y-3">
            <div class="space-y-1.5">
              <label class="text-xs font-medium text-muted-foreground">Query Timeout</label>
              <Select v-model="selectedTimeout">
                <SelectTrigger class="h-8 text-xs">
                  <div class="flex items-center gap-1.5">
                    <Clock class="h-3 w-3" />
                    <SelectValue />
                  </div>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="option in timeoutOptions" :key="option.value" :value="option.value.toString()">
                    {{ option.label }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  </div>
</template>
