<script setup lang="ts">
import AppSidebar from './components/AppSidebar.vue'
</script>

<template>
  <div class="app-container">
    <AppSidebar />
    <main class="main-content">
      <router-view></router-view>
    </main>
  </div>
</template>

<style>
.app-container {
  @apply flex min-h-screen bg-slate-50;
}

.main-content {
  @apply flex-1 p-8;
}
</style>
