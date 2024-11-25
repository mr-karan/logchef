<script setup>
import { ref } from 'vue'
import { api } from '@/services/api'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const props = defineProps({
  open: Boolean,
  sourceId: String,
  sourceName: String
})

const emit = defineEmits(['update:open', 'ttl-updated'])

const ttlDays = ref('')
const loading = ref(false)
const error = ref('')

const handleSubmit = async () => {
  if (!ttlDays.value || isNaN(ttlDays.value) || ttlDays.value <= 0) {
    error.value = 'Please enter a valid number of days'
    return
  }

  try {
    loading.value = true
    await api.updateSourceTTL(props.sourceId, parseInt(ttlDays.value))

    emit('ttl-updated')
    emit('update:open', false)
  } catch (err) {
    error.value = err.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="$emit('update:open', $event)">
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Update TTL for {{ sourceName }}</DialogTitle>
      </DialogHeader>
      
      <div class="py-4">
        <div class="space-y-4">
          <div class="space-y-2">
            <Label>TTL (days)</Label>
            <Input
              type="number"
              v-model="ttlDays"
              placeholder="Enter number of days"
              :disabled="loading"
            />
          </div>
          
          <div v-if="error" class="text-red-500 text-sm">
            {{ error }}
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          @click="$emit('update:open', false)"
          :disabled="loading"
        >
          Cancel
        </Button>
        <Button
          @click="handleSubmit"
          :loading="loading"
        >
          Update TTL
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
