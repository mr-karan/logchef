<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Loader2, AlertCircle } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { savedQueriesApi } from '@/api/savedQueries';
import { getErrorMessage } from '@/api/types';

// Resolves /logs/saved/:queryId. Loads the saved query, then redirects to
// /logs/explore?source=...&id=... so the existing explorer hydration path
// picks it up.

const route = useRoute();
const router = useRouter();

const error = ref<string | null>(null);
const isLoading = ref(true);

onMounted(async () => {
  const queryIdParam = route.params.queryId as string | undefined;

  if (!queryIdParam) {
    error.value = 'Invalid saved-query URL. Missing query id.';
    isLoading.value = false;
    return;
  }

  const queryIdNum = parseInt(queryIdParam, 10);
  if (isNaN(queryIdNum) || queryIdNum <= 0) {
    error.value = 'Invalid saved-query URL. Query id must be numeric.';
    isLoading.value = false;
    return;
  }

  try {
    const response = await savedQueriesApi.resolve(queryIdNum);
    if (!response.data) {
      throw new Error('Saved query not found.');
    }

    await router.replace({
      path: '/logs/explore',
      query: {
        source: response.data.source_id.toString(),
        id: queryIdNum.toString(),
      },
    });
  } catch (err) {
    console.error('Failed to load saved query:', err);
    error.value = getErrorMessage(err) || 'Failed to load saved query. It may have been deleted or you may not have access.';
    isLoading.value = false;
  }
});

function goToCollections() {
  router.push('/logs/saved');
}
</script>

<template>
  <div class="flex flex-col items-center justify-center h-screen gap-4">
    <template v-if="isLoading && !error">
      <Loader2 class="h-8 w-8 animate-spin text-primary" />
      <p class="text-muted-foreground">Loading saved query...</p>
    </template>

    <template v-if="error">
      <div class="flex flex-col items-center gap-4 text-center">
        <AlertCircle class="h-12 w-12 text-destructive" />
        <h2 class="text-xl font-semibold">Unable to Load Saved Query</h2>
        <p class="text-muted-foreground max-w-md">{{ error }}</p>
        <Button @click="goToCollections">
          Go to Collections
        </Button>
      </div>
    </template>
  </div>
</template>
