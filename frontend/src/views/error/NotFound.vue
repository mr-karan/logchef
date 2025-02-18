<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'
import { FileQuestion } from 'lucide-vue-next'

const router = useRouter()
</script>

<template>
    <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4">
        <div class="space-y-2 text-center">
            <FileQuestion class="mx-auto h-12 w-12 text-muted-foreground" />
            <h1 class="text-4xl font-bold">404</h1>
            <h2 class="text-xl font-semibold">Page Not Found</h2>
            <p class="text-muted-foreground">
                The page you're looking for doesn't exist.<br />
                Please check the URL and try again.
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