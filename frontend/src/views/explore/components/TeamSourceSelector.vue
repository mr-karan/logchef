<script setup lang="ts">
import { computed } from 'vue'
import type { TeamWithMemberCount, UserTeamMembership } from '@/api/teams'
import type { Source } from '@/api/sources'
import { formatSourceName } from '@/utils/format'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type TeamOption = UserTeamMembership | TeamWithMemberCount

interface Props {
  currentTeamId?: number | null
  currentSourceId?: number | null
  availableTeams?: TeamOption[]
  availableSources?: Source[]
  variant?: 'default' | 'toolbar'
}

const props = withDefaults(defineProps<Props>(), {
  currentTeamId: null,
  currentSourceId: null,
  availableTeams: () => [],
  availableSources: () => [],
  variant: 'default',
})

const emit = defineEmits<{
  (e: 'update:team', teamId: number): void
  (e: 'update:source', sourceId: number): void
}>()

const selectedTeamName = computed(() => {
  return props.availableTeams.find(team => team.id === props.currentTeamId)?.name || 'Select team'
})

const selectedSourceName = computed(() => {
  if (!props.currentSourceId) return 'Select source'

  const source = props.availableSources.find(item => item.id === props.currentSourceId)
  return source ? formatSourceName(source) : 'Select source'
})

const isToolbarVariant = computed(() => props.variant === 'toolbar')

const containerClass = computed(() =>
  isToolbarVariant.value ? 'flex items-center gap-2' : 'flex items-center space-x-3'
)

const teamTriggerClass = computed(() =>
  isToolbarVariant.value
    ? 'h-7 text-sm border-0 bg-transparent hover:bg-muted/50 px-2 min-w-[100px] w-auto focus:ring-0 focus:ring-offset-0'
    : 'h-8 text-sm w-48'
)

const sourceTriggerClass = computed(() =>
  isToolbarVariant.value
    ? 'h-7 text-sm border-0 bg-transparent hover:bg-muted/50 px-2 min-w-[120px] w-auto focus:ring-0 focus:ring-offset-0'
    : 'h-8 text-sm w-64'
)

function handleTeamChange(teamIdValue: string) {
  const teamId = parseInt(teamIdValue, 10)
  if (Number.isNaN(teamId)) return
  emit('update:team', teamId)
}

function handleSourceChange(sourceIdValue: string) {
  const sourceId = parseInt(sourceIdValue, 10)
  if (Number.isNaN(sourceId)) return
  emit('update:source', sourceId)
}
</script>

<template>
  <div :class="containerClass">
    <Select
      :model-value="currentTeamId?.toString() ?? ''"
      :disabled="availableTeams.length === 0"
      @update:model-value="handleTeamChange"
    >
      <SelectTrigger :class="teamTriggerClass">
        <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Teams</SelectLabel>
          <SelectItem
            v-for="team in availableTeams"
            :key="team.id"
            :value="team.id.toString()"
          >
            {{ team.name }}
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>

    <span v-if="isToolbarVariant" class="text-muted-foreground/50">/</span>

    <Select
      :model-value="currentSourceId?.toString() ?? ''"
      :disabled="!currentTeamId || availableSources.length === 0"
      @update:model-value="handleSourceChange"
    >
      <SelectTrigger :class="sourceTriggerClass">
        <SelectValue placeholder="Select source">{{ selectedSourceName }}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Log Sources</SelectLabel>
          <SelectItem v-if="!currentTeamId" value="no-team" disabled>
            Select a team first
          </SelectItem>
          <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>
            No sources available
          </SelectItem>
          <SelectItem
            v-for="source in availableSources"
            :key="source.id"
            :value="source.id.toString()"
          >
            {{ formatSourceName(source) }}
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  </div>
</template>
