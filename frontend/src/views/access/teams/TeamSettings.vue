<script setup lang="ts">
import { ref, onMounted, computed, watch, reactive } from 'vue'
import { storeToRefs } from 'pinia'
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
import type { RoomSummary, RoomMemberDetail, RoomChannel, AddRoomMemberRequest, CreateRoomChannelRequest } from '@/api/rooms'
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
import { useAuthStore } from "@/stores/auth"
import { useRoomsStore } from "@/stores/rooms"
import { formatDate, formatSourceName } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const { toast } = useToast()

// Initialize stores with proper Pinia pattern
const usersStore = useUsersStore()
const sourcesStore = useSourcesStore()
const teamsStore = useTeamsStore()
const authStore = useAuthStore()
const roomsStore = useRoomsStore()

// Get the teamId from route params
const teamId = computed(() => Number(route.params.id))

// Single loading state for better UX
const isLoading = ref(true)

// Get reactive state from the stores
const { error: teamError } = storeToRefs(teamsStore)

// Computed properties for cleaner store access
const team = computed(() => teamsStore.getTeamById(teamId.value))
const members = computed(() => teamsStore.getTeamMembersByTeamId(teamId.value) || [])
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(teamId.value) || [])
const rooms = computed(() => roomsStore.rooms)

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
const selectedRoomId = ref<number | null>(null)
const showCreateRoomDialog = ref(false)
const createRoomForm = reactive({ name: '', description: '' })
const showAddRoomMemberDialog = ref(false)
const selectedRoomUserId = ref('')
const addEntireTeamLoading = ref(false)
const channelType = ref<'slack' | 'webhook'>('slack')
const channelForm = reactive({ name: '', url: '' })

// Sync form data when team changes
watch(() => team.value, (newTeam) => {
    if (newTeam) {
        name.value = newTeam.name || ''
        description.value = newTeam.description || ''
    }
}, { immediate: true })

watch(() => teamId.value, async (newTeamId, oldTeamId) => {
    if (!newTeamId || newTeamId === oldTeamId) return
    await roomsStore.fetchRooms(newTeamId)
    selectedRoomId.value = rooms.value[0]?.id ?? null
    if (selectedRoomId.value) {
        await roomsStore.fetchMembers(newTeamId, selectedRoomId.value)
        await roomsStore.fetchChannels(newTeamId, selectedRoomId.value)
    }
}, { immediate: true })

watch(rooms, (newRooms) => {
    if (!newRooms.length) {
        selectedRoomId.value = null
        return
    }
    if (!selectedRoomId.value || !newRooms.some((room) => room.id === selectedRoomId.value)) {
        selectedRoomId.value = newRooms[0].id
    }
})

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

const roomMembers = computed(() => {
    if (!selectedRoomId.value) return [] as RoomMemberDetail[];
    return roomsStore.membersByRoom[selectedRoomId.value] || [];
})

const roomChannels = computed(() => {
    if (!selectedRoomId.value) return [] as RoomChannel[];
    return roomsStore.channelsByRoom[selectedRoomId.value] || [];
})

const selectedRoom = computed(() => rooms.value.find((room) => room.id === selectedRoomId.value) || null)

const availableRoomUsers = computed(() => {
    const memberIds = new Set(roomMembers.value.map((member) => member.user_id))
    return members.value.filter((member) => !memberIds.has(member.user_id))
})

watch(selectedRoomId, async (roomId) => {
    if (!roomId || !teamId.value) return
    await roomsStore.fetchMembers(teamId.value, roomId)
    await roomsStore.fetchChannels(teamId.value, roomId)
})

// Load users when dialog opens to prevent unnecessary API calls
watch(showAddMemberDialog, async (isOpen) => {
    if (isOpen && !usersStore.users.length) {
        await usersStore.loadUsers()
    }
})

watch(showAddRoomMemberDialog, async (isOpen) => {
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

    if (result.success) {
        toast({
            title: 'Success',
            description: 'Team settings updated successfully',
            variant: 'default',
        })
    }
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

const handleCreateRoom = async () => {
    if (!team.value) return
    if (!createRoomForm.name.trim()) {
        toast({ title: 'Error', description: 'Room name is required', variant: 'destructive' })
        return
    }
    const result = await roomsStore.createRoom(team.value.id, {
        name: createRoomForm.name.trim(),
        description: createRoomForm.description.trim(),
    })
    if (result.success) {
        showCreateRoomDialog.value = false
        createRoomForm.name = ''
        createRoomForm.description = ''
        if (team.value?.id) {
            await roomsStore.fetchRooms(team.value.id)
            selectedRoomId.value = rooms.value[0]?.id ?? null
        }
    }
}

const handleSelectRoom = async (roomId: number) => {
    selectedRoomId.value = roomId
}

const handleAddRoomMember = async () => {
    if (!team.value || !selectedRoomId.value || !selectedRoomUserId.value) return
    const payload: AddRoomMemberRequest = {
        user_id: Number(selectedRoomUserId.value),
        role: newMemberRole.value,
    }
    const result = await roomsStore.addMember(team.value.id, selectedRoomId.value, payload)
    if (result.success) {
        selectedRoomUserId.value = ''
        showAddRoomMemberDialog.value = false
    }
}

const handleRemoveRoomMember = async (userId: number) => {
    if (!team.value || !selectedRoomId.value) return
    await roomsStore.removeMember(team.value.id, selectedRoomId.value, userId)
}

const handleAddEntireTeamToRoom = async () => {
    if (!team.value || !selectedRoomId.value) return
    addEntireTeamLoading.value = true
    try {
        const existingIds = new Set(roomMembers.value.map((member) => member.user_id))
        for (const member of members.value) {
            if (!existingIds.has(member.user_id)) {
                await roomsStore.addMember(team.value.id, selectedRoomId.value, {
                    user_id: member.user_id,
                    role: 'member',
                })
            }
        }
        await roomsStore.fetchMembers(team.value.id, selectedRoomId.value)
    } finally {
        addEntireTeamLoading.value = false
    }
}

const handleCreateChannel = async () => {
    if (!team.value || !selectedRoomId.value) return
    if (!channelForm.url.trim()) {
        toast({ title: 'Error', description: 'Channel URL is required', variant: 'destructive' })
        return
    }
    const payload: CreateRoomChannelRequest = {
        type: channelType.value,
        name: channelForm.name.trim(),
        config: { url: channelForm.url.trim() },
        enabled: true,
    }
    const result = await roomsStore.createChannel(team.value.id, selectedRoomId.value, payload)
    if (result.success) {
        channelForm.name = ''
        channelForm.url = ''
    }
}

const handleDeleteChannel = async (channelId: number) => {
    if (!team.value || !selectedRoomId.value) return
    await roomsStore.deleteChannel(team.value.id, selectedRoomId.value, channelId)
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
                    <TabsTrigger value="rooms">Rooms</TabsTrigger>
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

                <!-- Rooms Tab -->
                <TabsContent value="rooms">
                    <div class="grid gap-6 lg:grid-cols-[280px,1fr]">
                        <Card class="self-start">
                            <CardHeader class="pb-4">
                                <CardTitle class="text-base">Rooms</CardTitle>
                                <CardDescription class="text-xs">
                                    Notification groups for alerts
                                </CardDescription>
                            </CardHeader>
                            <CardContent class="space-y-3">
                                <Dialog v-model:open="showCreateRoomDialog">
                                    <DialogTrigger asChild>
                                        <Button size="sm" class="w-full">
                                            <Plus class="mr-2 h-4 w-4" />
                                            Create Room
                                        </Button>
                                    </DialogTrigger>
                                    <DialogContent>
                                        <DialogHeader>
                                            <DialogTitle>Create Room</DialogTitle>
                                            <DialogDescription>
                                                Create a reusable group of recipients for alert notifications
                                            </DialogDescription>
                                        </DialogHeader>
                                        <div class="space-y-4 py-4">
                                            <div class="space-y-2">
                                                <Label for="room-name">Name</Label>
                                                <Input id="room-name" v-model="createRoomForm.name" placeholder="e.g., On-call Team, DevOps Alerts" />
                                            </div>
                                            <div class="space-y-2">
                                                <Label for="room-description">Description <span class="text-xs text-muted-foreground">(optional)</span></Label>
                                                <Textarea id="room-description" v-model="createRoomForm.description" rows="3" placeholder="Describe the purpose of this room" />
                                            </div>
                                        </div>
                                        <DialogFooter>
                                            <Button variant="outline" @click="showCreateRoomDialog = false">Cancel</Button>
                                            <Button @click="handleCreateRoom">
                                                <Plus class="mr-2 h-4 w-4" />
                                                Create Room
                                            </Button>
                                        </DialogFooter>
                                    </DialogContent>
                                </Dialog>

                                <div v-if="rooms.length" class="space-y-1">
                                    <Button
                                        v-for="room in rooms"
                                        :key="room.id"
                                        variant="ghost"
                                        size="sm"
                                        :class="[
                                            'w-full h-auto py-3 px-3 justify-start text-left transition-colors',
                                            selectedRoomId === room.id ? 'bg-muted hover:bg-muted/80' : 'hover:bg-muted/50'
                                        ]"
                                        @click="handleSelectRoom(room.id)"
                                    >
                                        <div class="flex flex-col items-start gap-1 min-w-0 w-full">
                                            <span class="text-sm font-medium truncate w-full">{{ room.name }}</span>
                                            <span class="text-xs text-muted-foreground">
                                                {{ room.member_count }} {{ room.member_count === 1 ? 'member' : 'members' }}
                                            </span>
                                            <span v-if="room.channel_types?.length" class="text-xs text-muted-foreground font-mono">
                                                {{ room.channel_types.join(', ') }}
                                            </span>
                                        </div>
                                    </Button>
                                </div>

                                <div v-else class="rounded-lg border border-dashed p-6 text-center">
                                    <p class="text-sm text-muted-foreground">
                                        No rooms yet. Create one to get started.
                                    </p>
                                </div>
                            </CardContent>
                        </Card>

                        <div v-if="selectedRoom" class="space-y-6">
                            <Card>
                                <CardHeader class="pb-4">
                                    <div class="space-y-4">
                                        <div>
                                            <CardTitle class="text-xl">{{ selectedRoom.name }}</CardTitle>
                                            <CardDescription class="mt-1.5">
                                                {{ selectedRoom.description || 'Room members receive email alerts when notifications are triggered.' }}
                                            </CardDescription>
                                        </div>
                                        <div class="flex flex-wrap items-center gap-2">
                                            <Dialog v-model:open="showAddRoomMemberDialog">
                                                <DialogTrigger asChild>
                                                    <Button size="sm">
                                                        <UserPlus class="mr-2 h-4 w-4" />
                                                        Add Member
                                                    </Button>
                                                </DialogTrigger>
                                                <DialogContent>
                                                    <DialogHeader>
                                                        <DialogTitle>Add Room Member</DialogTitle>
                                                        <DialogDescription>Select a team member to add to this room for alert notifications</DialogDescription>
                                                    </DialogHeader>
                                                    <div class="space-y-4 py-4">
                                                        <div class="space-y-2">
                                                            <Label>Team Member</Label>
                                                            <Select v-model="selectedRoomUserId">
                                                                <SelectTrigger>
                                                                    <SelectValue placeholder="Select team member" />
                                                                </SelectTrigger>
                                                                <SelectContent>
                                                                    <SelectItem v-for="member in availableRoomUsers" :key="member.user_id" :value="String(member.user_id)">
                                                                        {{ member.email }}
                                                                    </SelectItem>
                                                                </SelectContent>
                                                            </Select>
                                                        </div>
                                                    </div>
                                                    <DialogFooter>
                                                        <Button variant="outline" @click="showAddRoomMemberDialog = false">Cancel</Button>
                                                        <Button @click="handleAddRoomMember" :disabled="!selectedRoomUserId">
                                                            <Plus class="mr-2 h-4 w-4" />
                                                            Add Member
                                                        </Button>
                                                    </DialogFooter>
                                                </DialogContent>
                                            </Dialog>
                                            <Button size="sm" variant="outline" :disabled="addEntireTeamLoading" @click="handleAddEntireTeamToRoom">
                                                <Loader2 v-if="addEntireTeamLoading" class="mr-2 h-4 w-4 animate-spin" />
                                                <UserPlus v-else class="mr-2 h-4 w-4" />
                                                Add entire team
                                            </Button>
                                        </div>
                                    </div>
                                </CardHeader>
                                <CardContent class="pt-6">
                                    <div v-if="roomMembers.length === 0" class="rounded-lg border border-dashed p-8 text-center">
                                        <UserPlus class="h-8 w-8 mx-auto mb-3 text-muted-foreground/50" />
                                        <p class="text-sm font-medium mb-1">No members yet</p>
                                        <p class="text-xs text-muted-foreground">
                                            Add team members to this room to send them alert notifications
                                        </p>
                                    </div>
                                    <Table v-else>
                                        <TableHeader>
                                            <TableRow>
                                                <TableHead>Member</TableHead>
                                                <TableHead>Role</TableHead>
                                                <TableHead>Added</TableHead>
                                                <TableHead class="text-right">Actions</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            <TableRow v-for="member in roomMembers" :key="member.user_id">
                                                <TableCell>
                                                    <div class="flex flex-col gap-0.5">
                                                        <span class="font-medium">{{ member.email }}</span>
                                                        <span v-if="member.name" class="text-xs text-muted-foreground">{{ member.name }}</span>
                                                    </div>
                                                </TableCell>
                                                <TableCell>
                                                    <span class="inline-flex items-center rounded-md bg-muted px-2 py-1 text-xs font-medium capitalize">
                                                        {{ member.role }}
                                                    </span>
                                                </TableCell>
                                                <TableCell class="text-sm text-muted-foreground">{{ formatDate(member.added_at) }}</TableCell>
                                                <TableCell class="text-right">
                                                    <Button variant="ghost" size="icon" class="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10" @click="handleRemoveRoomMember(member.user_id)">
                                                        <Trash2 class="h-4 w-4" />
                                                    </Button>
                                                </TableCell>
                                            </TableRow>
                                        </TableBody>
                                    </Table>
                                </CardContent>
                            </Card>

                            <Card>
                                <CardHeader class="pb-4">
                                    <CardTitle class="text-base">Channels</CardTitle>
                                    <CardDescription>
                                        Configure Slack webhooks or HTTP endpoints to deliver real-time alert notifications
                                    </CardDescription>
                                </CardHeader>
                                <CardContent class="space-y-6">
                                    <div class="rounded-lg border bg-muted/30 p-4 space-y-4">
                                        <h4 class="text-sm font-medium">Add New Channel</h4>
                                        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-[140px,1fr,2fr,auto]">
                                            <div class="space-y-2">
                                                <Label for="channel-type" class="text-xs">Type</Label>
                                                <Select v-model="channelType" id="channel-type">
                                                    <SelectTrigger class="h-9">
                                                        <SelectValue />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value="slack">Slack</SelectItem>
                                                        <SelectItem value="webhook">Webhook</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                            <div class="space-y-2">
                                                <Label for="channel-name" class="text-xs">Name <span class="text-muted-foreground">(optional)</span></Label>
                                                <Input id="channel-name" v-model="channelForm.name" placeholder="e.g., #alerts" class="h-9" />
                                            </div>
                                            <div class="space-y-2">
                                                <Label for="channel-url" class="text-xs">Webhook URL</Label>
                                                <Input id="channel-url" v-model="channelForm.url" placeholder="https://hooks.slack.com/..." class="h-9" />
                                            </div>
                                            <div class="flex items-end">
                                                <Button size="sm" @click="handleCreateChannel" :disabled="!channelForm.url.trim()">
                                                    <Plus class="mr-2 h-3.5 w-3.5" />
                                                    Add Channel
                                                </Button>
                                            </div>
                                        </div>
                                    </div>

                                    <div v-if="roomChannels.length === 0" class="rounded-lg border border-dashed p-8 text-center">
                                        <Database class="h-8 w-8 mx-auto mb-3 text-muted-foreground/50" />
                                        <p class="text-sm font-medium mb-1">No channels configured</p>
                                        <p class="text-xs text-muted-foreground">
                                            Add Slack or webhook channels to deliver alerts beyond email notifications
                                        </p>
                                    </div>

                                    <Table v-else>
                                        <TableHeader>
                                            <TableRow>
                                                <TableHead>Channel</TableHead>
                                                <TableHead>Type</TableHead>
                                                <TableHead>Webhook URL</TableHead>
                                                <TableHead class="text-right">Actions</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            <TableRow v-for="channel in roomChannels" :key="channel.id">
                                                <TableCell>
                                                    <span class="font-medium">{{ channel.name || 'Unnamed Channel' }}</span>
                                                </TableCell>
                                                <TableCell>
                                                    <span class="inline-flex items-center rounded-md bg-muted px-2 py-1 text-xs font-medium capitalize">
                                                        {{ channel.type }}
                                                    </span>
                                                </TableCell>
                                                <TableCell class="max-w-[300px]">
                                                    <code class="text-xs text-muted-foreground truncate block">
                                                        {{ channel.config?.url }}
                                                    </code>
                                                </TableCell>
                                                <TableCell class="text-right">
                                                    <Button variant="ghost" size="icon" class="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10" @click="handleDeleteChannel(channel.id)">
                                                        <Trash2 class="h-4 w-4" />
                                                    </Button>
                                                </TableCell>
                                            </TableRow>
                                        </TableBody>
                                    </Table>
                                </CardContent>
                            </Card>
                        </div>
                        <div v-else class="flex items-center justify-center rounded-lg border border-dashed p-12 min-h-[400px]">
                            <div class="text-center space-y-3">
                                <div class="h-12 w-12 rounded-full bg-muted/50 mx-auto flex items-center justify-center mb-2">
                                    <Database class="h-6 w-6 text-muted-foreground/50" />
                                </div>
                                <p class="text-sm font-medium text-muted-foreground">Select a room to get started</p>
                                <p class="text-xs text-muted-foreground max-w-[280px]">
                                    Choose a room from the sidebar to manage its members and notification channels
                                </p>
                            </div>
                        </div>
                    </div>
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
