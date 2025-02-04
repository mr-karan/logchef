<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
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
const isFormValid = ref(false)
const isLoading = ref(false)
const enableTTL = ref(true)
const enableAuth = ref(false)

const formSchema = toTypedSchema(z.object({
    table_name: z.string()
        .min(4, 'Table name must be at least 4 characters')
        .max(30, 'Table name must be less than 30 characters')
        .regex(/^[a-zA-Z][a-zA-Z0-9_]*$/, 'Table name must start with a letter and contain only letters, numbers, and underscores'),
    description: z.string()
        .max(100, 'Description must be less than 100 characters')
        .optional(),
    schema_type: z.enum(['managed', 'unmanaged'], {
        required_error: 'Please select a source type',
    }).default('managed'),
    host: z.string().min(1, 'Host is required').default('localhost'),
    port: z.number().min(1).max(65535).default(9000),
    database: z.string().min(1, 'Database is required').default('logs'),
    username: z.string().optional(),
    password: z.string().optional(),
    ttl_days: z.number().min(1, 'TTL days must be at least 1').default(90),
}).refine((data) => {
    // If auth is enabled, both username and password must be provided
    if (enableAuth.value) {
        return data.username && data.password
    }
    return true
}, {
    message: 'Username and password are required when authentication is enabled'
}))

const { toast } = useToast()

const formData = ref({
    table_name: '',
    description: '',
    schema_type: 'managed' as const,
    host: 'localhost',
    port: 9000,
    database: 'logs',
    username: '',
    password: '',
    ttl_days: 90,
})

const form = useForm({
    initialValues: formData.value,
    validationSchema: formSchema,
})

// Watch for form validity
watch([form.values, form.errors, form.meta, enableAuth], () => {
    const isValid = form.meta.value?.valid === true
    const hasRequiredFields = Boolean(form.values.host && form.values.database && form.values.table_name)
    const hasAuthFields = !enableAuth.value || (enableAuth.value && form.values.username && form.values.password)
    isFormValid.value = isValid && hasRequiredFields && hasAuthFields
})

const onSubmit = form.handleSubmit(async (values) => {
    try {
        isLoading.value = true
        const payload: CreateSourcePayload = {
            schema_type: values.schema_type,
            connection: {
                host: values.host,
                username: enableAuth.value ? values.username || '' : '',
                password: enableAuth.value ? values.password || '' : '',
                database: values.database,
                table_name: values.table_name,
            },
            description: values.description,
            ttl_days: enableTTL.value ? Number(values.ttl_days) : -1,
        }

        const response = await sourcesApi.createSource(payload)
        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Source created successfully',
            duration: TOAST_DURATION.SUCCESS,
        })
        router.push('/sources/manage')
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
    { value: 'managed', label: 'Managed' },
    { value: 'unmanaged', label: 'Unmanaged' },
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
                    Choose between managed or unmanaged source type
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
            </div>

            <div class="space-y-4">
                <FormField v-slot="{ componentField }" name="host">
                    <FormItem>
                        <FormLabel>Host Address (including port)</FormLabel>
                        <FormControl>
                            <Input type="text" placeholder="localhost:9000" v-bind="componentField" />
                        </FormControl>
                        <FormDescription>
                            The Clickhouse server address including port (e.g., localhost:9000 or example.com:9000).
                        </FormDescription>
                        <FormMessage />
                    </FormItem>
                </FormField>

                <FormField v-slot="{ componentField }" name="database">
                    <FormItem>
                        <FormLabel>Database</FormLabel>
                        <FormControl>
                            <Input type="text" placeholder="logs" v-bind="componentField" />
                        </FormControl>
                        <FormMessage />
                    </FormItem>
                </FormField>

                <div class="flex items-center justify-between rounded-lg border p-4">
                    <div class="space-y-0.5">
                        <label class="text-base font-medium">Enable Authentication</label>
                        <p class="text-sm text-muted-foreground">
                            Enable if your Clickhouse instance requires authentication
                        </p>
                    </div>
                    <Switch :checked="enableAuth" @update:checked="enableAuth = $event"
                        class="data-[state=checked]:bg-primary" />
                </div>

                <template v-if="enableAuth">
                    <FormField v-slot="{ componentField }" name="username">
                        <FormItem>
                            <FormLabel>Username</FormLabel>
                            <FormControl>
                                <Input type="text" v-bind="componentField" />
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
                </template>

                <div class="flex items-center justify-between rounded-lg border p-4">
                    <div class="space-y-0.5">
                        <label class="text-base font-medium">Enable TTL</label>
                        <p class="text-sm text-muted-foreground">
                            Set a time-to-live duration for log entries
                        </p>
                    </div>
                    <Switch :checked="enableTTL" @update:checked="enableTTL = $event"
                        class="data-[state=checked]:bg-primary" />
                </div>

                <FormField v-if="enableTTL" v-slot="{ componentField }" name="ttl_days">
                    <FormItem>
                        <FormLabel>TTL Days</FormLabel>
                        <FormControl>
                            <Input type="number" placeholder="90" v-bind="componentField" />
                        </FormControl>
                        <FormDescription>
                            Number of days after which logs will be automatically deleted
                        </FormDescription>
                        <FormMessage />
                    </FormItem>
                </FormField>
            </div>
        </div>

        <Button type="submit" :disabled="!isFormValid || isLoading" :loading="isLoading">
            Create Source
        </Button>
    </form>
</template>
