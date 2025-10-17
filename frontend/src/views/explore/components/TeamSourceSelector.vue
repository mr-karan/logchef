<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { formatSourceName } from '@/utils/format'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'
import { useSourcesStore } from '@/stores/sources'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const router = useRouter()
const contextStore = useContextStore()
const teamsStore = useTeamsStore()
const sourcesStore = useSourcesStore()

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

function updateQuery(partial: Record<string, string | undefined>) {
  router.replace({
    query: {
      ...router.currentRoute.value.query,
      ...partial,
    },
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
</script>

<template>
  <div class="flex items-center space-x-3">
    <Select
      :model-value="currentTeamId?.toString() ?? ''"
      @update:model-value="handleTeamChange"
      :disabled="availableTeams.length === 0"
    >
      <SelectTrigger class="h-8 text-sm w-48">
        <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Teams</SelectLabel>
          <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
            {{ team.name }}
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>

    <Select
      :model-value="currentSourceId?.toString() ?? ''"
      @update:model-value="handleSourceChange"
      :disabled="!currentTeamId || availableSources.length === 0"
    >
      <SelectTrigger class="h-8 text-sm w-64">
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
          <SelectItem v-for="source in availableSources" :key="source.id" :value="source.id.toString()">
            {{ formatSourceName(source) }}
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  </div>
</template>
