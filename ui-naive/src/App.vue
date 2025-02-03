<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <n-loading-bar-provider>
            <n-global-style />
            <n-layout has-sider class="layout-container">
              <nav-bar />
              <n-layout>
                <n-layout-content>
                  <router-view v-slot="{ Component }">
                    <transition name="fade" mode="out-in">
                      <component :is="Component" />
                    </transition>
                  </router-view>
                </n-layout-content>
              </n-layout>
            </n-layout>
          </n-loading-bar-provider>
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import NavBar from '@/components/NavBar.vue'

const themeOverrides = {
  common: {
    fontFamily: 'Inter, sans-serif',
    fontWeightStrong: '600'
  }
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.1s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
