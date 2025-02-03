<template>
  <div class="page-container">
    <header class="page-header">
      <h1>Alerts</h1>
    </header>
    <div class="page-content">
      <!-- Alerts content will go here -->
    </div>
  </div>
</template>

<script setup lang="ts">
</script>

<style scoped>
.page-container {
  height: 100%;
}

.page-header {
  padding-bottom: 1.5rem;
  border-bottom: 1px solid var(--surface-border);
  margin-bottom: 2rem;
}

.page-header h1 {
  font-size: 1.75rem;
  font-weight: 600;
  color: var(--text-color);
  margin: 0;
}

.page-content {
  background: var(--surface-card);
  border-radius: 12px;
  padding: 1.5rem;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
}
</style>
