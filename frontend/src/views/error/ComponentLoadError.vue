<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { CloudOff } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const router = useRouter()
const { t } = useI18n()

function reload() {
  window.location.reload()
}
</script>

<template>
  <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4 p-6">
    <div class="max-w-md space-y-2 text-center">
      <CloudOff class="mx-auto h-12 w-12 text-muted-foreground" />
      <h1 class="text-xl font-semibold">{{ t('pages.componentLoadFailed') }}</h1>
      <p class="text-muted-foreground">{{ t('pages.componentLoadDescription') }}</p>
      <ul class="list-inside list-disc text-left text-sm text-muted-foreground">
        <li>{{ t('pages.networkIssues') }}</li>
        <li>{{ t('pages.componentBuildIssues') }}</li>
        <li>{{ t('pages.serverConfigurationIssues') }}</li>
      </ul>
    </div>
    <div class="flex space-x-4">
      <Button variant="outline" @click="router.back()">{{ t('pages.goBack') }}</Button>
      <Button @click="reload">{{ t('pages.reloadPage') }}</Button>
    </div>
  </div>
</template>
