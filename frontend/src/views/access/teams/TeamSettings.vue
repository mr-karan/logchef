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

// Initialize stores with error handling
let usersStore, sourcesStore, teamsStore;
try {
  usersStore = useUsersStore();
  sourcesStore = useSourcesStore();
  teamsStore = useTeamsStore();
} catch (error) {
  console.error("Error initializing stores:", error);
}

// Create fallback objects if stores fail to initialize
if (!teamsStore) {
  console.error("Teams store failed to initialize!");
  teamsStore = {
    getTeam: async () => ({ success: false }),
    listTeamMembers: async () => ({ success: false }),
    listTeamSources: async () => ({ success: false }),
    isLoading: false,
    error: null,
    loadingStates: {},
    isLoadingOperation: () => false,
  };
}

if (!usersStore) {
  console.error("Users store failed to initialize!");
  usersStore = {
    loadUsers: async () => {},
    getUsersNotInTeam: () => [],
    users: []
  };
}

if (!sourcesStore) {
  console.error("Sources store failed to initialize!");
  sourcesStore = {
    loadSources: async () => {},
    getSourcesNotInTeam: () => [],
    sources: []
  };
}

const team = ref<Team | null>(null)
const members = ref<TeamMember[]>([])
const { isLoading, error: teamError } = storeToRefs(teamsStore)
const isSaving = computed(() => {
  if (!teamsStore || typeof teamsStore.isLoadingOperation !== 'function') {
    return false;
  }
  return teamsStore.isLoadingOperation('updateTeam-' + route.params.id) ||
         teamsStore.isLoadingOperation('addTeamMember-' + route.params.id) ||
         teamsStore.isLoadingOperation('removeTeamMember-' + route.params.id) ||
         teamsStore.isLoadingOperation('addTeamSource-' + route.params.id) ||
         teamsStore.isLoadingOperation('removeTeamSource-' + route.params.id);
})

// Form state
const name = ref('')
const description = ref('')

// Add member dialog state
const showAddMemberDialog = ref(false)
const newMemberRole = ref('member')
const selectedUserId = ref('')
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

const loadTeam = async () => {
    const teamId = Number(route.params.id);
    console.log("TeamSettings - Loading team with ID:", teamId, "Route params:", route.params);
    
    if (isNaN(teamId) || teamId <= 0) {
        console.error("Invalid team ID:", teamId);
        return;
    }
    
    const result = await teamsStore.getTeam(teamId);
    console.log("TeamSettings - getTeam result:", result);
    
    if (result.success && result.data) {
        team.value = result.data;
        name.value = team.value.name;
        description.value = team.value.description || '';
        console.log("TeamSettings - Team loaded successfully:", team.value);
    } else {
        console.error("Failed to load team:", result.error);
        toast({
            title: 'Error',
            description: 'Failed to load team details',
            variant: 'destructive',
        });
    }
}

const loadTeamMembers = async () => {
    if (!team.value || !team.value.id) {
        console.warn("Cannot load team members: No team or invalid team ID");
        return;
    }

    console.log("TeamSettings - Loading members for team ID:", team.value.id);
    
    const result = await teamsStore.listTeamMembers(team.value.id);
    console.log("TeamSettings - listTeamMembers result:", result);
    
    if (result.success && result.data) {
        members.value = result.data;
        console.log("TeamSettings - Members loaded successfully:", members.value.length, "members");
    } else {
        console.error("Failed to load team members:", result.error);
    }
}

const loadTeamSources = async () => {
    if (!team.value || !team.value.id) {
        console.warn("Cannot load team sources: No team or invalid team ID");
        return;
    }

    console.log("TeamSettings - Loading sources for team ID:", team.value.id);
    
    const result = await teamsStore.listTeamSources(team.value.id);
    console.log("TeamSettings - listTeamSources result:", result);
    
    if (result.success && result.data) {
        teamSources.value = result.data;
        console.log("TeamSettings - Sources loaded successfully:", teamSources.value.length, "sources");
    } else {
        console.error("Failed to load team sources:", result.error);
    }
}

const handleSubmit = async () => {
    if (!team.value) return

    // Basic validation
    if (!name.value) {
        // Let store handle the error toast
        return
    }

    const result = await teamsStore.updateTeam(team.value.id, {
        name: name.value,
        description: description.value || '',
    })
    
    if (result.success) {
        loadTeam()
    }
}

const handleAddMember = async () => {
    if (!team.value || !selectedUserId.value) return

    const result = await teamsStore.addTeamMember(team.value.id, {
        user_id: Number(selectedUserId.value),
        role: newMemberRole.value as 'admin' | 'member',
    })
    
    if (result.success) {
        // Reset form
        selectedUserId.value = ''
        newMemberRole.value = 'member'
        showAddMemberDialog.value = false

        // Reload members
        loadTeamMembers()
    }
}

const handleRemoveMember = async (userId: string | number) => {
    if (!team.value) return

    const result = await teamsStore.removeTeamMember(team.value.id, Number(userId))
    
    if (result.success) {
        loadTeamMembers()
    }
}

const handleAddSource = async () => {
    if (!team.value || !selectedSourceId.value) return

    const result = await teamsStore.addTeamSource(team.value.id, Number(selectedSourceId.value))
    
    if (result.success) {
        // Reset form
        selectedSourceId.value = ''
        showAddSourceDialog.value = false

        // Reload sources
        loadTeamSources()
    }
}

const handleRemoveSource = async (sourceId: string | number) => {
    if (!team.value) return

    const result = await teamsStore.removeTeamSource(team.value.id, Number(sourceId))
    
    if (result.success) {
        loadTeamSources()
    }
}

onMounted(async () => {
    // Make sure we have a valid team ID from route
    const teamId = Number(route.params.id);
    console.log("TeamSettings - Component mounted. Team ID from route:", teamId);
    
    if (isNaN(teamId) || teamId <= 0) {
        console.error("Invalid team ID in route:", route.params.id);
        toast({
            title: 'Error',
            description: `Invalid team ID: ${route.params.id}`,
            variant: 'destructive',
        });
        return;
    }
    
    try {
        // First load the team
        await loadTeam();
        
        // Only continue if team was loaded successfully
        if (team.value) {
            console.log("TeamSettings - Team loaded, now loading users, members, and sources");
            
            // First load users
            await usersStore.loadUsers();
            
            // Then load members and sources sequentially to avoid race conditions
            await loadTeamMembers();
            await loadTeamSources();
        } else {
            console.warn("TeamSettings - Team not loaded, skipping members and sources");
        }
    } catch (error) {
        console.error("Error in TeamSettings onMounted:", error);
        toast({
            title: 'Error',
            description: 'An error occurred while loading team data',
            variant: 'destructive',
        });
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
