<script setup lang="ts">
import { onMounted, ref } from 'vue'
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
import { Plus, Trash2, Settings } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { format } from 'date-fns'
import { type Team } from '@/api/teams'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import AddTeam from './AddTeam.vue'
import { useTeamsStore } from '@/stores/teams'
import { storeToRefs } from 'pinia'

const router = useRouter()
const teamsStore = useTeamsStore()
const { teams, isLoading } = storeToRefs(teamsStore)
const showDeleteDialog = ref(false)
const teamToDelete = ref<Team | null>(null)

const handleDelete = (team: Team) => {
    teamToDelete.value = team
    showDeleteDialog.value = true
}

// Function to refresh teams list
const refreshTeams = async () => {
    await teamsStore.loadTeams(true) // Force reload
}

const confirmDelete = async () => {
    if (!teamToDelete.value) return
    const success = await teamsStore.deleteTeam(teamToDelete.value.id)
    if (success) {
        showDeleteDialog.value = false
        teamToDelete.value = null
    }
}

onMounted(() => {
    teamsStore.loadTeams()
})

const formatDate = (dateString: string) => {
    try {
        const date = new Date(dateString)
        return format(date, 'PPp') // Format like "Feb 17, 2025, 3:17 PM"
    } catch (error) {
        console.error('Error formatting date:', error)
        return 'Invalid date'
    }
}
</script>

<template>
    <div class="space-y-6">
        <Card>
            <CardHeader>
                <div class="flex items-center justify-between">
                    <div>
                        <CardTitle>Teams</CardTitle>
                        <CardDescription>
                            Groups of users that have common dashboard and permission needs
                        </CardDescription>
                    </div>
                    <AddTeam @team-created="refreshTeams" />
                </div>
            </CardHeader>
            <CardContent>
                <div class="space-y-4">
                    <!-- Search input -->
                    <div class="relative">
                        <input type="text" placeholder="Search teams..."
                            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
                    </div>

                    <div v-if="isLoading" class="text-center py-4">
                        Loading teams...
                    </div>
                    <div v-else-if="teams.length === 0" class="rounded-lg border p-4 text-center">
                        <p class="text-muted-foreground mb-4">No teams found</p>
                        <AddTeam @team-created="refreshTeams">
                            <Button>
                                <Plus class="mr-2 h-4 w-4" />
                                Create Your First Team
                            </Button>
                        </AddTeam>
                    </div>
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
                                <TableRow v-for="team in teams" :key="team.id">
                                    <TableCell>
                                        <router-link :to="{ name: 'TeamSettings', params: { id: team.id } }"
                                            class="font-medium hover:underline flex items-center gap-2">
                                            {{ team.name }}
                                        </router-link>
                                    </TableCell>
                                    <TableCell>{{ team.description }}</TableCell>
                                    <TableCell>{{ team.memberCount }}</TableCell>
                                    <TableCell>{{ formatDate(team.created_at) }}</TableCell>
                                    <TableCell class="text-right">
                                        <div class="flex justify-end gap-2">
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <Button variant="ghost" size="icon"
                                                        @click="router.push({ name: 'TeamSettings', params: { id: team.id } })">
                                                        <Settings class="h-4 w-4" />
                                                    </Button>
                                                </TooltipTrigger>
                                                <TooltipContent>
                                                    <p>Edit team settings</p>
                                                </TooltipContent>
                                            </Tooltip>

                                            <Tooltip>
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
