<script setup>
import { ref } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'

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
  <Dialog 
    v-model:visible="open" 
    modal 
    header="Delete Source"
    :style="{ width: '450px' }"
    :closable="false"
  >
    <div class="flex flex-column gap-4">
      <p class="text-red-600 font-medium">
        This action cannot be undone. This will permanently delete the source
        "{{ source?.Name }}" and remove all associated data.
      </p>
      
      <div class="flex flex-column gap-2">
        <p class="text-sm text-gray-600">
          Please type <span class="font-semibold">{{ source?.Name }}</span> to confirm.
        </p>
        <InputText
          v-model="confirmText"
          placeholder="Type source name to confirm"
          :disabled="loading"
        />
        
        <small v-if="error" class="text-red-500">
          {{ error }}
        </small>
      </div>
    </div>

    <template #footer>
      <div class="flex gap-2 justify-end">
        <Button
          label="Cancel"
          severity="secondary"
          @click="$emit('update:open', false)"
          :disabled="loading"
        />
        <Button
          label="Delete Source"
          severity="danger"
          @click="handleDelete"
          :loading="loading"
          :disabled="confirmText !== source?.Name"
        />
      </div>
    </template>
  </Dialog>
</template>
