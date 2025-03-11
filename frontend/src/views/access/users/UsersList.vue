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
import { Plus, Trash2, Shield, User2 } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
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

const router = useRouter()
const usersStore = useUsersStore()
const isLoading = ref(true)
const showDeleteDialog = ref(false)
const userToDelete = ref<User | null>(null)

const loadUsers = async () => {
    isLoading.value = true
    await usersStore.loadUsers(true)
    isLoading.value = false
}

const confirmDelete = async () => {
    if (!userToDelete.value) return

    const result = await usersStore.deleteUser(userToDelete.value.id)

    if (result.success) {
        // Reload users to get updated list
        await loadUsers()
    }

    showDeleteDialog.value = false
    userToDelete.value = null
}

const handleDelete = (user: User) => {
    userToDelete.value = user
    showDeleteDialog.value = true
}

const toggleUserStatus = async (user: User) => {
    const result = await usersStore.updateUser(user.id, {
        status: user.status === 'active' ? 'inactive' : 'active',
    })

    if (result.success) {
        // Reload users to get updated list
        await loadUsers()
    }
}

onMounted(loadUsers)
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
                    <Button @click="router.push({ name: 'NewUser' })">
                        <Plus class="mr-2 h-4 w-4" />
                        Add User
                    </Button>
                </div>
            </CardHeader>
            <CardContent>
                <div v-if="isLoading" class="text-center py-4">
                    Loading users...
                </div>
                <div v-else-if="usersStore.users.length === 0" class="rounded-lg border p-4 text-center">
                    <p class="text-muted-foreground mb-4">No users found</p>
                    <Button @click="router.push({ name: 'NewUser' })">
                        <Plus class="mr-2 h-4 w-4" />
                        Add Your First User
                    </Button>
                </div>
                <div v-else class="space-y-4">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead class="w-[200px]">Name</TableHead>
                                <TableHead class="w-[200px]">Email</TableHead>
                                <TableHead class="w-[100px]">Role</TableHead>
                                <TableHead class="w-[100px]">Status</TableHead>
                                <TableHead class="w-[150px]">Created At</TableHead>
                                <TableHead class="w-[150px]">Last Login</TableHead>
                                <TableHead class="w-[100px] text-right">Actions</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            <TableRow v-for="user in usersStore.users" :key="user.id">
                                <TableCell class="font-medium">{{ user.full_name }}</TableCell>
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
                                <TableCell class="text-right">
                                    <Button variant="destructive" size="icon" @click="handleDelete(user)">
                                        <Trash2 class="h-4 w-4" />
                                    </Button>
                                </TableCell>
                            </TableRow>
                        </TableBody>
                    </Table>
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
    </div>
</template>