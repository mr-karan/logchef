<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
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
import { Plus, Trash2, Shield, User2, Search, Pencil } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import type { User } from '@/types'
import { useUsersStore } from '@/stores/users'
import { formatDate } from '@/utils/format'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import AddUser from './AddUser.vue'

const usersStore = useUsersStore()
const { isLoading, error: userError } = storeToRefs(usersStore)
const showDeleteDialog = ref(false)
const userToDelete = ref<User | null>(null)
const showEditDialog = ref(false)
const userToEdit = ref<User | null>(null)
const editForm = ref({
    full_name: '',
    email: '',
    role: '' as 'admin' | 'member'
})

const searchQuery = ref('')

// Simple computed property to filter users
const filteredUsers = computed(() => {
    const users = usersStore.getUsersArray() || [];
    
    if (!searchQuery.value) return users;

    const query = searchQuery.value.toLowerCase();
    return users.filter(user =>
        user?.full_name?.toLowerCase().includes(query) ||
        user?.email?.toLowerCase().includes(query)
    );
})

const loadUsers = async (forceReload = false) => {
    return await usersStore.loadUsers(forceReload);
}

const confirmDelete = async () => {
    if (!userToDelete.value) return

    const result = await usersStore.deleteUser(userToDelete.value.id);
    if (result.success) {
        // Fetch fresh data from the API
        await loadUsers(true);
        showDeleteDialog.value = false;
        userToDelete.value = null;
        toast({
            title: 'Success',
            description: 'User deleted successfully.',
            variant: 'default',
        });
    } else {
        // Show error toast
        toast({
            title: 'Error',
            description: result.error?.message || 'Failed to delete user.',
            variant: 'destructive',
        });
    }
}

const handleDelete = (user: User) => {
    userToDelete.value = user
    showDeleteDialog.value = true
}

import { useToast } from '@/components/ui/toast/use-toast'

// Add toast at the top with other imports
const { toast } = useToast()

const toggleUserStatus = async (user: User) => {
    const result = await usersStore.updateUser(user.id, {
        status: user.status === 'active' ? 'inactive' : 'active',
    });
    
    if (result.success) {
        // Refresh the users list to ensure we have the latest data
        await loadUsers(true);
        toast({
            title: 'Success',
            description: `User ${user.full_name}'s status updated successfully.`,
            variant: 'default',
        });
    } else {
        // Show error toast
        toast({
            title: 'Error',
            description: result.error?.message || 'Failed to update user status.',
            variant: 'destructive',
        });
    }
}

const handleEdit = (user: User) => {
    userToEdit.value = user
    editForm.value = {
        full_name: user.full_name,
        email: user.email,
        role: user.role
    }
    showEditDialog.value = true
}

const confirmEdit = async () => {
    if (!userToEdit.value) return

    const result = await usersStore.updateUser(userToEdit.value.id.toString(), {
        full_name: editForm.value.full_name,
        email: editForm.value.email,
        role: editForm.value.role
    });
    
    if (result.success) {
        // Refresh the users list to ensure we have the latest data
        await loadUsers(true);
        showEditDialog.value = false;
        userToEdit.value = null;
        toast({
            title: 'Success',
            description: 'User updated successfully.',
            variant: 'default',
        });
    } else {
        // Show error toast
        toast({
            title: 'Error',
            description: result.error?.message || 'Failed to update user.',
            variant: 'destructive',
        });
    }
}

onMounted(() => {
    loadUsers();
});
</script>

<template>
    <div class="space-y-6">
        <Card>
            <CardHeader>
                <div class="flex items-center justify-between">
                    <div>
                        <CardTitle>Manage Users</CardTitle>
                        <CardDescription>
                            View and manage your users
                        </CardDescription>
                    </div>
                    <AddUser />
                </div>
            </CardHeader>
            <CardContent>
                <div v-if="isLoading" class="text-center py-4">
                    Loading users...
                </div>
                <div v-else-if="filteredUsers.length === 0" class="rounded-lg border p-4 text-center">
                    <p class="text-muted-foreground mb-4">No users found</p>
                    <AddUser>
                        <Button>
                            <Plus class="mr-2 h-4 w-4" />
                            Add Your First User
                        </Button>
                    </AddUser>
                </div>
                <div v-else class="space-y-4">
                    <div class="flex items-center">
                        <div class="relative w-full max-w-sm">
                            <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                            <Input placeholder="Search users by name or email..." class="pl-8" v-model="searchQuery" />
                        </div>
                    </div>
                    <div class="rounded-md border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Name</TableHead>
                                    <TableHead>Email</TableHead>
                                    <TableHead>Role</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead>Created At</TableHead>
                                    <TableHead>Last Login</TableHead>
                                    <TableHead></TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                <TableRow v-for="user in filteredUsers" :key="user.id">
                                    <TableCell>{{ user.full_name }}</TableCell>
                                    <TableCell>{{ user.email }}</TableCell>
                                    <TableCell>
                                        <Badge :variant="user.role === 'admin' ? 'destructive' : 'outline'"
                                            class="capitalize inline-flex items-center gap-1 font-medium">
                                            <Shield v-if="user.role === 'admin'" class="h-3 w-3" />
                                            <User2 v-else class="h-3 w-3" />
                                            {{ user.role }}
                                        </Badge>
                                    </TableCell>
                                    <TableCell>
                                        <div class="flex items-center space-x-2">
                                            <Switch :checked="user.status === 'active'" 
                                                @update:checked="toggleUserStatus(user)" />
                                            <span class="capitalize">{{ user.status }}</span>
                                        </div>
                                    </TableCell>
                                    <TableCell>{{ formatDate(user.created_at) }}</TableCell>
                                    <TableCell>{{ formatDate(user.last_login_at) }}</TableCell>
                                    <TableCell>
                                        <div class="flex items-center gap-2 justify-end">
                                            <Button variant="outline" size="icon" @click="handleEdit(user)">
                                                <Pencil class="h-4 w-4" />
                                            </Button>
                                            <Button variant="destructive" size="icon" @click="handleDelete(user)">
                                                <Trash2 class="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            </TableBody>
                        </Table>
                    </div>
                </div>
            </CardContent>
        </Card>

        <!-- Delete Confirmation Dialog -->
        <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = false">
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>Delete User</AlertDialogTitle>
                    <AlertDialogDescription class="space-y-2">
                        <p>Are you sure you want to delete user "{{ userToDelete?.full_name }}"?</p>
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

        <!-- Edit Dialog -->
        <Dialog :open="showEditDialog" @update:open="showEditDialog = false">
            <DialogContent class="sm:max-w-[425px]">
                <DialogHeader>
                    <DialogTitle>Edit User</DialogTitle>
                    <DialogDescription>
                        Make changes to the user's profile here.
                    </DialogDescription>
                </DialogHeader>
                <div class="grid gap-4 py-4">
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="name" class="text-right">Name</Label>
                        <Input id="name" v-model="editForm.full_name" class="col-span-3" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="email" class="text-right">Email</Label>
                        <Input id="email" type="email" v-model="editForm.email" class="col-span-3" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="role" class="text-right">Role</Label>
                        <Select v-model="editForm.role" class="col-span-3">
                            <SelectTrigger>
                                <SelectValue placeholder="Select a role" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="admin">Admin</SelectItem>
                                <SelectItem value="member">Member</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                </div>
                <DialogFooter>
                    <Button variant="outline" @click="showEditDialog = false">
                        Cancel
                    </Button>
                    <Button @click="confirmEdit">
                        Save changes
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    </div>
</template>
