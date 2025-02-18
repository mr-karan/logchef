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
import { Plus, Trash2, Settings, Users } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useToast } from '@/components/ui/toast/use-toast'
import { TOAST_DURATION } from '@/lib/constants'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { format } from 'date-fns'
import { teamsApi, type Team, type TeamMember } from '@/api/users'
import { isErrorResponse, isSuccessResponse, getErrorMessage, type APIResponse } from '@/api/types'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const router = useRouter()
const { toast } = useToast()
const teams = ref<(Team & { memberCount: number })[]>([])
const isLoading = ref(true)
const showDeleteDialog = ref(false)
const teamToDelete = ref<Team | null>(null)

const loadTeams = async () => {
    try {
        isLoading.value = true
        const response = await teamsApi.listTeams()

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            teams.value = []
            return
        }

        // Get member counts for each team
        const teamsWithMembers = await Promise.all(
            response.data.teams.map(async (team) => {
                const membersResponse = await teamsApi.listTeamMembers(team.id)
                const memberCount = isErrorResponse(membersResponse) ? 0 : membersResponse.data.members.length
                return { ...team, memberCount }
            })
        )

        teams.value = teamsWithMembers
        console.log('Loaded teams:', teams.value)
    } catch (error) {
        console.error('Error loading teams:', error)
        teams.value = []
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isLoading.value = false
    }
}

const handleDelete = (team: Team) => {
    teamToDelete.value = team
    showDeleteDialog.value = true
}

const confirmDelete = async () => {
    if (!teamToDelete.value) return

    try {
        // Log the team being deleted to verify the ID
        console.log('Deleting team:', teamToDelete.value)

        const response = await teamsApi.deleteTeam(teamToDelete.value.id)

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        if (isSuccessResponse(response)) {
            toast({
                title: 'Success',
                description: response.data.message,
                duration: TOAST_DURATION.SUCCESS,
            })
            await loadTeams()
        }
    } catch (error) {
        console.error('Error deleting team:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        showDeleteDialog.value = false
        teamToDelete.value = null
    }
}

onMounted(loadTeams)

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
                    <Button @click="router.push({ name: 'NewTeam' })">
                        <Plus class="mr-2 h-4 w-4" />
                        Add Team
                    </Button>
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
                        <Button @click="router.push({ name: 'NewTeam' })">
                            <Plus class="mr-2 h-4 w-4" />
                            Create Your First Team
                        </Button>
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