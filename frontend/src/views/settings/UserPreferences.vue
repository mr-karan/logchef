<script setup lang="ts">
import { ref } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

const { toast } = useToast()

// Preferences
const emailNotifications = ref(true)
const darkMode = ref(false)
const compactMode = ref(false)
const autoRefresh = ref(true)
const refreshInterval = ref(30)

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
            <h1 class="text-2xl font-bold tracking-tight">Preferences</h1>
            <p class="text-muted-foreground mt-2">
                Customize your application experience
            </p>
        </div>

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
    </div>
</template>
