<script setup>
import { ref } from 'vue'
import { useToast } from '@/components/ui/toast/use-toast'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

const props = defineProps({
  open: Boolean
})

const emit = defineEmits(['update:open'])

const formData = ref({
  name: '',
  schema_type: '',
  dsn: '',
  ttl_days: 90
})

const schemaTypes = [
  { value: 'http', label: 'HTTP Logs' },
  { value: 'application', label: 'Application Logs' }
]

const { toast } = useToast()

const handleSubmit = async () => {
  try {
    const response = await fetch('/api/sources', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(formData.value)
    })

    const data = await response.json()

    if (!response.ok) {
      throw new Error(data.message || 'Failed to create source')
    }

    toast({
      title: "Success",
      description: "Source created successfully",
    })

    emit('update:open', false)
    emit('source-added')
  } catch (error) {
    toast({
      variant: "destructive",
      title: "Error",
      description: error.message,
    })
    console.error('Error creating source:', error)
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="$emit('update:open', $event)">
    <DialogContent class="sm:max-w-[425px]">
      <DialogHeader>
        <DialogTitle>Add New Source</DialogTitle>
      </DialogHeader>
      <form @submit.prevent="handleSubmit" class="space-y-4">
        <div class="space-y-2">
          <Label for="name">Name</Label>
          <Input id="name" v-model="formData.name" placeholder="my-nginx-logs" required minlength="4" maxlength="30" />
        </div>
        <div class="space-y-2">
          <Label for="schema-type">Schema Type</Label>
          <Select v-model="formData.schema_type" required>
            <SelectTrigger>
              <SelectValue placeholder="Select schema type" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="type in schemaTypes" :key="type.value" :value="type.value">
                {{ type.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="space-y-2">
          <Label for="dsn">DSN</Label>
          <Input id="dsn" v-model="formData.dsn" placeholder="clickhouse://localhost:9000/logs" required />
        </div>
        <div class="space-y-2">
          <Label for="ttl">TTL (days)</Label>
          <Input id="ttl" type="number" v-model="formData.ttl_days" />
        </div>
        <DialogFooter>
          <Button type="submit" class="w-full">Create Source</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
