<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useRouter, useRoute } from 'vue-router'
import { useTitle } from '@vueuse/core'

const router = useRouter()
const route = useRoute()

useTitle('Users | LogChef')

const tabs = [
    { name: 'Add User', route: { name: 'NewUser' } },
    { name: 'Manage Users', route: { name: 'ManageUsers' } }
]
</script>

<template>
    <div class="space-y-6">
        <div class="flex items-center justify-between">
            <h1 class="text-3xl font-bold tracking-tight">Users</h1>
        </div>

        <div class="flex space-x-4 border-b">
            <Button v-for="tab in tabs" :key="tab.name" variant="ghost" :class="[
                'relative h-9 rounded-none border-b-2 border-transparent px-4',
                route.name === tab.route.name && 'border-primary'
            ]" @click="router.push({ name: tab.route.name })">
                {{ tab.name }}
            </Button>
        </div>

        <router-view />
    </div>
</template>