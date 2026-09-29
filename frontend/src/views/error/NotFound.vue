<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'
import { FileQuestion } from 'lucide-vue-next'

const router = useRouter()
const { t } = useI18n()
</script>

<template>
    <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4">
        <div class="space-y-2 text-center">
            <FileQuestion class="mx-auto h-12 w-12 text-muted-foreground" />
            <h1 class="text-4xl font-bold">404</h1>
            <h2 class="text-xl font-semibold">{{ t('pages.notFoundTitle') }}</h2>
            <p class="text-muted-foreground">
                {{ t('pages.notFoundDescription') }}<br />
                {{ t('pages.checkURL') }}
            </p>
        </div>
        <div class="flex space-x-4">
            <Button variant="outline" @click="router.back()">
                {{ t('pages.goBack') }}
            </Button>
            <Button @click="router.push('/logs/explore')">
                {{ t('pages.goToExplore') }}
            </Button>
        </div>
    </div>
</template>
