<template>
  <div class="component-load-error">
    <h2>{{ t('pages.componentLoadFailed') }}</h2>
    <p>{{ t('pages.componentLoadDescription') }}</p>
    <ul>
      <li>{{ t('pages.networkIssues') }}</li>
      <li>{{ t('pages.componentBuildIssues') }}</li>
      <li>{{ t('pages.serverConfigurationIssues') }}</li>
    </ul>
    <div class="actions">
      <button @click="goBack" class="back-button">{{ t('pages.goBack') }}</button>
      <button @click="reload" class="reload-button">{{ t('pages.reloadPage') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';

const router = useRouter();
const { t } = useI18n();

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
