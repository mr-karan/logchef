<template>
  <v-app>
    <v-navigation-drawer v-model="drawer" location="left">
      <v-list>
        <v-list-item prepend-icon="mdi-home" title="Dashboard" value="dashboard" to="/" />
        <v-list-item prepend-icon="mdi-file-document-multiple" title="Log Explorer" value="explorer" to="/explorer" />
        <v-list-item prepend-icon="mdi-chart-box" title="Analytics" value="analytics" to="/analytics" />
        <v-list-item prepend-icon="mdi-cog" title="Settings" value="settings" to="/settings" />
      </v-list>
    </v-navigation-drawer>

    <v-app-bar location="top">
      <v-app-bar-nav-icon @click="drawer = !drawer"></v-app-bar-nav-icon>
      <v-app-bar-title>LogChef</v-app-bar-title>
      <v-spacer></v-spacer>
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
  </v-app>
</template>

<script lang="ts" setup>
import { ref } from 'vue'

const drawer = ref(true)
</script>
