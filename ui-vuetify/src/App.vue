<template>
  <v-app>
    <v-navigation-drawer
      v-model="drawer"
      location="left"
    >
      <v-list>
        <v-list-item
          prepend-icon="mdi-magnify"
          title="Explore"
          value="explore"
          to="/explore"
        />
        <v-list-item
          prepend-icon="mdi-database"
          title="Sources"
          value="sources"
          to="/sources"
        />
        <v-list-item
          prepend-icon="mdi-bell"
          title="Alerts"
          value="alerts"
          to="/alerts"
        />
      </v-list>
    </v-navigation-drawer>

    <v-app-bar location="top">
      <v-app-bar-nav-icon @click="drawer = !drawer" />
      <v-app-bar-title>LogChef</v-app-bar-title>
      <v-spacer />
      <v-btn icon>
        <v-icon>mdi-magnify</v-icon>
      </v-btn>
      <v-btn icon>
        <v-icon>mdi-help-circle</v-icon>
      </v-btn>
    </v-app-bar>

    <v-main>
      <v-container fluid>
        <router-view />
      </v-container>
    </v-main>
    <!-- Global notification -->
    <v-snackbar
      v-model="notification.state.show"
      :color="notification.state.color"
      :timeout="3000"
    >
      {{ notification.state.message }}
    </v-snackbar>
  </v-app>
</template>

<script lang="ts" setup>
import { ref, provide } from 'vue'
import { useNotification } from '@/composables/useNotification'

const drawer = ref(true)
const notification = useNotification()

// Make notification globally available
provide('notification', notification)
</script>
