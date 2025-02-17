<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { usersApi } from '@/api/users'
import { getErrorMessage } from '@/api/types'

const router = useRouter()
const { toast } = useToast()

const email = ref('')
const fullName = ref('')
const role = ref('member')
const isSubmitting = ref(false)

const handleSubmit = async () => {
    if (isSubmitting.value) return

    // Basic validation
    if (!email.value || !fullName.value) {
        toast({
            title: 'Error',
            description: 'Please fill in all required fields',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    isSubmitting.value = true

    try {
        const response = await usersApi.createUser({
            email: email.value,
            full_name: fullName.value,
            role: role.value as 'admin' | 'member',
        })

        if ('error' in response.data) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'User created successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Redirect to manage users
        router.push({ name: 'ManageUsers' })
    } catch (error) {
        console.error('Error creating user:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isSubmitting.value = false
    }
}
</script>

<template>
    <Card>
        <CardHeader>
            <CardTitle>Add User</CardTitle>
            <CardDescription>
                Create a new user account
            </CardDescription>
        </CardHeader>
        <CardContent>
            <form @submit.prevent="handleSubmit" class="space-y-6">
                <div class="space-y-4">
                    <div class="grid gap-2">
                        <Label for="email">Email</Label>
                        <Input id="email" v-model="email" type="email" placeholder="user@example.com" required />
                    </div>

                    <div class="grid gap-2">
                        <Label for="fullName">Full Name</Label>
                        <Input id="fullName" v-model="fullName" placeholder="John Doe" required />
                    </div>

                    <div class="grid gap-2">
                        <Label for="role">Role</Label>
                        <Select v-model="role">
                            <SelectTrigger id="role">
                                <SelectValue placeholder="Select a role" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="member">Member</SelectItem>
                                <SelectItem value="admin">Admin</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                </div>

                <div class="flex justify-end space-x-4">
                    <Button type="button" variant="outline" @click="router.push({ name: 'ManageUsers' })">
                        Cancel
                    </Button>
                    <Button type="submit" :disabled="isSubmitting">
                        {{ isSubmitting ? 'Creating...' : 'Create User' }}
                    </Button>
                </div>
            </form>
        </CardContent>
    </Card>
</template>
