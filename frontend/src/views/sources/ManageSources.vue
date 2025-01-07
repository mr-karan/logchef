<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Plus, Pencil, Trash2 } from 'lucide-vue-next'
import { useRouter } from 'vue-router'

const router = useRouter()

const sources = [] // This will be populated with actual sources later
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>Manage Sources</CardTitle>
        <CardDescription>
          View and manage your log sources
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div v-if="sources.length === 0" class="rounded-lg border p-4 text-center">
          <p class="text-muted-foreground mb-4">No sources configured yet</p>
          <Button @click="router.push({ name: 'NewSource' })">
            <Plus class="mr-2 h-4 w-4" />
            Add Your First Source
          </Button>
        </div>
        <div v-else class="space-y-4">
          <!-- Sources list will go here -->
        </div>
      </CardContent>
    </Card>
  </div>
</template>
