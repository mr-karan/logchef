<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Plus, Trash2, Settings, Search, Loader2 } from 'lucide-vue-next'
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

// Check if user is a global admin
const isGlobalAdmin = computed(() => authStore.user?.role === 'admin')

// Improved loading state
const isLoading = ref(true)
const showDeleteDialog = ref(false)
const teamToDelete = ref<Team | null>(null)
const searchQuery = ref('')

// Filter teams based on search query
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

// Get processed teams with default values
// Uses managedTeams which returns adminTeams for global admins,
// or filtered userTeams (where role is admin) for team admins
const teamsWithDefaults = computed(() => {
    return teamsStore.managedTeams.map(team => ({
        ...team,
        name: team.name || `Team ${team.id}`,
        description: team.description || '',
        memberCount: team.member_count || 0
    }));
});

// Show empty state only if not loading and no teams after search
const showEmptyState = computed(() =>
    !isLoading.value &&
    filteredTeams.value.length === 0 &&
    !searchQuery.value
);

// Show not found state when search has no results
const showNotFoundState = computed(() =>
    !isLoading.value &&
    filteredTeams.value.length === 0 &&
    searchQuery.value.trim() !== ''
);

const handleDelete = (team: Team) => {
    teamToDelete.value = team
    showDeleteDialog.value = true
}

const confirmDelete = async () => {
    if (!teamToDelete.value) return

    try {
        await teamsStore.deleteTeam(teamToDelete.value.id)

        // Reset UI state
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
        <Card>
            <CardHeader>
                <div class="flex items-center justify-between">
                    <div>
                        <CardTitle>Teams</CardTitle>
                        <CardDescription>
                            {{ isGlobalAdmin ? 'Groups of users that have common dashboard and permission needs' : 'Teams you administer' }}
                        </CardDescription>
                    </div>
                    <!-- Only global admins can create teams -->
                    <AddTeam v-if="isGlobalAdmin" @team-created="handleTeamCreated" />
                </div>
            </CardHeader>
            <CardContent>
                <div class="space-y-4">
                    <!-- Enhanced search input -->
                    <div class="relative flex items-center">
                        <Search class="absolute left-3 h-4 w-4 text-muted-foreground" />
                        <Input v-model="searchQuery" type="text" placeholder="Search teams by name or description..."
                            class="pl-9 w-full" />
                    </div>

                    <!-- Loading State -->
                    <div v-if="isLoading" class="flex flex-col items-center justify-center py-10">
                        <Loader2 class="h-8 w-8 animate-spin text-primary mb-2" />
                        <p class="text-muted-foreground">Loading teams...</p>
                    </div>

                    <!-- Empty State -->
                    <div v-else-if="showEmptyState" class="rounded-lg border p-6 text-center">
                        <div class="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-muted mb-4">
                            <Plus class="h-6 w-6" />
                        </div>
                        <h3 class="text-lg font-semibold mb-1">No teams found</h3>
                        <p class="text-muted-foreground mb-4">
                            {{ isGlobalAdmin ? 'Create your first team to get started' : 'You are not an admin of any teams' }}
                        </p>
                        <!-- Only global admins can create teams -->
                        <AddTeam v-if="isGlobalAdmin" @team-created="handleTeamCreated">
                            <Button>
                                <Plus class="mr-2 h-4 w-4" />
                                Create Your First Team
                            </Button>
                        </AddTeam>
                    </div>

                    <!-- Search Not Found State -->
                    <div v-else-if="showNotFoundState" class="rounded-lg border p-6 text-center">
                        <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-muted/50 mb-3">
                            <Search class="h-5 w-5 text-muted-foreground" />
                        </div>
                        <h3 class="text-lg font-semibold mb-1">No results found</h3>
                        <p class="text-muted-foreground">No teams match your search criteria</p>
                    </div>

                    <!-- Teams Table -->
                    <div v-else>
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

                                            <!-- Only global admins can delete teams -->
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
            </CardContent>
        </Card>

        <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = false">
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>Delete Team</AlertDialogTitle>
                    <AlertDialogDescription class="space-y-2">
                        <p>Are you sure you want to delete team "{{ teamToDelete?.name }}"?</p>
                        <p class="font-medium text-destructive">
                            This action cannot be undone.
                        </p>
                    </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel @click="showDeleteDialog = false">
                        Cancel
                    </AlertDialogCancel>
                    <AlertDialogAction @click="confirmDelete"
                        class="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                        Delete
                    </AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    </div>
</template>
