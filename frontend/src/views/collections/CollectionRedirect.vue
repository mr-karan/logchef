<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Loader2, AlertCircle } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';

const route = useRoute();
const router = useRouter();

const error = ref<string | null>(null);
const isLoading = ref(true);

onMounted(async () => {
  const { teamId, sourceId, collectionId } = route.params;

  // Validate params
  if (!teamId || !sourceId || !collectionId) {
    error.value = 'Invalid collection URL. Missing required parameters.';
    isLoading.value = false;
    return;
  }

  // Validate params are numeric
  const teamIdNum = parseInt(teamId as string);
  const sourceIdNum = parseInt(sourceId as string);
  const collectionIdNum = parseInt(collectionId as string);

  if (isNaN(teamIdNum) || isNaN(sourceIdNum) || isNaN(collectionIdNum)) {
    error.value = 'Invalid collection URL. Parameters must be numeric.';
    isLoading.value = false;
    return;
  }

  // Redirect to explore with query params
  try {
    await router.replace({
      path: '/logs/explore',
      query: {
        team: teamId as string,
        source: sourceId as string,
        query_id: collectionId as string,
      },
    });
  } catch (err) {
    console.error('Failed to redirect to collection:', err);
    error.value = 'Failed to load collection. Please try again or check your permissions.';
    isLoading.value = false;
  }
});

function goToCollections() {
  router.push('/logs/saved');
}
</script>

<template>
  <div class="flex flex-col items-center justify-center h-screen gap-4">
    <!-- Loading state -->
    <template v-if="isLoading && !error">
      <Loader2 class="h-8 w-8 animate-spin text-primary" />
      <p class="text-muted-foreground">Loading collection...</p>
    </template>

    <!-- Error state -->
    <template v-if="error">
      <div class="flex flex-col items-center gap-4 text-center">
        <AlertCircle class="h-12 w-12 text-destructive" />
        <h2 class="text-xl font-semibold">Unable to Load Collection</h2>
        <p class="text-muted-foreground max-w-md">{{ error }}</p>
        <Button @click="goToCollections">
          Go to Collections
        </Button>
      </div>
    </template>
  </div>
</template>
