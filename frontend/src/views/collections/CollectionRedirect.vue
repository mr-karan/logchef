<script setup lang="ts">
import { ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Loader2, AlertCircle } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { savedQueriesApi } from '@/api/savedQueries';
import { getErrorMessage } from '@/api/types';
import { useI18n } from 'vue-i18n';

// Resolves /logs/saved/:queryId. Loads the saved query, then redirects to
// /logs/explore?source=...&id=... so the existing explorer hydration path
// picks it up.

const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const error = ref<string | null>(null);
const isLoading = ref(true);
let loadToken = 0;

async function resolveSavedQuery(queryIdParam: string | string[] | undefined) {
  const token = ++loadToken;
  const rawQueryId = Array.isArray(queryIdParam) ? queryIdParam[0] : queryIdParam;
  error.value = null;
  isLoading.value = true;

  if (!rawQueryId) {
    error.value = t('pages.invalidSavedQueryMissingID');
    isLoading.value = false;
    return;
  }

  const queryIdNum = parseInt(rawQueryId, 10);
  if (isNaN(queryIdNum) || queryIdNum <= 0) {
    error.value = t('pages.invalidSavedQueryID');
    isLoading.value = false;
    return;
  }

  try {
    const preferredTeam = typeof route.query.team === 'string' ? route.query.team : undefined;
    const response = await savedQueriesApi.resolve(queryIdNum, preferredTeam);
    if (token !== loadToken) {
      return;
    }
    if (!response.data) {
      throw new Error(t('pages.savedQueryNotFound'));
    }

    await router.replace({
      path: '/logs/explore',
      query: {
        team: response.data.resolved_team_id.toString(),
        source: response.data.source_id.toString(),
        id: queryIdNum.toString(),
      },
    });
  } catch (err) {
    if (token !== loadToken) {
      return;
    }
    console.error('Failed to load saved query:', err);
    error.value = getErrorMessage(err) || t('pages.savedQueryLoadFailed');
    isLoading.value = false;
  }
}

watch(
  () => route.params.queryId,
  (queryId) => {
    resolveSavedQuery(queryId as string | string[] | undefined);
  },
  { immediate: true }
);

function goToCollections() {
  router.push('/logs/library');
}
</script>

<template>
  <div class="flex flex-col items-center justify-center h-screen gap-4">
    <template v-if="isLoading && !error">
      <Loader2 class="h-8 w-8 animate-spin text-primary" />
      <p class="text-muted-foreground">{{ t('pages.loadingSavedQuery') }}</p>
    </template>

    <template v-if="error">
      <div class="flex flex-col items-center gap-4 text-center">
        <AlertCircle class="h-12 w-12 text-destructive" />
        <h2 class="text-xl font-semibold">{{ t('pages.savedQueryLoadFailedTitle') }}</h2>
        <p class="text-muted-foreground max-w-md">{{ error }}</p>
        <Button @click="goToCollections">
          {{ t('pages.goToCollections') }}
        </Button>
      </div>
    </template>
  </div>
</template>
