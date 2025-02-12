<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

function handleLogin() {
    authStore.login()
}
</script>

<template>
    <div class="min-h-screen flex items-center justify-center bg-background p-4">
        <Card class="mx-auto w-full max-w-sm">
            <CardHeader>
                <CardTitle class="text-2xl text-center">
                    Welcome to LogChef
                </CardTitle>
                <CardDescription class="text-center">
                    Your centralized log analytics platform
                </CardDescription>
            </CardHeader>
            <CardContent>
                <div class="grid gap-4">
                    <Button @click="handleLogin" class="w-full">
                        Sign in with SSO
                    </Button>
                </div>
            </CardContent>
        </Card>
    </div>
</template>