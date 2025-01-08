<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { sourcesApi } from '@/api/sources'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast/use-toast'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"

import { toTypedSchema } from '@vee-validate/zod'
import { useForm } from 'vee-validate'
import * as z from 'zod'

const router = useRouter()
const useSeparateFields = ref(true)
const isFormValid = ref(false)
const isLoading = ref(false)

const formSchema = toTypedSchema(z.object({
  name: z.string()
    .min(4, 'Name must be at least 4 characters')
    .max(30, 'Name must be less than 30 characters')
    .regex(/^[a-zA-Z0-9_]+$/, 'Name must contain only alphanumeric characters and underscores'),
  description: z.string()
    .max(100, 'Description must be less than 100 characters')
    .optional(),
  schema_type: z.enum(['ncsa', 'otel', 'custom'], {
    required_error: 'Please select a schema type',
  }).default('ncsa'),
  dsn: z.string().min(1, 'DSN is required').optional(),
  ttl_days: z.number().min(1).max(365).default(90),
  // Separate connection fields
  host: z.string().min(1, 'Host is required').default('localhost'),
  port: z.number().min(1).max(65535).default(9000),
  database: z.string().min(1, 'Database is required').default('logs'),
  username: z.string().min(1, 'Username is required').optional(),
  password: z.string().min(1, 'Password is required').optional(),
}).refine((data) => {
  if (useSeparateFields.value) {
    return data.host && data.port && data.database
  }
  return data.dsn
}, {
  message: 'Please fill in all connection details',
}))

const { toast } = useToast()

const { handleSubmit, values, errors, meta } = useForm({
  validationSchema: formSchema,
})

// Watch for form validity
watch([values, errors, meta], () => {
  const isValid = meta.value?.valid === true
  const hasRequiredFields = useSeparateFields.value
    ? Boolean(values.host && values.port && values.database)
    : Boolean(values.dsn)

  isFormValid.value = isValid && hasRequiredFields
})

const onSubmit = handleSubmit(async (values) => {
  try {
    isLoading.value = true

    let dsn = values.dsn

    // Convert separate fields to DSN if needed
    if (useSeparateFields.value) {
      const credentials = values.username && values.password
        ? `${values.username}:${values.password}@`
        : ''
      dsn = `clickhouse://${credentials}${values.host}:${values.port}/${values.database}`
    }

    // Ensure dsn is not undefined
    if (!dsn) {
      throw new Error('DSN is required')
    }

    // Prepare payload with correct types
    const payload = {
      name: values.name,
      schema_type: values.schema_type,
      ttl_days: values.ttl_days,
      dsn,
      description: values.description
    }

    await sourcesApi.createSource(payload)

    toast({
      title: 'Success',
      description: 'Source created successfully',
      duration: 3000,
    })

    // Redirect to sources management page
    router.push('/sources/manage')
  } catch (error) {
    let errorMessage = 'Failed to create source'
    let description = errorMessage

    if (axios.isAxiosError(error)) {
      const responseData = error.response?.data
      if (responseData?.error_type === 'ValidationError' && responseData?.data) {
        // Handle validation error with field-specific message
        errorMessage = 'Validation Error'
        description = `${responseData.data.field}: ${responseData.data.message}`
      } else {
        description = responseData?.message || errorMessage
      }
    }

    toast({
      title: errorMessage,
      description: description,
      variant: 'destructive',
      duration: 3000,
    })
  } finally {
    isLoading.value = false
  }
})

const sourceTypes = [
  { value: 'ncsa', label: 'NCSA Common Log Format' },
  { value: 'otel', label: 'OTEL Application Logs' },
  { value: 'custom', label: 'Custom Log Format' },
]
</script>

<template>
  <form class="w-2/3 space-y-6" @submit="onSubmit">
    <FormField v-slot="{ componentField }" name="name">
      <FormItem>
        <FormLabel>Source Name</FormLabel>
        <FormControl>
          <Input type="text" placeholder="Production Logs" v-bind="componentField" />
        </FormControl>
        <FormDescription>
          A unique name to identify this source
        </FormDescription>
        <FormMessage />
      </FormItem>
    </FormField>

    <FormField v-slot="{ componentField }" name="description">
      <FormItem>
        <FormLabel>Description</FormLabel>
        <FormControl>
          <Input type="text" placeholder="Optional description of this source" v-bind="componentField" />
        </FormControl>
        <FormDescription>
          A brief description of what this source is used for (optional)
        </FormDescription>
        <FormMessage />
      </FormItem>
    </FormField>

    <FormField v-slot="{ componentField }" name="schema_type">
      <FormItem>
        <FormLabel>Source Type</FormLabel>
        <Select v-bind="componentField">
          <FormControl>
            <SelectTrigger>
              <SelectValue placeholder="Select a source type" />
            </SelectTrigger>
          </FormControl>
          <SelectContent>
            <SelectItem v-for="type in sourceTypes" :key="type.value" :value="type.value">
              {{ type.label }}
            </SelectItem>
          </SelectContent>
        </Select>
        <FormDescription>
          The type of logs this source will collect
        </FormDescription>
        <FormMessage />
      </FormItem>
    </FormField>

    <div class="space-y-6 rounded-lg border p-6">
      <div class="flex items-center justify-between border-b pb-4">
        <div>
          <h3 class="text-lg font-medium">Connection Configuration</h3>
          <p class="text-sm text-muted-foreground">Configure how to connect to your Clickhouse instance</p>
        </div>
        <FormField v-slot="{ value, handleChange }" name="connectionMode">
          <FormItem class="flex flex-row items-center space-x-3">
            <div class="space-y-0.5 text-right">
              <FormLabel>Advanced Mode</FormLabel>
              <FormDescription class="text-xs">
                Use direct DSN connection string
              </FormDescription>
            </div>
            <FormControl>
              <Switch :checked="!useSeparateFields" @update:checked="(val) => useSeparateFields = !val" />
            </FormControl>
          </FormItem>
        </FormField>
      </div>

      <div v-if="!useSeparateFields">
        <FormField v-slot="{ componentField }" name="dsn">
          <FormItem>
            <FormLabel>Connection String (DSN)</FormLabel>
            <FormControl>
              <Input type="text" placeholder="clickhouse://user:pass@host:port/db" v-bind="componentField" />
            </FormControl>
            <FormDescription>
              Clickhouse connection string in DSN format
            </FormDescription>
            <FormMessage />
          </FormItem>
        </FormField>
      </div>

      <div v-if="useSeparateFields" class="grid grid-cols-2 gap-4">
        <FormField v-slot="{ componentField }" name="host">
          <FormItem>
            <FormLabel>Host</FormLabel>
            <FormControl>
              <Input type="text" placeholder="localhost" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="port">
          <FormItem>
            <FormLabel>Port</FormLabel>
            <FormControl>
              <Input type="number" placeholder="9000" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="database">
          <FormItem>
            <FormLabel>Database</FormLabel>
            <FormControl>
              <Input type="text" placeholder="default" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="username">
          <FormItem>
            <FormLabel>Username</FormLabel>
            <FormControl>
              <Input type="text" placeholder="default" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="password">
          <FormItem>
            <FormLabel>Password</FormLabel>
            <FormControl>
              <Input type="password" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>
      </div>
    </div>

    <Button type="submit" :disabled="!isFormValid || isLoading">
      {{ isLoading ? 'Creating...' : 'Create Source' }}
    </Button>
  </form>
</template>
