<template>
    <Dialog v-model:open="showDialog">
        <DialogTrigger as-child>
            <slot>
                <Button>
                    <Plus class="mr-2 h-4 w-4" />
                    Add User
                </Button>
            </slot>
        </DialogTrigger>
        <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
                <DialogTitle>Add New User</DialogTitle>
                <DialogDescription>
                    Create a new user account by providing their details.
                </DialogDescription>
            </DialogHeader>
            <form @submit.prevent="handleSubmit">
                <div class="grid gap-4 py-4">
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="full_name" class="text-right">Full Name</Label>
                        <Input id="full_name" v-model="formData.full_name" placeholder="Enter user's full name"
                            class="col-span-3" required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="email" class="text-right">Email</Label>
                        <Input id="email" v-model="formData.email" type="email" placeholder="Enter user's email"
                            class="col-span-3" required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="role" class="text-right">Role</Label>
                        <div class="col-span-3">
                            <Select v-model="formData.role">
                                <SelectTrigger class="w-full">
                                    <SelectValue placeholder="Select role" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="admin">Admin</SelectItem>
                                    <SelectItem value="member">Member</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="status" class="text-right">Status</Label>
                        <div class="col-span-3">
                            <Select v-model="formData.status">
                                <SelectTrigger class="w-full">
                                    <SelectValue placeholder="Select status" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="active">Active</SelectItem>
                                    <SelectItem value="inactive">Inactive</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                </div>
                <DialogFooter>
                    <Button type="submit" :disabled="isLoading">
                        {{ isLoading ? 'Creating...' : 'Create User' }}
                    </Button>
                </DialogFooter>
            </form>
        </DialogContent>
    </Dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Plus } from 'lucide-vue-next'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import type { CreateUserRequest } from '@/api/users'
import { useUsersStore } from '@/stores/users'


const usersStore = useUsersStore()
const showDialog = ref(false)

interface FormData extends CreateUserRequest {
    status: 'active' | 'inactive'
}

const formData = ref<FormData>({
    email: '',
    full_name: '',
    role: 'member',
    status: 'active',
})

const { isLoading, error: formError } = storeToRefs(usersStore)

async function handleSubmit() {
    const result = await usersStore.createUser({
        email: formData.value.email,
        full_name: formData.value.full_name,
        role: formData.value.role,
    });
    
    if (result.success) {
        // Reset form
        formData.value = {
            email: '',
            full_name: '',
            role: 'member',
            status: 'active',
        }
        showDialog.value = false
    }
}
</script>
