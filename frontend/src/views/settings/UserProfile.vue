<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'
import { Loader2 } from 'lucide-vue-next'
import { formatDate } from '@/utils/format'

const authStore = useAuthStore()
const usersStore = useUsersStore()

// Form state
const fullName = ref('')
const isSubmitting = ref(false)

// Load user data
onMounted(() => {
    if (authStore.user) {
        fullName.value = authStore.user.full_name
    }
})

const handleSubmit = async () => {
    if (isSubmitting.value || !authStore.user) return

    isSubmitting.value = true

    const result = await usersStore.updateUser(authStore.user.id, {
        full_name: fullName.value,
    })

    if (result.success && result.data?.user) {
        // Update the local user state in auth store
        if (authStore.user) {
            authStore.$patch({
                user: {
                    ...authStore.user,
                    full_name: result.data.user.full_name
                }
            });
        }
    }

    isSubmitting.value = false
}
</script>

<template>
    <div class="space-y-6">
        <!-- Header -->
        <div>
            <h1 class="text-2xl font-bold tracking-tight">My Profile</h1>
            <p class="text-muted-foreground mt-2">
                Manage your account information
            </p>
        </div>

        <div class="space-y-6">
            <!-- Account Info -->
            <Card>
                <CardHeader>
                    <CardTitle>Account Information</CardTitle>
                    <CardDescription>
                        View your account details
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <dl class="space-y-4 text-sm">
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Email</dt>
                            <dd class="font-medium">{{ authStore.user?.email }}</dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Role</dt>
                            <dd class="font-medium capitalize">{{ authStore.user?.role }}</dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Last Login</dt>
                            <dd class="font-medium">
                                {{ authStore.user?.last_login_at ? formatDate(authStore.user.last_login_at) :
                                    'Never'
                                }}
                            </dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Account Created</dt>
                            <dd class="font-medium">{{ formatDate(authStore.user?.created_at || '') }}</dd>
                        </div>
                    </dl>
                </CardContent>
            </Card>

            <!-- Profile Settings -->
            <Card>
                <CardHeader>
                    <CardTitle>Profile Settings</CardTitle>
                    <CardDescription>
                        Update your profile information
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <form @submit.prevent="handleSubmit" class="space-y-6">
                        <div class="space-y-4">
                            <div class="grid gap-2">
                                <Label for="full_name">Full Name</Label>
                                <Input id="full_name" v-model="fullName" required />
                            </div>
                        </div>

                        <div class="flex justify-end">
                            <Button type="submit" :disabled="isSubmitting">
                                <Loader2 v-if="isSubmitting" class="mr-2 h-4 w-4 animate-spin" />
                                Save Changes
                            </Button>
                        </div>
                    </form>
                </CardContent>
            </Card>
        </div>
    </div>
</template>
