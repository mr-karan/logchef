<template>
  <v-row>
    <v-col cols="12">
      <h1 class="text-h4 mb-4">
        Dashboard
      </h1>
    </v-col>

    <v-col
      cols="12"
      md="6"
      lg="3"
    >
      <v-card>
        <v-card-item>
          <v-card-title>Total Logs</v-card-title>
          <div class="text-h4 pt-2">
            0
          </div>
        </v-card-item>
      </v-card>
    </v-col>

    <v-col
      cols="12"
      md="6"
      lg="3"
    >
      <v-card>
        <v-card-item>
          <v-card-title>Error Rate</v-card-title>
          <div class="text-h4 pt-2">
            0%
          </div>
        </v-card-item>
      </v-card>
    </v-col>

    <v-col
      cols="12"
      md="6"
      lg="3"
    >
      <v-card>
        <v-card-item>
          <v-card-title>Avg Response Time</v-card-title>
          <div class="text-h4 pt-2">
            0ms
          </div>
        </v-card-item>
      </v-card>
    </v-col>

    <v-col
      cols="12"
      md="6"
      lg="3"
    >
      <v-card>
        <v-card-item>
          <v-card-title>Active Sources</v-card-title>
          <div class="text-h4 pt-2">
            0
          </div>
        </v-card-item>
      </v-card>
    </v-col>

    <v-col
      cols="12"
      lg="8"
    >
      <v-card>
        <v-card-title class="px-4 pt-4">
          Log Activity
        </v-card-title>
        <v-card-text>
          <div class="text-center py-12 text-medium-emphasis">
            Log activity chart will be displayed here
          </div>
        </v-card-text>
      </v-card>
    </v-col>

    <v-col
      cols="12"
      lg="4"
    >
      <v-card>
        <v-card-title class="px-4 pt-4">
          Recent Errors
        </v-card-title>
        <v-card-text>
          <v-list lines="two">
            <v-list-item
              prepend-icon="mdi-alert-circle"
              title="No errors found"
              subtitle="Your logs are looking good!"
            />
          </v-list>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
// Dashboard component logic will go here
</script>
