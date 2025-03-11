<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardHeader, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import type { CreateUserRequest } from '@/api/users'
import { useUsersStore } from '@/stores/users'

interface FormData extends CreateUserRequest {
    status: 'active' | 'inactive'
}

const router = useRouter()
const usersStore = useUsersStore()

const formData = ref<FormData>({
    email: '',
    full_name: '',
    role: 'member',
    status: 'active',
})

const isLoading = ref(false)

async function handleSubmit() {
    if (isLoading.value) return

    isLoading.value = true

    const result = await usersStore.createUser({
        email: formData.value.email,
        full_name: formData.value.full_name,
        role: formData.value.role,
    })

    isLoading.value = false

    if (result.success) {
        router.push({ name: 'ManageUsers' })
    }
}
</script>

<template>
    <Card>
        <CardHeader>
            <div class="flex items-center justify-between">
                <div>
                    <h2 class="text-lg font-semibold">Add New User</h2>
                    <p class="text-sm text-muted-foreground">Create a new user account</p>
                </div>
                <Button variant="outline" @click="router.back()">Cancel</Button>
            </div>
        </CardHeader>
        <form @submit.prevent="handleSubmit">
            <CardContent class="space-y-6">
                <div class="space-y-2">
                    <Label for="full_name">Full Name</Label>
                    <Input id="full_name" v-model="formData.full_name" type="text" placeholder="Enter user's full name"
                        required />
                </div>

                <div class="space-y-2">
                    <Label for="email">Email</Label>
                    <Input id="email" v-model="formData.email" type="email" placeholder="Enter user's email" required />
                </div>

                <div class="space-y-2">
                    <Label for="role">Role</Label>
                    <Select v-model="formData.role">
                        <SelectTrigger>
                            <SelectValue placeholder="Select role" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="admin">Admin</SelectItem>
                            <SelectItem value="member">Member</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                <div class="space-y-2">
                    <Label for="status">Status</Label>
                    <Select v-model="formData.status">
                        <SelectTrigger>
                            <SelectValue placeholder="Select status" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="active">Active</SelectItem>
                            <SelectItem value="inactive">Inactive</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </CardContent>

            <CardFooter>
                <Button type="submit" :disabled="isLoading">
                    {{ isLoading ? 'Creating...' : 'Create User' }}
                </Button>
            </CardFooter>
        </form>
    </Card>
</template>
