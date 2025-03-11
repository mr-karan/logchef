<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { useSourcesStore } from '@/stores/sources'
import { sourcesApi } from '@/api/sources'
import { Code, ChevronsUpDown, Plus, Database } from 'lucide-vue-next'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Separator } from '@/components/ui/separator'

const router = useRouter()
const { toast } = useToast()
const sourcesStore = useSourcesStore()

// Define types for our API requests and responses
interface ConnectionInfo {
    host: string;
    username: string;
    password: string;
    database: string;
    table_name: string;
}

interface ValidateConnectionRequest extends ConnectionInfo {
    timestamp_field?: string;
    severity_field?: string;
}

interface ConnectionValidationResult {
    success: boolean;
    message: string;
}

interface CreateSourcePayload {
    meta_is_auto_created: boolean;
    meta_ts_field: string;
    meta_severity_field: string;
    connection: ConnectionInfo;
    description: string;
    ttl_days: number;
}

// Form state
const tableMode = ref<'create' | 'connect'>('create') // 'create' or 'connect'
const createTable = computed(() => tableMode.value === 'create')
const host = ref<string | number>('')
const enableAuth = ref<boolean>(false)
const username = ref<string | number>('')
const password = ref<string | number>('')
const database = ref<string | number>('')
const tableName = ref<string | number>('')
const description = ref<string | number>('')
const ttlDays = ref<string | number>(90)
const isSubmitting = ref<boolean>(false)
const showSchema = ref<boolean>(false)
const metaTSField = ref<string | number>('timestamp')
const metaSeverityField = ref<string | number>('severity_text')

// Validation state
const isValidating = ref(false)
const validationResult = ref<ConnectionValidationResult | null>(null)
const isValidated = ref(false) // Track if validation was successful

// Schema preview
const tableSchema = `CREATE TABLE IF NOT EXISTS "{database}"."{table_name}"
(
    timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
    trace_id String CODEC(ZSTD(1)),
    span_id String CODEC(ZSTD(1)),
    trace_flags UInt32 CODEC(ZSTD(1)),
    severity_text LowCardinality(String) CODEC(ZSTD(1)),
    severity_number Int32 CODEC(ZSTD(1)),
    service_name LowCardinality(String) CODEC(ZSTD(1)),
    namespace LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1)),
    log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),

    INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4,
    INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL {ttl_days} DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;`

// Computed schema with actual values
const actualSchema = computed(() => {
    const db = database.value ? String(database.value) : 'your_database'
    const table = tableName.value ? String(tableName.value) : 'your_table'
    return tableSchema
        .replace(/{database}/g, db)
        .replace(/{table_name}/g, table)
        .replace(/{ttl_days}/g, String(ttlDays.value))
})

// Form validation
const isValid = computed(() => {
    if (!host.value || !database.value || !tableName.value) return false
    if (enableAuth.value && (!username.value || !password.value)) return false
    return true
})

// Event handlers
const handleAuthToggle = (checked: boolean) => {
    enableAuth.value = checked
}

// Computed properties
const validateButtonText = computed(() => {
    if (isValidating.value) return 'Validating...'
    return tableMode.value === 'connect' ? 'Validate Connection & Columns' : 'Validate Connection'
})

const submitButtonText = computed(() => {
    if (isSubmitting.value) return 'Creating...'
    if (createTable.value) return 'Create Source'

    // For "Connect Existing Table" mode
    if (isValidated.value) return 'Import Source'
    return 'Validate & Import'
})

// Validate connection
const validateConnection = async () => {
    if (isValidating.value) return
    if (!host.value || !database.value) {
        toast({
            title: 'Error',
            description: 'Please fill in host and database fields',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    isValidating.value = true
    validationResult.value = null
    isValidated.value = false

    try {
        // Prepare request payload
        const payload: ValidateConnectionRequest = {
            host: String(host.value),
            username: enableAuth.value ? String(username.value) : '',
            password: enableAuth.value ? String(password.value) : '',
            database: String(database.value),
            table_name: String(tableName.value),
        }

        // Add timestamp and severity fields if connecting to existing table
        if (tableMode.value === 'connect' && tableName.value) {
            payload.timestamp_field = String(metaTSField.value)
            // Only add severity field if it's not empty
            if (metaSeverityField.value) {
                payload.severity_field = String(metaSeverityField.value)
            }
        }

        console.log('Validation payload:', payload)

        // Use the sourcesStore instead of direct API call
        const result = await sourcesStore.validateSourceConnection(payload)

        if (result.success) {
            validationResult.value = result.data

            if (result.data.success) {
                isValidated.value = true
                toast({
                    title: 'Success',
                    description: result.data.message,
                    variant: 'default',
                    duration: TOAST_DURATION.SUCCESS,
                })
            } else {
                toast({
                    title: 'Connection Error',
                    description: result.data.message,
                    variant: 'destructive',
                    duration: TOAST_DURATION.ERROR,
                })
            }
        } else {
            console.error('Validation error:', result)
            validationResult.value = {
                success: false,
                message: result.error || 'Failed to validate connection'
            }
            toast({
                title: 'Error',
                description: result.error || 'Failed to validate connection',
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
        }
    } catch (error) {
        console.error('Validation exception:', error)
        validationResult.value = {
            success: false,
            message: 'An unexpected error occurred during validation'
        }
        toast({
            title: 'Error',
            description: 'Failed to validate connection',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isValidating.value = false
    }
}

const handleSubmit = async () => {
    if (isSubmitting.value) return

    if (!isValid.value) {
        toast({
            title: 'Error',
            description: 'Please fill in all required fields',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    // For "Connect Existing Table" mode, validate first if not already validated
    if (!createTable.value && !isValidated.value) {
        await validateConnection()
        // If validation failed, don't proceed
        if (!isValidated.value) return
    }

    isSubmitting.value = true

    try {
        const result = await sourcesStore.createSource({
            meta_is_auto_created: createTable.value,
            meta_ts_field: String(metaTSField.value),
            meta_severity_field: metaSeverityField.value ? String(metaSeverityField.value) : "",
            connection: {
                host: String(host.value),
                username: enableAuth.value ? String(username.value) : '',
                password: enableAuth.value ? String(password.value) : '',
                database: String(database.value),
                table_name: String(tableName.value),
            },
            description: String(description.value),
            ttl_days: Number(ttlDays.value),
        } as CreateSourcePayload)

        if (result.success) {
            // Redirect to sources list
            router.push({ name: 'Sources' })
        }
    } finally {
        isSubmitting.value = false
    }
}
</script>

<template>
    <Card>
        <CardHeader>
            <CardTitle>Add Source</CardTitle>
            <CardDescription>
                Connect to a ClickHouse database and configure log ingestion
            </CardDescription>
        </CardHeader>
        <CardContent>
            <form @submit.prevent="handleSubmit" class="space-y-6">
                <div class="space-y-6">
                    <!-- Basic Info -->
                    <div class="space-y-4">
                        <h3 class="text-lg font-medium">Basic Information</h3>

                        <div class="grid gap-2">
                            <Label for="table_name" class="required">Source Name</Label>
                            <Input id="table_name" v-model="tableName" placeholder="app_logs" required />
                            <p class="text-sm text-muted-foreground">
                                This will be used as the table name in ClickHouse
                            </p>
                        </div>

                        <div class="grid gap-2">
                            <Label for="description">Description</Label>
                            <Textarea id="description" v-model="description" placeholder="Optional description"
                                rows="2" />
                        </div>
                    </div>

                    <!-- Connection Details -->
                    <div class="space-y-4">
                        <h3 class="text-lg font-medium">Connection Details</h3>

                        <div class="grid gap-2">
                            <Label for="host" class="required">Host</Label>
                            <Input id="host" v-model="host" placeholder="localhost:9000" required />
                            <p class="text-sm text-muted-foreground">
                                The ClickHouse server host and port
                            </p>
                        </div>

                        <div class="grid gap-2">
                            <Label for="database" class="required">Database</Label>
                            <Input id="database" v-model="database" placeholder="logs" required />
                        </div>

                        <!-- Auth Toggle -->
                        <div class="space-y-4">
                            <div class="flex items-center justify-between">
                                <div class="space-y-0.5">
                                    <Label>Enable Authentication</Label>
                                    <p class="text-sm text-muted-foreground">
                                        Use username and password authentication
                                    </p>
                                </div>
                                <Switch :checked="enableAuth" @update:checked="handleAuthToggle" />
                            </div>

                            <!-- Auth Fields -->
                            <div v-show="enableAuth"
                                class="grid gap-4 md:grid-cols-2 animate-in fade-in slide-in-from-top-2">
                                <div class="grid gap-2">
                                    <Label for="username" class="required">Username</Label>
                                    <Input id="username" v-model="username" placeholder="default"
                                        :required="enableAuth" />
                                </div>

                                <div class="grid gap-2">
                                    <Label for="password" class="required">Password</Label>
                                    <Input id="password" v-model="password" type="password" :required="enableAuth" />
                                </div>
                            </div>
                        </div>
                    </div>

                    <!-- Table Creation Option -->
                    <div class="space-y-4">
                        <h3 class="text-lg font-medium">Table Configuration</h3>

                        <RadioGroup :model-value="tableMode"
                            @update:model-value="(val) => tableMode = val as 'create' | 'connect'"
                            class="grid grid-cols-[1fr_auto_1fr] items-start gap-4">
                            <!-- Create Table Card -->
                            <Card :class="{ 'border-primary': tableMode === 'create' }" class="cursor-pointer"
                                @click="tableMode = 'create' as const">
                                <CardHeader>
                                    <div class="flex items-center gap-2">
                                        <RadioGroupItem value="create" id="create" />
                                        <Label for="create" class="cursor-pointer">Create New Table</Label>
                                    </div>
                                </CardHeader>
                                <CardContent class="space-y-4">
                                    <div class="flex items-start gap-4">
                                        <Plus class="h-5 w-5 mt-1 text-muted-foreground" />
                                        <div class="space-y-1">
                                            <p class="text-sm font-medium">Let LogChef create the table</p>
                                            <p class="text-sm text-muted-foreground">Optimized schema with
                                                OTLP-compatible fields and efficient compression</p>
                                        </div>
                                    </div>

                                    <!-- TTL Days for Create Table -->
                                    <div class="grid gap-2 mt-4 border-t pt-4">
                                        <Label for="ttl_days">TTL Days</Label>
                                        <Input id="ttl_days" v-model="ttlDays" type="number" min="1" />
                                        <p class="text-sm text-muted-foreground">
                                            Number of days to keep logs before automatic deletion
                                        </p>
                                    </div>

                                    <!-- Schema Preview -->
                                    <Collapsible v-model:open="showSchema" class="space-y-2">
                                        <CollapsibleTrigger asChild>
                                            <Button variant="outline" class="w-full flex items-center justify-between">
                                                <div class="flex items-center gap-2">
                                                    <Code class="h-4 w-4" />
                                                    <span>View Auto-Generated Schema</span>
                                                </div>
                                                <ChevronsUpDown class="h-4 w-4 transition-transform"
                                                    :class="{ 'rotate-180': showSchema }" />
                                            </Button>
                                        </CollapsibleTrigger>
                                        <CollapsibleContent>
                                            <div class="space-y-2">
                                                <p class="text-sm text-muted-foreground">This schema will be
                                                    automatically created for you when you submit the form:</p>
                                                <div class="rounded-md bg-muted p-4">
                                                    <pre
                                                        class="text-sm text-muted-foreground whitespace-pre-wrap">{{ actualSchema }}</pre>
                                                </div>
                                            </div>
                                        </CollapsibleContent>
                                    </Collapsible>
                                </CardContent>
                            </Card>

                            <!-- Separator -->
                            <div class="flex flex-col items-center justify-center h-full">
                                <div class="flex flex-col items-center gap-2">
                                    <Separator orientation="vertical" class="h-8" />
                                    <span class="text-sm text-muted-foreground px-4">or</span>
                                    <Separator orientation="vertical" class="h-8" />
                                </div>
                            </div>

                            <!-- Connect Table Card -->
                            <Card :class="{ 'border-primary': tableMode === 'connect' }" class="cursor-pointer"
                                @click="tableMode = 'connect' as const">
                                <CardHeader>
                                    <div class="flex items-center gap-2">
                                        <RadioGroupItem value="connect" id="connect" />
                                        <Label for="connect" class="cursor-pointer">Connect Existing Table</Label>
                                    </div>
                                </CardHeader>
                                <CardContent class="space-y-4">
                                    <div class="flex items-start gap-4">
                                        <Database class="h-5 w-5 mt-1 text-muted-foreground" />
                                        <div class="space-y-1">
                                            <p class="text-sm font-medium">Use an existing table</p>
                                            <p class="text-sm text-muted-foreground">Connect to an existing ClickHouse
                                                table where your logs are already being ingested. You'll need to
                                                specify which fields contain the timestamp and severity.</p>
                                        </div>
                                    </div>

                                    <!-- Timestamp and Severity Fields Input -->
                                    <div v-if="tableMode === 'connect'" class="mt-4 border-t pt-4 space-y-4">
                                        <div class="grid gap-2">
                                            <Label for="meta_ts_field" class="required">Timestamp Field Name</Label>
                                            <Input id="meta_ts_field" v-model="metaTSField" placeholder="timestamp"
                                                required />
                                            <p class="text-sm text-muted-foreground mt-1">
                                                Specify the field name that contains the timestamp in your table. Must
                                                be of
                                                type DateTime or DateTime64.
                                            </p>
                                        </div>

                                        <div class="grid gap-2">
                                            <Label for="meta_severity_field">Severity Field
                                                Name (Optional)</Label>
                                            <Input id="meta_severity_field" v-model="metaSeverityField"
                                                placeholder="severity_text" />
                                            <p class="text-sm text-muted-foreground mt-1">
                                                Optionally specify the field name that contains the severity level in
                                                your table. Leave empty if not needed.
                                            </p>
                                        </div>
                                    </div>
                                </CardContent>
                            </Card>
                        </RadioGroup>
                    </div>

                    <!-- Validation Section -->
                    <div v-if="!createTable" class="space-y-4">
                        <div class="flex justify-end">
                            <Button type="button" variant="outline" @click="validateConnection"
                                :disabled="isValidating || isValidated">
                                {{ isValidated ? 'Validated ✓' : validateButtonText }}
                            </Button>
                        </div>

                        <!-- Validation Result -->
                        <div v-if="validationResult" class="p-3 rounded-md text-sm"
                            :class="validationResult.success ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'">
                            {{ validationResult.message }}
                        </div>
                    </div>
                </div>

                <div class="flex justify-end space-x-4">
                    <Button type="button" variant="outline" @click="router.push({ name: 'Sources' })">
                        Cancel
                    </Button>
                    <Button type="submit" :disabled="isSubmitting || !isValid || (!createTable && !isValidated)">
                        {{ submitButtonText }}
                    </Button>
                </div>
            </form>
        </CardContent>
    </Card>
</template>

<style scoped>
.required::after {
    content: " *";
    color: hsl(var(--destructive));
}
</style>
