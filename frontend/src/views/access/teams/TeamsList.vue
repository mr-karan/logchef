<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { Button } from '@/components/ui/button'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { PageHeader, EmptyState, LoadingState } from '@/components/layout'
import { Input } from '@/components/ui/input'
import { Plus, Trash2, Settings, Search, Users } from 'lucide-vue-next'
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
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import AddTeam from './AddTeam.vue'
import { useTeamsStore } from '@/stores/teams'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { formatDate } from '@/utils/format'

const router = useRouter()
const { toast } = useToast()
const teamsStore = useTeamsStore()
const authStore = useAuthStore()

const isGlobalAdmin = computed(() => authStore.user?.role === 'admin')

const isLoading = ref(true)
const showDeleteDialog = ref(false)
const teamToDelete = ref<Team | null>(null)
const searchQuery = ref('')

const filteredTeams = computed(() => {
    if (!searchQuery.value.trim()) {
        return teamsWithDefaults.value;
    }

    const query = searchQuery.value.toLowerCase();
    return teamsWithDefaults.value.filter(team =>
        team.name.toLowerCase().includes(query) ||
        (team.description && team.description.toLowerCase().includes(query))
    );
});

// managedTeams returns adminTeams for global admins, or userTeams filtered
// to admin role for team admins.
const teamsWithDefaults = computed(() => {
    return teamsStore.managedTeams.map(team => ({
        ...team,
        name: team.name || `Team ${team.id}`,
        description: team.description || '',
        memberCount: team.member_count || 0
    }));
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
            title: 'Error',
            description: 'Failed to delete team. Please try again.',
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
            title: 'Error',
            description: 'Failed to load teams. Please try refreshing the page.',
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
            title="Teams"
            :description="isGlobalAdmin ? 'Groups of users that have common dashboard and permission needs.' : 'Teams you administer.'"
        >
            <template v-if="isGlobalAdmin" #actions>
                <AddTeam @team-created="handleTeamCreated" />
            </template>
        </PageHeader>

        <LoadingState v-if="isLoading" label="Loading teams…" />

        <EmptyState
            v-else-if="filteredTeams.length === 0 && !searchQuery"
            :icon="Users"
            title="No teams yet"
            :description="isGlobalAdmin ? 'Create your first team to get started.' : 'You are not an admin of any teams.'"
        >
            <template v-if="isGlobalAdmin" #action>
                <AddTeam @team-created="handleTeamCreated">
                    <Button size="sm">
                        <Plus class="mr-2 h-4 w-4" />
                        Create team
                    </Button>
                </AddTeam>
            </template>
        </EmptyState>

        <div v-else class="space-y-4">
            <div class="relative w-full max-w-sm">
                <Search class="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input v-model="searchQuery" type="text" placeholder="Search teams by name or description…"
                    class="pl-8 h-9" />
            </div>

            <EmptyState
                v-if="filteredTeams.length === 0 && searchQuery.trim()"
                :icon="Search"
                title="No results"
                description="No teams match your search."
                class="rounded-md border"
            />

            <div v-else class="rounded-md border">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead class="w-[200px]">Name</TableHead>
                            <TableHead class="w-[300px]">Description</TableHead>
                            <TableHead class="w-[100px]">Members</TableHead>
                            <TableHead class="w-[150px]">Created At</TableHead>
                            <TableHead class="w-[100px] text-right">Actions</TableHead>
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
                                <span class="line-clamp-1">{{ team.description || 'No description' }}</span>
                            </TableCell>
                            <TableCell>{{ team.memberCount }}</TableCell>
                            <TableCell>{{ formatDate(team.created_at) }}</TableCell>
                            <TableCell class="text-right">
                                <div class="flex justify-end gap-2">
                                    <Tooltip>
                                        <TooltipTrigger asChild>
                                            <Button variant="ghost" size="icon"
                                                @click="router.push({ name: 'TeamSettings', params: { id: String(team.id) } })">
                                                <Settings class="h-4 w-4" />
                                            </Button>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                            <p>Edit team settings</p>
                                        </TooltipContent>
                                    </Tooltip>
                                    <Tooltip v-if="isGlobalAdmin">
                                        <TooltipTrigger asChild>
                                            <Button variant="destructive" size="icon"
                                                @click="handleDelete(team)">
                                                <Trash2 class="h-4 w-4" />
                                            </Button>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                            <p>Delete team</p>
                                        </TooltipContent>
                                    </Tooltip>
                                </div>
                            </TableCell>
                        </TableRow>
                    </TableBody>
                </Table>
            </div>
        </div>

        <ConfirmDialog
            :open="showDeleteDialog"
            title="Delete team?"
            :description="teamToDelete ? `Delete team &quot;${teamToDelete.name}&quot;? This action cannot be undone.` : undefined"
            confirm-text="Delete"
            destructive
            @update:open="(v) => { if (!v) { showDeleteDialog = false; teamToDelete = null } }"
            @confirm="confirmDelete"
        />
    </div>
</template>
