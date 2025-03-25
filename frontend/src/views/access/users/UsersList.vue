<script setup lang="ts">
import { onMounted, ref, h, computed, watch } from 'vue'
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
import { useRouter } from 'vue-router'
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
    useVueTable,
    getCoreRowModel,
    getFilteredRowModel,
    type ColumnDef,
    type ColumnFiltersState,
} from '@tanstack/vue-table'
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

const router = useRouter()
const usersStore = useUsersStore()
const { isLoading, error: userError } = storeToRefs(usersStore)
const showDeleteDialog = ref(false)
const userToDelete = ref<User | null>(null)
const columnFilters = ref<ColumnFiltersState>([])
const showEditDialog = ref(false)
const userToEdit = ref<User | null>(null)
const editForm = ref({
    full_name: '',
    email: '',
    role: '' as 'admin' | 'member'
})

const columns: ColumnDef<User>[] = [
    {
        accessorKey: 'full_name',
        header: 'Name',
        cell: (props) => props.getValue() as string,
    },
    {
        accessorKey: 'email',
        header: 'Email',
        cell: (props) => props.getValue() as string,
    },
    {
        accessorKey: 'role',
        header: 'Role',
        cell: (props) => {
            const role = props.getValue() as string
            return h(Badge, {
                variant: role === 'admin' ? 'destructive' : 'outline',
                class: 'capitalize inline-flex items-center gap-1 font-medium'
            }, () => [
                role === 'admin' ? h(Shield, { class: 'h-3 w-3' }) : h(User2, { class: 'h-3 w-3' }),
                role
            ])
        },
    },
    {
        accessorKey: 'status',
        header: 'Status',
        cell: (props) => {
            const user = props.row.original
            return h('div', { class: 'flex items-center space-x-2' }, [
                h(Switch, {
                    checked: user.status === 'active',
                    'onUpdate:checked': () => toggleUserStatus(user),
                }),
                h('span', { class: 'capitalize' }, user.status),
            ])
        },
    },
    {
        accessorKey: 'created_at',
        header: 'Created At',
        cell: (props) => formatDate(props.getValue() as string | undefined),
    },
    {
        accessorKey: 'last_login_at',
        header: 'Last Login',
        cell: (props) => formatDate(props.getValue() as string | undefined),
    },
    {
        id: 'actions',
        cell: (props) => {
            const user = props.row.original
            return h('div', { class: 'flex items-center gap-2 justify-end' }, [
                h(Button, {
                    variant: 'outline',
                    size: 'icon',
                    onClick: () => handleEdit(user),
                }, () => h(Pencil, { class: 'h-4 w-4' })),
                h(Button, {
                    variant: 'destructive',
                    size: 'icon',
                    onClick: () => handleDelete(user),
                }, () => h(Trash2, { class: 'h-4 w-4' }))
            ])
        },
    },
]

const searchQuery = ref('')

const filteredUsers = computed(() => {
    // Debug the users being accessed
    console.log("Computing filteredUsers. Current users:", usersStore.users.value);
    
    // Always ensure we have an array to work with
    const users = usersStore.users.value || [];
    
    if (!searchQuery.value) return users;

    const query = searchQuery.value.toLowerCase();
    return users.filter(user =>
        user?.full_name?.toLowerCase().includes(query) ||
        user?.email?.toLowerCase().includes(query)
    );
})

const table = useVueTable({
    data: filteredUsers,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    state: {
        get columnFilters() { return columnFilters.value },
    },
})

const loadUsers = async () => {
    console.log("Starting loadUsers...");
    await usersStore.loadUsers();
    console.log("loadUsers completed");
}

const confirmDelete = async () => {
    if (!userToDelete.value) return

    const result = await usersStore.deleteUser(userToDelete.value.id);
    if (result.success) {
        await loadUsers();
        showDeleteDialog.value = false;
        userToDelete.value = null;
    }
    // Keep dialog open on error
}

const handleDelete = (user: User) => {
    userToDelete.value = user
    showDeleteDialog.value = true
}

const toggleUserStatus = async (user: User) => {
    const result = await usersStore.updateUser(user.id, {
        status: user.status === 'active' ? 'inactive' : 'active',
    });
    
    if (result.success) {
        await loadUsers();
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
        await loadUsers();
        showEditDialog.value = false;
        userToEdit.value = null;
    }
}

onMounted(async () => {
    console.log("Component mounted, loading users...");
    const result = await loadUsers();
    console.log("User loading result:", result);
    console.log("Users after loading:", usersStore.users.value);
});

// Add this watch to debug users data
watch(() => usersStore.users.value, (newUsers) => {
    console.log("Users updated:", newUsers);
}, { immediate: true });
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
                <div v-else-if="!filteredUsers.length" class="rounded-lg border p-4 text-center">
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
                                    <TableHead v-for="column in table.getAllColumns()" :key="column.id">
                                        {{ column.id === 'actions' ? '' : column.columnDef.header }}
                                    </TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                <TableRow v-for="row in table.getRowModel().rows" :key="row.id">
                                    <TableCell v-for="cell in row.getVisibleCells()" :key="cell.id">
                                        <component :is="cell.column.columnDef.cell" v-bind="cell.getContext()" />
                                    </TableCell>
                                </TableRow>
                                <TableRow v-if="table.getRowModel().rows.length === 0">
                                    <TableCell :colspan="columns.length" class="h-24 text-center">
                                        No results found.
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
