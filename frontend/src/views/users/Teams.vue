<script setup lang="ts">
import { useTitle } from '@vueuse/core'

useTitle('Teams | LogChef')
</script>

<template>
    <div class="space-y-6">
        <!-- Teams content will be rendered here through router-view -->
        <router-view />
    </div>
</template>