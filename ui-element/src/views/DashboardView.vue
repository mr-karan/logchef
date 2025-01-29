<template>
  <div class="p-6">
    <h1 class="text-2xl font-semibold mb-6">Dashboard</h1>
    <el-card class="coming-soon flex items-center justify-center">
      <div class="text-center py-12">
        <el-icon class="text-4xl text-gray-400 mb-4"><DataLine /></el-icon>
        <h2 class="text-xl font-medium text-gray-600 mb-2">Dashboard Coming Soon</h2>
        <p class="text-gray-500">Real-time log analytics and insights will be available here</p>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { DataLine } from '@element-plus/icons-vue'
</script>
