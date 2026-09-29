<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'
import { ShieldAlert } from 'lucide-vue-next'

const router = useRouter()
const { t } = useI18n()
</script>

<template>
    <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4">
        <div class="space-y-2 text-center">
            <ShieldAlert class="mx-auto h-12 w-12 text-destructive" />
            <h1 class="text-4xl font-bold">403</h1>
            <h2 class="text-xl font-semibold">{{ t('pages.accessDenied') }}</h2>
            <p class="text-muted-foreground">
                {{ t('pages.noPagePermission') }}<br />
                {{ t('pages.contactAdministrator') }}
            </p>
        </div>
        <div class="flex space-x-4">
            <Button variant="outline" @click="router.back()">
                {{ t('pages.goBack') }}
            </Button>
            <Button @click="router.push('/logs/explore')">
                {{ t('pages.goToExplore') }}
            </Button>
            <Button variant="destructive" @click="router.push('/auth/logout')">
                {{ t('pages.logout') }}
            </Button>
        </div>
    </div>
</template>
