<script setup>
import Sidebar from './Sidebar.vue'
</script>

<template>
  <div class="flex min-h-screen bg-slate-50">
    <Sidebar />
    <main class="flex-1 transition-all duration-300 pl-16">
      <slot />
    </main>
  </div>
</template>
