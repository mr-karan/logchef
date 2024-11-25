<script setup lang="ts">
import MainLayout from './components/layout/MainLayout.vue'
import Toaster from '@/components/ui/toast/Toaster.vue'
import { RouterView } from 'vue-router'
</script>

<template>
  <MainLayout>
    <RouterView />
  </MainLayout>
  <Toaster />
</template>
