<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { useAuthStore } from '@/stores/auth'
import { usersApi } from '@/api/users'
import { Loader2 } from 'lucide-vue-next'
import { formatDate } from '@/utils/format'
import { handleApiCall } from '@/stores/base'

const { toast } = useToast()
const authStore = useAuthStore()

// Form state
const fullName = ref('')
const isSubmitting = ref(false)

// Preferences
const emailNotifications = ref(true)
const darkMode = ref(false)
const compactMode = ref(false)
const autoRefresh = ref(true)
const refreshInterval = ref(30)

// Load user data
onMounted(() => {
    if (authStore.user) {
        fullName.value = authStore.user.full_name
    }
})

const handleSubmit = async () => {
    if (isSubmitting.value) return

    isSubmitting.value = true

    try {
        const response = await handleApiCall({
            apiCall: () => usersApi.updateUser(authStore.user?.id || '', {
                full_name: fullName.value,
            }),
            successMessage: 'Profile updated successfully',
        })

        if (response.success && response.data?.user) {
            // Update the local user state
            authStore.$patch((state) => {
                if (state.user) {
                    state.user.full_name = response.data?.user?.full_name || state.user.full_name
                }
            })
        }
    } finally {
        isSubmitting.value = false
    }
}

const handleUpdatePreferences = () => {
    // TODO: Implement preferences update
    toast({
        title: 'Success',
        description: 'Preferences updated successfully',
        duration: TOAST_DURATION.SUCCESS,
    })
}
</script>

<template>
    <div class="space-y-6">
        <!-- Header -->
        <div>
            <h1 class="text-2xl font-bold tracking-tight">Profile Settings</h1>
            <p class="text-muted-foreground mt-2">
                Manage your account settings and preferences
            </p>
        </div>

        <!-- Tabs -->
        <Tabs defaultValue="profile" class="space-y-6">
            <TabsList>
                <TabsTrigger value="profile">Profile</TabsTrigger>
                <TabsTrigger value="preferences">Preferences</TabsTrigger>
            </TabsList>

            <!-- Profile Tab -->
            <TabsContent value="profile">
                <div class="space-y-6">
                    <!-- Account Info -->
                    <Card>
                        <CardHeader>
                            <CardTitle>Account Information</CardTitle>
                            <CardDescription>
                                View and update your account details
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
            </TabsContent>

            <!-- Preferences Tab -->
            <TabsContent value="preferences">
                <Card>
                    <CardHeader>
                        <CardTitle>User Preferences</CardTitle>
                        <CardDescription>
                            Customize your experience
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <form @submit.prevent="handleUpdatePreferences" class="space-y-6">
                            <!-- Notifications -->
                            <div class="space-y-4">
                                <h3 class="text-lg font-medium">Notifications</h3>
                                <div class="flex items-center justify-between">
                                    <div class="space-y-0.5">
                                        <Label>Email Notifications</Label>
                                        <p class="text-sm text-muted-foreground">
                                            Receive email notifications for important updates
                                        </p>
                                    </div>
                                    <Switch v-model="emailNotifications" />
                                </div>
                            </div>

                            <Separator />

                            <!-- Display -->
                            <div class="space-y-4">
                                <h3 class="text-lg font-medium">Display</h3>
                                <div class="flex items-center justify-between">
                                    <div class="space-y-0.5">
                                        <Label>Dark Mode</Label>
                                        <p class="text-sm text-muted-foreground">
                                            Enable dark mode for the interface
                                        </p>
                                    </div>
                                    <Switch v-model="darkMode" />
                                </div>
                                <div class="flex items-center justify-between">
                                    <div class="space-y-0.5">
                                        <Label>Compact Mode</Label>
                                        <p class="text-sm text-muted-foreground">
                                            Show more content with reduced spacing
                                        </p>
                                    </div>
                                    <Switch v-model="compactMode" />
                                </div>
                            </div>

                            <Separator />

                            <!-- Auto Refresh -->
                            <div class="space-y-4">
                                <h3 class="text-lg font-medium">Auto Refresh</h3>
                                <div class="flex items-center justify-between">
                                    <div class="space-y-0.5">
                                        <Label>Enable Auto Refresh</Label>
                                        <p class="text-sm text-muted-foreground">
                                            Automatically refresh log data
                                        </p>
                                    </div>
                                    <Switch v-model="autoRefresh" />
                                </div>
                                <div v-if="autoRefresh" class="grid gap-2">
                                    <Label for="refresh_interval">Refresh Interval (seconds)</Label>
                                    <Input id="refresh_interval" v-model="refreshInterval" type="number" min="5"
                                        max="300" />
                                </div>
                            </div>

                            <div class="flex justify-end">
                                <Button type="submit">Save Preferences</Button>
                            </div>
                        </form>
                    </CardContent>
                </Card>
            </TabsContent>
        </Tabs>
    </div>
</template>