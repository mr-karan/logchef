<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { teamsApi, type Team, type TeamMember } from '@/api/teams'
import { type Source } from '@/api/sources'
import { isErrorResponse, getErrorMessage } from '@/api/types'
import { format } from 'date-fns'
import { Loader2, Plus, Trash2, UserPlus, Database } from 'lucide-vue-next'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import { useUsersStore } from "@/stores/users"
import { useSourcesStore } from "@/stores/sources"
import { formatDate, formatSourceName } from '@/utils/format'

const route = useRoute()
const { toast } = useToast()

const team = ref<Team | null>(null)
const members = ref<TeamMember[]>([])
const isLoading = ref(true)
const isSaving = ref(false)

// Form state
const name = ref('')
const description = ref('')

// Add member dialog state
const showAddMemberDialog = ref(false)
const newMemberRole = ref('member')
const selectedUserId = ref('')
const usersStore = useUsersStore()

// Add sources store
const sourcesStore = useSourcesStore()
const teamSources = ref<Source[]>([])
const showAddSourceDialog = ref(false)
const selectedSourceId = ref('')

// Compute available users (users not in team)
const availableUsers = computed(() => {
    const teamMemberIds = members.value?.map(m => String(m.user_id)) || []
    return usersStore.getUsersNotInTeam(teamMemberIds)
})

// Compute available sources (sources not in team)
const availableSources = computed(() => {
    const teamSourceIds = teamSources.value?.map((s: Source) => s.id) || []
    return sourcesStore.getSourcesNotInTeam(teamSourceIds)
})

// Load users when dialog opens
watch(showAddMemberDialog, async (isOpen) => {
    if (isOpen && !usersStore.users.length) {
        await usersStore.loadUsers()
    }
})

// Load sources when dialog opens
watch(showAddSourceDialog, async (isOpen) => {
    if (isOpen && !sourcesStore.sources.length) {
        await sourcesStore.loadSources()
    }
})

// Add this computed function after other computed properties
const getMemberUser = (userId: string | number) => {
    return usersStore.users.find(user => user.id === String(userId))
}

const loadTeam = async () => {
    try {
        isLoading.value = true
        const response = await teamsApi.getTeam(Number(route.params.id))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        team.value = response.data
        name.value = team.value.name
        description.value = team.value.description || ''

        // Load team members
        await loadTeamMembers()
    } catch (error) {
        console.error('Error loading team:', error)
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

const loadTeamMembers = async () => {
    try {
        const response = await teamsApi.listTeamMembers(Number(route.params.id))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        members.value = response.data
    } catch (error) {
        console.error('Error loading team members:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    }
}

const loadTeamSources = async () => {
    try {
        const response = await teamsApi.listTeamSources(Number(route.params.id))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        teamSources.value = response.data
    } catch (error) {
        console.error('Error loading team sources:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    }
}

const handleSubmit = async () => {
    if (isSaving.value || !team.value) return

    // Basic validation
    if (!name.value) {
        toast({
            title: 'Error',
            description: 'Please enter a team name',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    isSaving.value = true

    try {
        const response = await teamsApi.updateTeam(team.value.id, {
            name: name.value,
            description: description.value,
        })

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Team updated successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Reload team data
        await loadTeam()
    } catch (error) {
        console.error('Error updating team:', error)
        toast({
            title: 'Error',
            description: 'Failed to update team. Please try again.',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSaving.value = false
    }
}

const handleAddMember = async () => {
    if (!team.value || !selectedUserId.value) return

    try {
        isSaving.value = true
        const response = await teamsApi.addTeamMember(team.value.id, {
            user_id: Number(selectedUserId.value),
            role: newMemberRole.value as 'admin' | 'member',
        })

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Member added successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Reset form
        selectedUserId.value = ''
        newMemberRole.value = 'member'
        showAddMemberDialog.value = false

        // Reload members
        await loadTeamMembers()
    } catch (error) {
        console.error('Error adding team member:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSaving.value = false
    }
}

const handleRemoveMember = async (userId: string | number) => {
    if (!team.value) return

    try {
        isSaving.value = true
        const response = await teamsApi.removeTeamMember(team.value.id, Number(userId))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Member removed successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Reload members
        await loadTeamMembers()
    } catch (error) {
        console.error('Error removing team member:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSaving.value = false
    }
}

const handleAddSource = async () => {
    if (!team.value || !selectedSourceId.value) return

    try {
        isSaving.value = true
        const response = await teamsApi.addTeamSource(team.value.id, Number(selectedSourceId.value))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Source added successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Reset form
        selectedSourceId.value = ''
        showAddSourceDialog.value = false

        // Reload sources
        await loadTeamSources()
    } catch (error) {
        console.error('Error adding team source:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSaving.value = false
    }
}

const handleRemoveSource = async (sourceId: string | number) => {
    if (!team.value) return

    try {
        isSaving.value = true
        const response = await teamsApi.removeTeamSource(team.value.id, Number(sourceId))

        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Source removed successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Reload sources
        await loadTeamSources()
    } catch (error) {
        console.error('Error removing team source:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSaving.value = false
    }
}

onMounted(async () => {
    await Promise.all([
        loadTeam(),
        usersStore.loadUsers(),
        loadTeamSources(),
    ])
})
</script>

<template>
    <div class="space-y-6">
        <div v-if="isLoading" class="text-center py-4">
            Loading team...
        </div>
        <div v-else-if="!team" class="text-center py-4">
            Team not found
        </div>
        <template v-else>
            <!-- Header -->
            <div>
                <h1 class="text-2xl font-bold tracking-tight">{{ team.name }}</h1>
                <p class="text-muted-foreground mt-2">
                    Manage team settings and members
                </p>
            </div>

            <!-- Tabs -->
            <Tabs defaultValue="members" class="space-y-6">
                <TabsList>
                    <TabsTrigger value="members">Members</TabsTrigger>
                    <TabsTrigger value="sources">Sources</TabsTrigger>
                    <TabsTrigger value="settings">Settings</TabsTrigger>
                </TabsList>

                <!-- Members Tab -->
                <TabsContent value="members">
                    <Card>
                        <CardHeader>
                            <div class="flex items-center justify-between">
                                <div>
                                    <CardTitle>Team Members</CardTitle>
                                    <CardDescription>
                                        Manage team members and their roles
                                    </CardDescription>
                                </div>
                                <Dialog v-model:open="showAddMemberDialog">
                                    <DialogTrigger asChild>
                                        <Button>
                                            <UserPlus class="mr-2 h-4 w-4" />
                                            Add Member
                                        </Button>
                                    </DialogTrigger>
                                    <DialogContent>
                                        <DialogHeader>
                                            <DialogTitle>Add Team Member</DialogTitle>
                                            <DialogDescription>
                                                Add a new member to the team
                                            </DialogDescription>
                                        </DialogHeader>
                                        <div class="space-y-4 py-4">
                                            <div class="space-y-2">
                                                <Label>User</Label>
                                                <Select v-model="selectedUserId">
                                                    <SelectTrigger>
                                                        <SelectValue placeholder="Select a user" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem v-for="user in availableUsers" :key="user.id"
                                                            :value="user.id">
                                                            {{ user.email }} ({{ user.full_name }})
                                                        </SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                            <div class="space-y-2">
                                                <Label>Role</Label>
                                                <Select v-model="newMemberRole">
                                                    <SelectTrigger>
                                                        <SelectValue placeholder="Select a role" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value="member">Member</SelectItem>
                                                        <SelectItem value="admin">Admin</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                        </div>
                                        <DialogFooter>
                                            <Button variant="outline" @click="showAddMemberDialog = false">
                                                Cancel
                                            </Button>
                                            <Button :disabled="isSaving" @click="handleAddMember">
                                                <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                                <Plus v-else class="mr-2 h-4 w-4" />
                                                Add Member
                                            </Button>
                                        </DialogFooter>
                                    </DialogContent>
                                </Dialog>
                            </div>
                        </CardHeader>
                        <CardContent>
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Email</TableHead>
                                        <TableHead>Role</TableHead>
                                        <TableHead>Added</TableHead>
                                        <TableHead class="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-for="member in members" :key="member.user_id">
                                        <TableCell>
                                            <div class="flex flex-col">
                                                <span>{{ getMemberUser(member.user_id)?.email }}</span>
                                                <span class="text-sm text-muted-foreground">{{
                                                    getMemberUser(member.user_id)?.full_name }}</span>
                                            </div>
                                        </TableCell>
                                        <TableCell class="capitalize">{{ member.role }}</TableCell>
                                        <TableCell>{{ format(new Date(member.created_at), 'PPp') }}</TableCell>
                                        <TableCell class="text-right">
                                            <Button variant="destructive" size="icon" :disabled="isSaving"
                                                @click="handleRemoveMember(member.user_id)">
                                                <Loader2 v-if="isSaving" class="h-4 w-4 animate-spin" />
                                                <Trash2 v-else class="h-4 w-4" />
                                            </Button>
                                        </TableCell>
                                    </TableRow>
                                </TableBody>
                            </Table>
                        </CardContent>
                    </Card>
                </TabsContent>

                <!-- Sources Tab -->
                <TabsContent value="sources">
                    <Card>
                        <CardHeader>
                            <div class="flex items-center justify-between">
                                <div>
                                    <CardTitle>Team Sources</CardTitle>
                                    <CardDescription>
                                        Manage data sources for this team
                                    </CardDescription>
                                </div>
                                <Dialog v-model:open="showAddSourceDialog">
                                    <DialogTrigger asChild>
                                        <Button>
                                            <Database class="mr-2 h-4 w-4" />
                                            Add Source
                                        </Button>
                                    </DialogTrigger>
                                    <DialogContent>
                                        <DialogHeader>
                                            <DialogTitle>Add Data Source</DialogTitle>
                                            <DialogDescription>
                                                Add a data source to the team
                                            </DialogDescription>
                                        </DialogHeader>
                                        <div class="space-y-4 py-4">
                                            <div class="space-y-2">
                                                <Label>Source</Label>
                                                <Select v-model="selectedSourceId">
                                                    <SelectTrigger>
                                                        <SelectValue placeholder="Select a source" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem v-for="source in availableSources" :key="source.id"
                                                            :value="String(source.id)">
                                                            {{ formatSourceName(source) }}
                                                        </SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                        </div>
                                        <DialogFooter>
                                            <Button variant="outline" @click="showAddSourceDialog = false">
                                                Cancel
                                            </Button>
                                            <Button :disabled="isSaving" @click="handleAddSource">
                                                <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                                <Plus v-else class="mr-2 h-4 w-4" />
                                                Add Source
                                            </Button>
                                        </DialogFooter>
                                    </DialogContent>
                                </Dialog>
                            </div>
                        </CardHeader>
                        <CardContent>
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Source</TableHead>
                                        <TableHead>Description</TableHead>
                                        <TableHead>Added</TableHead>
                                        <TableHead class="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-for="source in teamSources" :key="source.id">
                                        <TableCell>{{ formatSourceName(source) }}</TableCell>
                                        <TableCell>{{ source.description }}</TableCell>
                                        <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                                        <TableCell class="text-right">
                                            <Button variant="destructive" size="icon" :disabled="isSaving"
                                                @click="handleRemoveSource(source.id)">
                                                <Loader2 v-if="isSaving" class="h-4 w-4 animate-spin" />
                                                <Trash2 v-else class="h-4 w-4" />
                                            </Button>
                                        </TableCell>
                                    </TableRow>
                                </TableBody>
                            </Table>
                        </CardContent>
                    </Card>
                </TabsContent>

                <!-- Settings Tab -->
                <TabsContent value="settings">
                    <Card>
                        <CardHeader>
                            <CardTitle>Team Settings</CardTitle>
                            <CardDescription>
                                Update team information
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <form @submit.prevent="handleSubmit" class="space-y-6">
                                <div class="space-y-4">
                                    <div class="grid gap-2">
                                        <Label for="name">Team Name</Label>
                                        <Input id="name" v-model="name" required />
                                    </div>

                                    <div class="grid gap-2">
                                        <Label for="description">Description</Label>
                                        <Textarea id="description" v-model="description" placeholder="Team description"
                                            rows="3" />
                                    </div>
                                </div>

                                <div class="flex justify-end">
                                    <Button type="submit" :disabled="isSaving">
                                        <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                        Save Changes
                                    </Button>
                                </div>
                            </form>
                        </CardContent>
                    </Card>
                </TabsContent>
            </Tabs>
        </template>
    </div>
</template>