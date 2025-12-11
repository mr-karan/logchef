<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/composables/useToast'
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
const router = useRouter()
const { toast } = useToast()

// Initialize stores with proper Pinia pattern
const usersStore = useUsersStore()
const sourcesStore = useSourcesStore()
const teamsStore = useTeamsStore()
// Get the teamId from route params
const teamId = computed(() => Number(route.params.id))

// Single loading state for better UX
const isLoading = ref(true)

// Computed properties for cleaner store access
const team = computed(() => teamsStore.getTeamById(teamId.value))
const members = computed(() => teamsStore.getTeamMembersByTeamId(teamId.value) || [])
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(teamId.value) || [])

// Combined saving state - more specific than overall loading
const isSaving = computed(() => {
    return teamsStore.isLoadingOperation('updateTeam-' + teamId.value) ||
        teamsStore.isLoadingOperation('addTeamMember-' + teamId.value) ||
        teamsStore.isLoadingOperation('removeTeamMember-' + teamId.value) ||
        teamsStore.isLoadingOperation('addTeamSource-' + teamId.value) ||
        teamsStore.isLoadingOperation('removeTeamSource-' + teamId.value);
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

// Compute available users (users not in team) sorted alphabetically by email
const availableUsers = computed(() => {
    const teamMemberIds = members.value?.map(m => String(m.user_id)) || []
    const users = usersStore.getUsersNotInTeam(teamMemberIds)
    return users.sort((a, b) => a.email.localeCompare(b.email))
})

// Compute available sources (sources not in team) sorted alphabetically by name
const availableSources = computed(() => {
    const teamSourceIds = teamSources.value?.map((s: Source) => s.id) || []
    const sources = sourcesStore.getSourcesNotInTeam(teamSourceIds)
    return sources.sort((a, b) => a.name.localeCompare(b.name))
})

// Load users when dialog opens to prevent unnecessary API calls
watch(showAddMemberDialog, async (isOpen) => {
    if (isOpen && !usersStore.users.length) {
        await usersStore.loadUsers()
    }
})

// Load sources when dialog opens to prevent unnecessary API calls
watch(showAddSourceDialog, async (isOpen) => {
    if (isOpen && !sourcesStore.sources.length) {
        await sourcesStore.loadSources()
    }
})

// Simplified submit function
const handleSubmit = async () => {
    if (!team.value) return

    // Basic validation
    if (!name.value) {
        toast({
            title: 'Error',
            description: 'Team name is required',
            variant: 'destructive',
        })
        return
    }

    const result = await teamsStore.updateTeam(team.value.id, {
        name: name.value,
        description: description.value || '',
    })

}


const handleAddMember = async () => {
    if (!team.value || !selectedUserId.value) return

    const result = await teamsStore.addTeamMember(team.value.id, {
        user_id: Number(selectedUserId.value),
        role: newMemberRole.value as 'admin' | 'member' | 'editor',
    })

    if (result.success) {
        // Reset form
        selectedUserId.value = ''
        newMemberRole.value = 'member'
        showAddMemberDialog.value = false
    }
}

const handleRemoveMember = async (userId: string | number) => {
    if (!team.value) return;

    try {
        const result = await teamsStore.removeTeamMember(team.value.id, Number(userId));

        if (!result.success) {
            // API call failed, show an error toast.
            // The success toast ("Member removed successfully") is handled by the store's callApi utility.
            toast({
                title: 'Error',
                description: result.error?.message || 'Failed to remove team member.',
                variant: 'destructive',
            });
        }
        // If result.success is true, the store handles the success toast.
    } catch (error) {
        // This catch is for unexpected errors during the teamsStore.removeTeamMember call itself
        console.error('Error removing team member:', error);
        toast({
            title: 'Error',
            description: 'An unexpected error occurred while trying to remove the team member.',
            variant: 'destructive',
        });
    }
};

const handleAddSource = async () => {
    if (!team.value || !selectedSourceId.value) return

    // Make sure we're on the sources tab
    activeTab.value = 'sources'

    const result = await teamsStore.addTeamSource(team.value.id, Number(selectedSourceId.value))

    if (result.success) {
        // Reset form
        selectedSourceId.value = ''
        showAddSourceDialog.value = false
    }
}

const handleRemoveSource = async (sourceId: string | number) => {
    if (!team.value) return

    // Make sure we're on the sources tab
    activeTab.value = 'sources'

    await teamsStore.removeTeamSource(team.value.id, Number(sourceId))
}

// Optimized initialization to load everything in parallel
onMounted(async () => {
    const id = teamId.value

    if (isNaN(id) || id <= 0) {
        toast({
            title: 'Error',
            description: `Invalid team ID: ${route.params.id}`,
            variant: 'destructive',
        })
        isLoading.value = false
        return
    }

    try {
        isLoading.value = true

        // Load basic data in parallel for better performance
        await Promise.all([
            // Load admin teams first to ensure we have the team in the store
            teamsStore.loadAdminTeams(),

            // Load these in parallel for efficiency
            usersStore.loadUsers(),
            sourcesStore.loadSources()
        ])

        // Get detailed team info after confirming admin teams are loaded
        await teamsStore.getTeam(id)

        // Load team-specific data in parallel
        await Promise.all([
            teamsStore.listTeamMembers(id),
            teamsStore.listTeamSources(id)
        ])

    } catch (error) {
        console.error("Error loading team settings:", error)
        toast({
            title: 'Error',
            description: 'An error occurred while loading team data. Please try again.',
            variant: 'destructive',
        })
    } finally {
        isLoading.value = false
    }
})
</script>

<template>
    <div class="space-y-6">
        <div v-if="isLoading" class="flex items-center justify-center py-10">
            <div class="flex flex-col items-center">
                <div class="animate-spin w-10 h-10 rounded-full border-4 border-primary border-t-transparent mb-4">
                </div>
                <p class="text-muted-foreground">Loading team settings...</p>
            </div>
        </div>
        <div v-else-if="!team" class="text-center py-12">
            <h3 class="text-lg font-medium mb-2">Team not found</h3>
            <p class="text-muted-foreground mb-4">The team you're looking for doesn't exist or you don't have access.
            </p>
            <Button variant="outline" @click="router.push('/access/teams')">
                Back to Teams
            </Button>
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
                                                            :value="String(user.id)" :text-value="user.email">
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
                                                        <SelectItem value="editor">Editor</SelectItem>
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
                            <div v-if="teamsStore.isLoadingTeamMembers(teamId)" class="text-center py-4">
                                <Loader2 class="h-6 w-6 animate-spin mx-auto mb-2" />
                                <p class="text-sm text-muted-foreground">Loading members...</p>
                            </div>
                            <Table v-else>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Email</TableHead>
                                        <TableHead>Role</TableHead>
                                        <TableHead>Added</TableHead>
                                        <TableHead class="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-if="members.length === 0">
                                        <TableCell colspan="4" class="text-center py-4 text-muted-foreground">
                                            No members found
                                        </TableCell>
                                    </TableRow>
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
                                                            :value="String(source.id)"
                                                            :text-value="formatSourceName(source)">
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
                            <div v-if="teamsStore.isLoadingTeamSources(teamId)" class="text-center py-4">
                                <Loader2 class="h-6 w-6 animate-spin mx-auto mb-2" />
                                <p class="text-sm text-muted-foreground">Loading sources...</p>
                            </div>
                            <Table v-else>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Source</TableHead>
                                        <TableHead>Description</TableHead>
                                        <TableHead>Added</TableHead>
                                        <TableHead class="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-if="teamSources.length === 0">
                                        <TableCell colspan="4" class="text-center py-4 text-muted-foreground">
                                            No sources found
                                        </TableCell>
                                    </TableRow>
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
