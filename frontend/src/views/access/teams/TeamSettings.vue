<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/components/ui/toast/use-toast'
import { type Team, type TeamMember } from '@/api/teams'
import { type Source } from '@/api/sources'
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
import { useTeamsStore } from "@/stores/teams"
import { formatDate, formatSourceName } from '@/utils/format'

const route = useRoute()
const { toast } = useToast()

// Initialize stores with proper Pinia pattern
const usersStore = useUsersStore()
const sourcesStore = useSourcesStore()
const teamsStore = useTeamsStore()

// Get reactive state from the stores
const { isLoading, error: teamError } = storeToRefs(teamsStore)

// Computed properties for cleaner store access
const team = computed(() => teamsStore.getTeamById(Number(route.params.id)))
const members = computed(() => teamsStore.getTeamMembersByTeamId(Number(route.params.id)) || [])
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(Number(route.params.id)) || [])

// Combined loading state
const isSaving = computed(() => {
    const teamId = Number(route.params.id)
    return teamsStore.isLoadingOperation('updateTeam-' + teamId) ||
        teamsStore.isLoadingOperation('addTeamMember-' + teamId) ||
        teamsStore.isLoadingOperation('removeTeamMember-' + teamId) ||
        teamsStore.isLoadingOperation('addTeamSource-' + teamId) ||
        teamsStore.isLoadingOperation('removeTeamSource-' + teamId);
})

// Form state - use team data when available
const name = ref('')
const description = ref('')

// UI state
const activeTab = ref('members')
const showAddMemberDialog = ref(false)
const newMemberRole = ref('member')
const selectedUserId = ref('')
const showAddSourceDialog = ref(false)
const selectedSourceId = ref('')

// Sync form data when team changes
watch(() => team.value, (newTeam) => {
    if (newTeam) {
        name.value = newTeam.name || ''
        description.value = newTeam.description || ''
    }
}, { immediate: true })

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

// Simplified loading functions that rely on the store
const loadTeam = async () => {
    const teamId = Number(route.params.id)

    if (isNaN(teamId) || teamId <= 0) {
        toast({
            title: 'Error',
            description: 'Invalid team ID',
            variant: 'destructive',
        })
        return
    }

    await teamsStore.getTeam(teamId)
    // Store automatically handles errors and success
}

const loadTeamMembers = async () => {
    const teamId = Number(route.params.id)

    if (isNaN(teamId) || teamId <= 0) return

    await teamsStore.listTeamMembers(teamId)
    // Store automatically handles errors and success
}

const loadTeamSources = async () => {
    const teamId = Number(route.params.id)

    if (isNaN(teamId) || teamId <= 0) return

    await teamsStore.listTeamSources(teamId)
    // Store automatically handles errors and success
}

const handleSubmit = async () => {
    if (!team.value) return

    // Basic validation
    if (!name.value) return

    await teamsStore.updateTeam(team.value.id, {
        name: name.value,
        description: description.value || '',
    })
    // Store handles success/error states
}

const handleAddMember = async () => {
    if (!team.value || !selectedUserId.value) return

    await teamsStore.addTeamMember(team.value.id, {
        user_id: Number(selectedUserId.value),
        role: newMemberRole.value as 'admin' | 'member',
    })

    // Reset form
    selectedUserId.value = ''
    newMemberRole.value = 'member'
    showAddMemberDialog.value = false
    // Store automatically updates the members list
}

const handleRemoveMember = async (userId: string | number) => {
    if (!team.value) return

    await teamsStore.removeTeamMember(team.value.id, Number(userId))
    // Store automatically updates the members list
}

const handleAddSource = async () => {
    if (!team.value || !selectedSourceId.value) return

    // Make sure we're on the sources tab
    activeTab.value = 'sources'

    await teamsStore.addTeamSource(team.value.id, Number(selectedSourceId.value))

    // Reset form
    selectedSourceId.value = ''
    showAddSourceDialog.value = false
    // Store automatically updates the sources list
}

const handleRemoveSource = async (sourceId: string | number) => {
    if (!team.value) return

    // Make sure we're on the sources tab
    activeTab.value = 'sources'

    await teamsStore.removeTeamSource(team.value.id, Number(sourceId))
    // Store automatically updates the sources list
}

onMounted(async () => {
    const teamId = Number(route.params.id)

    if (isNaN(teamId) || teamId <= 0) {
        toast({
            title: 'Error',
            description: `Invalid team ID: ${route.params.id}`,
            variant: 'destructive',
        })
        return
    }

    // Clear existing team data to prevent stale data issues
    teamsStore.clearState()

    try {
        // Load all data in parallel for performance
        await Promise.all([
            usersStore.loadUsers(),
            sourcesStore.loadSources(),
            teamsStore.getTeam(teamId),
            teamsStore.listTeamMembers(teamId),
            teamsStore.listTeamSources(teamId)
        ])
    } catch (error) {
        // Store handles individual API errors automatically
        toast({
            title: 'Error',
            description: 'An error occurred while loading team data',
            variant: 'destructive',
        })
    }
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
            <Tabs v-model="activeTab" class="space-y-6">
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
                                                            :value="String(user.id)">
                                                            {{ user.email }}
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
                                                <span>{{ member.email }}</span>
                                                <span class="text-sm text-muted-foreground">{{ member.full_name
                                                }}</span>
                                            </div>
                                        </TableCell>
                                        <TableCell class="capitalize">{{ member.role }}</TableCell>
                                        <TableCell>{{ formatDate(member.created_at) }}</TableCell>
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
                                        <Button @click="activeTab = 'sources'">
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
