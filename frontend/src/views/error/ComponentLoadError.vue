<template>
  <div class="component-load-error">
    <h2>Component Failed to Load</h2>
    <p>Sorry, we couldn't load this page component. This could be due to:</p>
    <ul>
      <li>Network connectivity issues</li>
      <li>Missing or incorrectly built component</li>
      <li>Server configuration problems</li>
    </ul>
    <div class="actions">
      <button @click="goBack" class="back-button">Go Back</button>
      <button @click="reload" class="reload-button">Reload Page</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';

const router = useRouter();

function goBack() {
  router.back();
}

function reload() {
  window.location.reload();
}
</script>

<style scoped>
.component-load-error {
  max-width: 600px;
  margin: 40px auto;
  padding: 20px;
  background-color: #f8f9fa;
  border-radius: 8px;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.1);
}

h2 {
  color: #dc3545;
  margin-bottom: 16px;
}

ul {
  margin-bottom: 24px;
  padding-left: 20px;
}

li {
  margin-bottom: 8px;
}

.actions {
  display: flex;
  gap: 12px;
  margin-top: 20px;
}

button {
  padding: 8px 16px;
  border-radius: 4px;
  border: none;
  cursor: pointer;
}

.back-button {
  background-color: #6c757d;
  color: white;
}

.reload-button {
  background-color: #007bff;
  color: white;
}
</style>
