<template>
  <div class="app-layout">
    <el-container>
      <el-aside width="240px">
        <!-- Logo -->
        <div class="logo">
          <h1>LogChef</h1>
        </div>

        <!-- Navigation Menu -->
        <el-menu router :default-active="$route.path">
          <el-menu-item index="/dashboard">
            <el-icon><DataLine /></el-icon>
            <span>Dashboard</span>
          </el-menu-item>

          <el-menu-item index="/explorer">
            <el-icon><Search /></el-icon>
            <span>Log Explorer</span>
          </el-menu-item>

          <el-menu-item index="/alerts">
            <el-icon><Bell /></el-icon>
            <span>Alerts</span>
          </el-menu-item>

          <el-menu-item index="/settings">
            <el-icon><Setting /></el-icon>
            <span>Settings</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-container>
        <el-header>
          <!-- Search Bar -->
          <el-input
            v-model="searchQuery"
            placeholder="Search logs..."
            :prefix-icon="Search"
            style="max-width: 300px"
          />
        </el-header>

        <el-main>
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </el-main>
      </el-container>
    </el-container>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import {
  DataLine,
  Search,
  Bell,
  Setting
} from '@element-plus/icons-vue'

const searchQuery = ref('')
</script>

<style>
.app-layout {
  min-height: 100vh;
}

.logo {
  padding: 16px;
  border-bottom: 1px solid var(--el-border-color-light);
}

.logo h1 {
  margin: 0;
  font-size: 18px;
}

.el-aside {
  border-right: 1px solid var(--el-border-color-light);
}

.el-header {
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--el-border-color-light);
}

/* Basic fade transition */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>