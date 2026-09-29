<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/composables/useToast'
import { type Source } from '@/api/sources'
import { Loader2, Plus, Trash2, UserPlus, Database, Bot, User, Search, ArrowUpDown, ArrowUp, ArrowDown } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
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
import { SearchableSelect } from '@/components/ui/searchable-select'
import { useTableSearchSort } from '@/composables/useTableSearchSort'
import { useUsersStore } from "@/stores/users"
import { useSourcesStore } from "@/stores/sources"
import { useTeamsStore } from "@/stores/teams"
import { useAuthStore } from "@/stores/auth"
import { formatDate, formatSourceName } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const { toast } = useToast()
const { t } = useI18n()

// Initialize stores with proper Pinia pattern
const usersStore = useUsersStore()
const sourcesStore = useSourcesStore()
const teamsStore = useTeamsStore()
const authStore = useAuthStore()

// Get the teamId from route params
const teamId = computed(() => Number(route.params.id))

function translateRole(role: string) {
    switch (role) {
        case 'admin': return t('access.roleAdmin')
        case 'editor': return t('access.roleEditor')
        case 'member': return t('access.roleMember')
        default: return role
    }
}

// Check if user is a global admin
const isGlobalAdmin = computed(() => authStore.user?.role === 'admin')

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
const newMemberType = ref<'human' | 'service'>('human')
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

// Compute available users (users not in team) sorted alphabetically by full_name/email
const availableUsers = computed(() => {
    const teamMemberIds = members.value?.map(m => String(m.user_id)) || []
    const users = usersStore.getUsersNotInTeam(teamMemberIds)
    const wantedType = newMemberType.value
    return users
        .filter(user => (user.account_type ?? 'human') === wantedType)
        .sort((a, b) => (a.full_name || a.email).localeCompare(b.full_name || b.email))
})

// Compute available sources (sources not in team) sorted alphabetically by name
const availableSources = computed(() => {
    const teamSourceIds = teamSources.value?.map((s: Source) => s.id) || []
    const sources = sourcesStore.getSourcesNotInTeam(teamSourceIds)
    return sources.sort((a, b) => a.name.localeCompare(b.name))
})

// Items for the searchable pickers (add-member and add-source dialogs).
const userItems = computed(() =>
    availableUsers.value.map(u => ({
        value: String(u.id),
        label: u.full_name || u.email,
        sublabel: (u.full_name && newMemberType.value === 'human') ? u.email : undefined,
    })))
const sourceItems = computed(() =>
    availableSources.value.map(s => ({ value: String(s.id), label: formatSourceName(s) })))

// Search + sort for the members table.
const {
    search: memberSearch,
    rows: sortedMembers,
    sortKey: memberSortKey,
    sortDir: memberSortDir,
    toggleSort: toggleMemberSort,
} = useTableSearchSort(members, {
    searchKeys: ['email', (m) => m.full_name, (m) => m.role],
    sortAccessors: {
        name: (m) => (m.full_name || m.email || '').toLowerCase(),
        role: (m) => m.role || '',
        added: (m) => new Date(m.created_at),
    },
    initialSort: { key: 'added', dir: 'desc' },
})

// Load users when dialog opens to prevent unnecessary API calls
watch(showAddMemberDialog, async (isOpen) => {
    if (isOpen) {
        newMemberType.value = 'human'
        selectedUserId.value = ''
        if (!usersStore.users.length) {
            await usersStore.loadUsers()
        }
    }
})

// Reset selection when switching the account type filter
watch(newMemberType, () => {
    selectedUserId.value = ''
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
            title: t('ui.error'),
            description: t('access.teamNameRequired'),
            variant: 'destructive',
        })
        return
    }

    await teamsStore.updateTeam(team.value.id, {
        name: name.value,
        description: description.value || '',
    })

}


const handleAddMember = async () => {
    if (!team.value || !selectedUserId.value || selectedUserId.value === '__none__') return

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
                title: t('ui.error'),
                description: result.error?.message || t('access.removeTeamMemberFailed'),
                variant: 'destructive',
            });
        }
        // If result.success is true, the store handles the success toast.
    } catch (error) {
        // This catch is for unexpected errors during the teamsStore.removeTeamMember call itself
        console.error('Error removing team member:', error);
        toast({
            title: t('ui.error'),
            description: t('access.removeTeamMemberUnexpected'),
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
            title: t('ui.error'),
            description: t('access.invalidTeamID', { id: route.params.id }),
            variant: 'destructive',
        })
        isLoading.value = false
        return
    }

    try {
        isLoading.value = true

        // Load basic data in parallel for better performance
        // Global admins load all teams, team admins load their user teams
        const teamsLoadPromise = isGlobalAdmin.value
            ? teamsStore.loadAdminTeams()
            : teamsStore.loadUserTeams()

        await Promise.all([
            teamsLoadPromise,
            // Load users and sources in parallel for efficiency
            usersStore.loadUsers(),
            sourcesStore.loadSources()
        ])

        // Get detailed team info after confirming teams are loaded
        await teamsStore.getTeam(id)

        // Load team-specific data in parallel
        await Promise.all([
            teamsStore.listTeamMembers(id),
            teamsStore.listTeamSources(id)
        ])

    } catch (error) {
        console.error("Error loading team settings:", error)
        toast({
            title: t('ui.error'),
            description: t('access.loadTeamFailed'),
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
                <p class="text-muted-foreground">{{ t('access.loadingTeamSettings') }}</p>
            </div>
        </div>
        <div v-else-if="!team" class="text-center py-12">
            <h3 class="text-lg font-medium mb-2">{{ t('access.teamNotFound') }}</h3>
            <p class="text-muted-foreground mb-4">{{ t('access.teamNotFoundDescription') }}
            </p>
            <Button variant="outline" @click="router.push('/access/teams')">
                {{ t('access.backToTeams') }}
            </Button>
        </div>
        <template v-else>
            <!-- Header -->
            <div>
                <h1 class="text-2xl font-bold tracking-tight">{{ team.name }}</h1>
                <p class="text-muted-foreground mt-2">
                    {{ t('access.manageTeamDescription') }}
                </p>
            </div>

            <!-- Tabs -->
            <Tabs v-model="activeTab" class="space-y-6">
                <TabsList>
                    <TabsTrigger value="members">{{ t('access.members') }}</TabsTrigger>
                    <TabsTrigger value="sources">{{ t('ui.sources') }}</TabsTrigger>
                    <TabsTrigger value="settings">{{ t('access.settings') }}</TabsTrigger>
                </TabsList>

                <!-- Members Tab -->
                <TabsContent value="members">
                    <Card>
                        <CardHeader>
                            <div class="flex items-center justify-between">
                                <div>
                                    <CardTitle>{{ t('access.teamMembers') }}</CardTitle>
                                    <CardDescription>
                                        {{ t('access.manageTeamMembersDescription') }}
                                    </CardDescription>
                                </div>
                                <Dialog v-model:open="showAddMemberDialog">
                                    <DialogTrigger asChild>
                                        <Button>
                                            <UserPlus class="mr-2 h-4 w-4" />
                                            {{ t('access.addMember') }}
                                        </Button>
                                    </DialogTrigger>
                                    <DialogContent>
                                        <DialogHeader>
                                            <DialogTitle>{{ t('access.addTeamMember') }}</DialogTitle>
                                            <DialogDescription>
                                                {{ t('access.addTeamMemberDescription') }}
                                            </DialogDescription>
                                        </DialogHeader>
                                        <div class="space-y-4 py-4">
                                            <div class="space-y-2">
                                                <Label>{{ t('access.accountType') }}</Label>
                                                <div class="grid grid-cols-2 gap-2">
                                                    <Button type="button" size="sm"
                                                        :variant="newMemberType === 'human' ? 'default' : 'outline'"
                                                        class="gap-2 justify-start"
                                                        @click="newMemberType = 'human'">
                                                        <User class="h-4 w-4" />
                                                        {{ t('access.humanUser') }}
                                                    </Button>
                                                    <Button type="button" size="sm"
                                                        :variant="newMemberType === 'service' ? 'default' : 'outline'"
                                                        class="gap-2 justify-start"
                                                        @click="newMemberType = 'service'">
                                                        <Bot class="h-4 w-4" />
                                                        {{ t('access.serviceAccount') }}
                                                    </Button>
                                                </div>
                                            </div>
                                            <div class="space-y-2">
                                                <Label>{{ newMemberType === 'service' ? t('access.serviceAccount') : t('access.user') }}</Label>
                                                <SearchableSelect
                                                    v-model="selectedUserId"
                                                    :items="userItems"
                                                    :placeholder="newMemberType === 'service' ? t('access.selectServiceAccount') : t('access.selectUser')"
                                                    :search-placeholder="newMemberType === 'service' ? t('access.searchServiceAccounts') : t('access.searchUsers')"
                                                    :empty-text="newMemberType === 'service' ? t('access.noServiceAccountsAvailable') : t('access.noUsersAvailable')" />
                                            </div>
                                            <div class="space-y-2">
                                                <Label>{{ t('ui.role') }}</Label>
                                                <Select v-model="newMemberRole">
                                                    <SelectTrigger>
                                                        <SelectValue :placeholder="t('access.selectRole')" />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value="member">{{ t('access.roleMember') }}</SelectItem>
                                                        <SelectItem value="editor">{{ t('access.roleEditor') }}</SelectItem>
                                                        <SelectItem value="admin">{{ t('access.roleAdmin') }}</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                        </div>
                                        <DialogFooter>
                                            <Button variant="outline" @click="showAddMemberDialog = false">
                                                {{ t('ui.cancel') }}
                                            </Button>
                                            <Button :disabled="isSaving" @click="handleAddMember">
                                                <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                                <Plus v-else class="mr-2 h-4 w-4" />
                                                {{ t('access.addMember') }}
                                            </Button>
                                        </DialogFooter>
                                    </DialogContent>
                                </Dialog>
                            </div>
                        </CardHeader>
                        <CardContent>
                            <div v-if="teamsStore.isLoadingTeamMembers(teamId)" class="text-center py-4">
                                <Loader2 class="h-6 w-6 animate-spin mx-auto mb-2" />
                                <p class="text-sm text-muted-foreground">{{ t('access.loadingMembers') }}</p>
                            </div>
                            <template v-else>
                                <div v-if="members.length > 0" class="relative mb-3 max-w-sm">
                                    <Search class="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                                    <Input v-model="memberSearch" :placeholder="t('access.searchMembers')" class="pl-8" />
                                </div>
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>
                                            <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleMemberSort('name')">
                                                {{ t('ui.email') }}
                                                <component :is="memberSortKey === 'name' ? (memberSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                            </button>
                                        </TableHead>
                                        <TableHead>
                                            <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleMemberSort('role')">
                                                {{ t('ui.role') }}
                                                <component :is="memberSortKey === 'role' ? (memberSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                            </button>
                                        </TableHead>
                                        <TableHead>
                                            <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleMemberSort('added')">
                                                {{ t('access.added') }}
                                                <component :is="memberSortKey === 'added' ? (memberSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                            </button>
                                        </TableHead>
                                        <TableHead class="text-right">{{ t('ui.actions') }}</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-if="sortedMembers.length === 0">
                                        <TableCell colspan="4" class="text-center py-4 text-muted-foreground">
                                            {{ memberSearch ? t('access.noMembersMatch') : t('access.noMembersFound') }}
                                        </TableCell>
                                    </TableRow>
                                    <TableRow v-for="member in sortedMembers" :key="member.user_id">
                                        <TableCell>
                                            <div class="flex flex-col gap-1">
                                                <div class="flex items-center gap-2">
                                                    <Bot v-if="member.account_type === 'service'" class="h-4 w-4 text-muted-foreground" />
                                                    <span class="font-medium">{{ member.full_name || member.email }}</span>
                                                    <Badge v-if="member.account_type === 'service'" variant="secondary" class="text-xs">
                                                         {{ t('access.serviceAccount') }}
                                                    </Badge>
                                                </div>
                                                <span v-if="member.account_type !== 'service' && member.full_name"
                                                    class="text-sm text-muted-foreground">{{ member.email }}</span>
                                            </div>
                                        </TableCell>
                                        <TableCell>{{ translateRole(member.role) }}</TableCell>
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
                            </template>
                        </CardContent>
                    </Card>
                </TabsContent>

                <!-- Sources Tab -->
                <TabsContent value="sources">
                    <Card>
                        <CardHeader>
                            <div class="flex items-center justify-between">
                                <div>
                                    <CardTitle>{{ t('access.teamSources') }}</CardTitle>
                                    <CardDescription>
                                        {{ t('access.manageTeamSourcesDescription') }}
                                    </CardDescription>
                                </div>
                                <Dialog v-model:open="showAddSourceDialog">
                                    <DialogTrigger asChild>
                                        <Button @click="activeTab = 'sources'">
                                            <Database class="mr-2 h-4 w-4" />
                                            {{ t('sources.addSource') }}
                                        </Button>
                                    </DialogTrigger>
                                    <DialogContent>
                                        <DialogHeader>
                                            <DialogTitle>{{ t('access.addDataSource') }}</DialogTitle>
                                            <DialogDescription>
                                                {{ t('access.addDataSourceDescription') }}
                                            </DialogDescription>
                                        </DialogHeader>
                                        <div class="space-y-4 py-4">
                                            <div class="space-y-2">
                                                <Label>{{ t('ui.source') }}</Label>
                                                <SearchableSelect
                                                    v-model="selectedSourceId"
                                                    :items="sourceItems"
                                                    :placeholder="t('sources.selectSource')"
                                                    :search-placeholder="t('access.searchSources')"
                                                    :empty-text="t('access.noSourcesAvailable')" />
                                            </div>
                                        </div>
                                        <DialogFooter>
                                            <Button variant="outline" @click="showAddSourceDialog = false">
                                                {{ t('ui.cancel') }}
                                            </Button>
                                            <Button :disabled="isSaving" @click="handleAddSource">
                                                <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                                <Plus v-else class="mr-2 h-4 w-4" />
                                                {{ t('sources.addSource') }}
                                            </Button>
                                        </DialogFooter>
                                    </DialogContent>
                                </Dialog>
                            </div>
                        </CardHeader>
                        <CardContent>
                            <div v-if="teamsStore.isLoadingTeamSources(teamId)" class="text-center py-4">
                                <Loader2 class="h-6 w-6 animate-spin mx-auto mb-2" />
                                <p class="text-sm text-muted-foreground">{{ t('access.loadingSources') }}</p>
                            </div>
                            <Table v-else>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>{{ t('ui.source') }}</TableHead>
                                        <TableHead>{{ t('ui.description') }}</TableHead>
                                        <TableHead>{{ t('access.added') }}</TableHead>
                                        <TableHead class="text-right">{{ t('ui.actions') }}</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    <TableRow v-if="teamSources.length === 0">
                                        <TableCell colspan="4" class="text-center py-4 text-muted-foreground">
                                            {{ t('access.noSourcesFound') }}
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
                                    <CardTitle>{{ t('access.settings') }}</CardTitle>
                            <CardDescription>
                                        {{ t('access.updateTeamDescription') }}
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <form @submit.prevent="handleSubmit" class="space-y-6">
                                <div class="space-y-4">
                                    <div class="grid gap-2">
                                        <Label for="name">{{ t('access.teamName') }}</Label>
                                        <Input id="name" v-model="name" required />
                                    </div>

                                    <div class="grid gap-2">
                                        <Label for="description">{{ t('ui.description') }}</Label>
                                        <Textarea id="description" v-model="description" :placeholder="t('access.teamDescription')"
                                            rows="3" />
                                    </div>
                                </div>

                                <div class="flex justify-end">
                                    <Button type="submit" :disabled="isSaving">
                                        <Loader2 v-if="isSaving" class="mr-2 h-4 w-4 animate-spin" />
                                        {{ t('ui.saveChanges2') }}
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
