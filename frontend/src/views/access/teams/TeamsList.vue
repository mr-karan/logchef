<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { onMounted, ref, computed } from 'vue'
import { Button } from '@/components/ui/button'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { PageHeader, EmptyState, LoadingState, SearchInput, SortHeader, RowActionsMenu } from '@/components/layout'
import { Plus, Settings, Search, Users } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { type Team } from '@/api/teams'
import AddTeam from './AddTeam.vue'
import { useTeamsStore } from '@/stores/teams'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { useTableSearchSort } from '@/composables/useTableSearchSort'
import { formatDate } from '@/utils/format'

const router = useRouter()
const { toast } = useToast()
const { t } = useI18n()
const teamsStore = useTeamsStore()
const authStore = useAuthStore()

const isGlobalAdmin = computed(() => authStore.user?.role === 'admin')

const isLoading = ref(true)
const showDeleteDialog = ref(false)
const teamToDelete = ref<Team | null>(null)
// managedTeams returns adminTeams for global admins, or userTeams filtered
// to admin role for team admins.
const teamsWithDefaults = computed(() => {
    return teamsStore.managedTeams.map(team => ({
        ...team,
        name: team.name || t('access.teamNumber', { id: team.id }),
        description: team.description || '',
        memberCount: team.member_count || 0
    }));
});

const {
    search: searchQuery,
    rows: filteredTeams,
    sortKey,
    sortDir,
    toggleSort,
} = useTableSearchSort(teamsWithDefaults, {
    searchKeys: ['name', 'description'],
    sortAccessors: {
        name: (team) => team.name.toLowerCase(),
        members: (team) => team.memberCount,
        created: (team) => new Date(team.created_at),
    },
    initialSort: { key: 'name', dir: 'asc' },
    persistKey: 'teams',
});

const handleDelete = (team: typeof teamsWithDefaults.value[number]) => {
    teamToDelete.value = team as Team
    showDeleteDialog.value = true
}

const confirmDelete = async () => {
    if (!teamToDelete.value) return

    try {
        await teamsStore.deleteTeam(teamToDelete.value.id)

        showDeleteDialog.value = false
        teamToDelete.value = null
    } catch (error) {
        console.error('Error deleting team:', error)
        toast({
            title: t('ui.error'),
            description: t('access.deleteTeamFailed'),
            variant: 'destructive'
        })
    }
}

const handleTeamCreated = (_teamId?: number) => {
    // Reset search to ensure the new team is visible
    searchQuery.value = ''
}

const loadTeams = async () => {
    try {
        isLoading.value = true
        // Global admins load all teams, team admins load their user teams
        if (isGlobalAdmin.value) {
            await teamsStore.loadAdminTeams(true)
        } else {
            await teamsStore.loadUserTeams()
        }
    } catch (error) {
        console.error('Error loading teams:', error)
        toast({
            title: t('ui.error'),
            description: t('access.loadTeamsFailed'),
            variant: 'destructive'
        })
    } finally {
        isLoading.value = false
    }
}

onMounted(() => {
    loadTeams()
})
</script>

<template>
    <div class="space-y-6">
        <PageHeader
            :title="t('routes.teams')"
            :description="isGlobalAdmin ? t('access.teamsDescription') : t('access.teamsYouAdminister')"
        >
            <template v-if="isGlobalAdmin" #actions>
                <AddTeam @team-created="handleTeamCreated" />
            </template>
        </PageHeader>

        <LoadingState v-if="isLoading" :label="t('access.loadingTeams')" />

        <EmptyState
            v-else-if="teamsWithDefaults.length === 0"
            :icon="Users"
            :title="t('access.noTeamsYet')"
            :description="isGlobalAdmin ? t('access.createFirstTeam') : t('access.noAdminTeams')"
        >
            <template v-if="isGlobalAdmin" #action>
                <AddTeam @team-created="handleTeamCreated">
                    <Button size="sm">
                        <Plus class="mr-2 h-4 w-4" />
                        {{ t('access.createTeam') }}
                    </Button>
                </AddTeam>
            </template>
        </EmptyState>

        <div v-else class="space-y-4">
            <SearchInput v-model="searchQuery" :placeholder="t('access.searchTeams')" />

            <EmptyState
                v-if="filteredTeams.length === 0"
                :icon="Search"
                :title="t('access.noResults')"
                :description="t('access.noTeamsMatch')"
                class="rounded-md border"
            />

            <div v-else class="rounded-md border">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead class="w-[200px]">
                                <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                                    {{ t('ui.name') }}
                                </SortHeader>
                            </TableHead>
                            <TableHead class="w-[300px]">{{ t('ui.description') }}</TableHead>
                            <TableHead class="w-[100px]">
                                <SortHeader column="members" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                                    {{ t('access.members') }}
                                </SortHeader>
                            </TableHead>
                            <TableHead class="w-[150px]">
                                <SortHeader column="created" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">
                                    {{ t('access.createdAt') }}
                                </SortHeader>
                            </TableHead>
                            <TableHead class="w-[100px] text-right">{{ t('ui.actions') }}</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        <TableRow v-for="team in filteredTeams" :key="team.id">
                            <TableCell>
                                <router-link :to="{ name: 'TeamSettings', params: { id: String(team.id) } }"
                                    class="font-medium hover:underline flex items-center gap-2">
                                    {{ team.name }}
                                </router-link>
                            </TableCell>
                            <TableCell>
                                <span class="line-clamp-1">{{ team.description || t('access.noDescription') }}</span>
                            </TableCell>
                            <TableCell>{{ team.memberCount }}</TableCell>
                            <TableCell>{{ formatDate(team.created_at) }}</TableCell>
                            <TableCell class="text-right">
                                <div class="flex items-center justify-end gap-1">
                                    <Button variant="ghost" size="icon" class="h-8 w-8" :title="t('access.editTeamSettings')"
                                        @click="router.push({ name: 'TeamSettings', params: { id: String(team.id) } })">
                                        <Settings class="h-4 w-4" />
                                    </Button>
                                    <RowActionsMenu v-if="isGlobalAdmin" :delete-label="t('access.deleteTeam')"
                                        @delete="handleDelete(team)" />
                                </div>
                            </TableCell>
                        </TableRow>
                    </TableBody>
                </Table>
            </div>
        </div>

        <ConfirmDialog
            :open="showDeleteDialog"
            :title="t('access.deleteTeamTitle')"
            :description="teamToDelete ? t('access.deleteTeamConfirmation', { name: teamToDelete.name }) : undefined"
            :confirm-text="t('ui.delete')"
            destructive
            @update:open="(v) => { if (!v) { showDeleteDialog = false; teamToDelete = null } }"
            @confirm="confirmDelete"
        />
    </div>
</template>
