<script setup>
import { ref } from 'vue'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const props = defineProps({
  open: Boolean,
  source: Object,
})

const emit = defineEmits(['update:open', 'confirm'])

const confirmText = ref('')
const loading = ref(false)
const error = ref('')

const handleDelete = async () => {
  if (confirmText.value !== props.source.Name) {
    error.value = 'Please type the source name exactly to confirm deletion'
    return
  }

  loading.value = true
  emit('confirm')
  loading.value = false
  confirmText.value = ''
  error.value = ''
}
</script>

<template>
  <Dialog :open="open" @update:open="$emit('update:open', $event)">
    <DialogContent>
      <DialogHeader>
        <DialogTitle class="text-destructive">Delete Source</DialogTitle>
        <DialogDescription>
          This action cannot be undone. This will permanently delete the source
          "{{ source?.Name }}" and remove all associated data.
        </DialogDescription>
      </DialogHeader>
      
      <div class="py-4">
        <div class="space-y-4">
          <div class="space-y-2">
            <p class="text-sm text-muted-foreground">
              Please type <span class="font-semibold">{{ source?.Name }}</span> to confirm.
            </p>
            <Input
              v-model="confirmText"
              placeholder="Type source name to confirm"
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
          variant="destructive"
          @click="handleDelete"
          :loading="loading"
          :disabled="confirmText !== source?.Name"
        >
          Delete Source
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
