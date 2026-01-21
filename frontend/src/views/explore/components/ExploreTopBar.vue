<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { formatSourceName } from '@/utils/format'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'
import { useSourcesStore } from '@/stores/sources'
import { useExploreStore } from '@/stores/explore'
import { useTimeRange } from '@/composables/useTimeRange'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/date-time-picker'
import { ChevronRight, Share2, Settings, Clock, Terminal } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { generateCliCommand } from '@/utils/cliCommand'
import { ref } from 'vue'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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

const router = useRouter()
const { toast } = useToast()
const contextStore = useContextStore()
const teamsStore = useTeamsStore()
const sourcesStore = useSourcesStore()
const exploreStore = useExploreStore()

const { timeRange, quickRangeLabelToRelativeTime, getHumanReadableTimeRange: _getHumanReadableTimeRange } = useTimeRange()

// Team/Source state
const currentTeamId = computed(() => contextStore.teamId)
const currentSourceId = computed(() => contextStore.sourceId)
const availableTeams = computed(() => teamsStore.teams || [])
const availableSources = computed(() => sourcesStore.teamSources || [])

const selectedTeamName = computed(() => teamsStore.currentTeam?.name || 'Select team')
const selectedSourceName = computed(() => {
  if (!currentSourceId.value) return 'Select source'
  const source = availableSources.value.find((s) => s.id === currentSourceId.value)
  return source ? formatSourceName(source) : 'Select source'
})

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

// Mode-specific behavior
// In SQL mode, time/limit controls are disabled - user controls these in their query
const isSqlMode = computed(() => exploreStore.activeMode === 'sql')

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

// Handlers
function updateQuery(partial: Record<string, string | undefined>) {
  router.replace({
    query: { ...router.currentRoute.value.query, ...partial },
  })
}

function handleTeamChange(teamIdStr: string) {
  const teamId = parseInt(teamIdStr, 10)
  if (Number.isNaN(teamId)) return
  updateQuery({ team: String(teamId), source: undefined })
}

function handleSourceChange(sourceIdStr: string) {
  const sourceId = parseInt(sourceIdStr, 10)
  if (Number.isNaN(sourceId)) return
  updateQuery({ source: String(sourceId) })
}

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
  if (!contextStore.teamId || !contextStore.sourceId) {
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
    teamId: contextStore.teamId,
    sourceId: contextStore.sourceId,
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
      <!-- Team Selector -->
      <Select
        :model-value="currentTeamId?.toString() ?? ''"
        @update:model-value="handleTeamChange"
        :disabled="availableTeams.length === 0"
      >
        <SelectTrigger class="h-7 text-sm border-0 bg-transparent hover:bg-muted/50 px-2 min-w-[100px] w-auto focus:ring-0 focus:ring-offset-0">
          <SelectValue placeholder="Team">
            <span class="font-medium">{{ selectedTeamName }}</span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectLabel class="text-xs">Teams</SelectLabel>
            <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
              {{ team.name }}
            </SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>

      <ChevronRight class="h-3.5 w-3.5 text-muted-foreground/50" />

      <!-- Source Selector -->
      <Select
        :model-value="currentSourceId?.toString() ?? ''"
        @update:model-value="handleSourceChange"
        :disabled="!currentTeamId || availableSources.length === 0"
      >
        <SelectTrigger class="h-7 text-sm border-0 bg-transparent hover:bg-muted/50 px-2 min-w-[120px] w-auto focus:ring-0 focus:ring-offset-0">
          <SelectValue placeholder="Source">
            <span class="font-medium">{{ selectedSourceName }}</span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectLabel class="text-xs">Log Sources</SelectLabel>
            <SelectItem v-if="!currentTeamId" value="no-team" disabled>
              Select a team first
            </SelectItem>
            <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>
              No sources available
            </SelectItem>
            <SelectItem v-for="source in availableSources" :key="source.id" :value="source.id.toString()">
              {{ formatSourceName(source) }}
            </SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>

      <!-- Divider -->
      <div class="h-5 w-px bg-border" />

      <!-- Date/Time Picker -->
      <!-- Disabled in SQL mode - users control time range in their query -->
      <TooltipProvider v-if="isSqlMode">
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
      <!-- Disabled in SQL mode - users control LIMIT in their query -->
      <TooltipProvider v-if="isSqlMode">
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
            v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" 
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

