<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'
import { ShieldAlert } from 'lucide-vue-next'

const router = useRouter()
</script>

<template>
    <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4">
        <div class="space-y-2 text-center">
            <ShieldAlert class="mx-auto h-12 w-12 text-destructive" />
            <h1 class="text-4xl font-bold">403</h1>
            <h2 class="text-xl font-semibold">Access Denied</h2>
            <p class="text-muted-foreground">
                You don't have permission to access this page.<br />
                Please contact your administrator for access.
            </p>
        </div>
        <div class="flex space-x-4">
            <Button variant="outline" @click="router.back()">
                Go Back
            </Button>
            <Button @click="router.push('/logs/explore')">
                Go to Explore
            </Button>
        </div>
    </div>
</template>