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
import { TOAST_DURATION } from '@/lib/constants'

import { toTypedSchema } from '@vee-validate/zod'
import { useForm } from 'vee-validate'
import * as z from 'zod'
import type { CreateSourcePayload } from '@/api/sources'
import { isErrorResponse, getErrorMessage } from '@/api/types'

const router = useRouter()
const useSeparateFields = ref(true)
const isFormValid = ref(false)
const isLoading = ref(false)

const formSchema = toTypedSchema(z.object({
  table_name: z.string()
    .min(4, 'Table name must be at least 4 characters')
    .max(30, 'Table name must be less than 30 characters')
    .regex(/^[a-zA-Z][a-zA-Z0-9_]*$/, 'Table name must start with a letter and contain only letters, numbers, and underscores'),
  description: z.string()
    .max(100, 'Description must be less than 100 characters')
    .optional(),
  schema_type: z.enum(['http', 'otel', 'custom'], {
    required_error: 'Please select a schema type',
  }).default('http'),
  dsn: z.string().min(1, 'DSN is required').optional(),
  enable_ttl: z.boolean().default(false),
  ttl_days: z.number().min(1, 'TTL days must be at least 1').default(90),
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
}).refine((data) => {
  // If TTL is enabled, ttl_days must be provided and be a valid number
  return !data.enable_ttl || (data.enable_ttl && typeof data.ttl_days === 'number' && data.ttl_days >= 1)
}, {
  message: 'TTL days is required when TTL is enabled',
}))

const { toast } = useToast()

const formData = ref({
  table_name: '',
  description: '',
  schema_type: 'http' as const,
  dsn: '',
  enable_ttl: false,
  ttl_days: 90,
  host: 'localhost',
  port: 9000,
  database: 'logs',
  username: '',
  password: '',
})

const form = useForm({
  initialValues: formData.value,
  validationSchema: formSchema,
})

// Watch for form validity
watch([form.values, form.errors, form.meta], () => {
  const isValid = form.meta.value?.valid === true
  const hasRequiredFields = useSeparateFields.value
    ? Boolean(form.values.host && form.values.port && form.values.database)
    : Boolean(form.values.dsn)

  isFormValid.value = isValid && hasRequiredFields
})

const onSubmit = form.handleSubmit(async (values) => {
  try {
    isLoading.value = true
    const payload: CreateSourcePayload = {
      table_name: values.table_name,
      schema_type: values.schema_type,
      ttl_days: values.enable_ttl ? Number(values.ttl_days) : -1,
      dsn: useSeparateFields.value
        ? `${values.host}:${values.port}?database=${values.database}`
        : values.dsn || '',
      description: values.description,
    }

    const response = await sourcesApi.createSource(payload)
    if (isErrorResponse(response.data)) {
      toast({
        title: 'Error',
        description: getErrorMessage(response.data),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
    } else {
      toast({
        title: 'Success',
        description: 'Source created successfully',
        duration: TOAST_DURATION.SUCCESS,
      })
      router.push('/sources/manage')
    }
  } catch (error) {
    console.error('Error creating source:', error)
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  } finally {
    isLoading.value = false
  }
})

const sourceTypes = [
  { value: 'http', label: 'HTTP Common Log Format (NCSA)' },
  { value: 'otel', label: 'OTEL Application Logs' },
  { value: 'custom', label: 'Custom Log Format' },
]
</script>

<template>
  <form class="w-2/3 space-y-6" @submit="onSubmit">
    <FormField v-slot="{ componentField }" name="table_name">
      <FormItem>
        <FormLabel>Table Name</FormLabel>
        <FormControl>
          <Input type="text" placeholder="production_logs" v-bind="componentField" />
        </FormControl>
        <FormDescription>
          The name of the table in Clickhouse. Must start with a letter and contain only letters, numbers, and
          underscores.
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

    <div class="space-y-4">
      <div class="flex items-center justify-between rounded-lg border p-4">
        <div class="space-y-0.5">
          <label class="text-base font-medium">Enable TTL</label>
          <p class="text-sm text-muted-foreground">
            Set a time-to-live duration for log entries
          </p>
        </div>
        <FormField v-slot="{ field }" name="enable_ttl">
          <FormItem class="flex items-center space-x-2">
            <FormControl>
              <Switch :checked="field.value" @update:checked="field.onChange" class="data-[state=checked]:bg-primary" />
            </FormControl>
          </FormItem>
        </FormField>
      </div>

      <div v-if="form.values.enable_ttl" class="space-y-2">
        <FormField v-slot="{ componentField }" name="ttl_days">
          <FormItem>
            <FormLabel>TTL (days)</FormLabel>
            <FormControl>
              <Input type="number" :min="1" placeholder="90" v-bind="componentField" />
            </FormControl>
            <FormDescription>
              Number of days to keep log entries (minimum 1 day)
            </FormDescription>
            <FormMessage />
          </FormItem>
        </FormField>
      </div>
    </div>

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
