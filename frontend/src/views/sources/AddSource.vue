<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { useSourcesStore } from '@/stores/sources'
import { Code, ChevronRight, ChevronsUpDown, Info, Plus, Database } from 'lucide-vue-next'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Separator } from '@/components/ui/separator'

const router = useRouter()
const { toast } = useToast()
const sourcesStore = useSourcesStore()

// Form state
const tableMode = ref('create') // 'create' or 'connect'
const createTable = computed(() => tableMode.value === 'create')
const host = ref('')
const enableAuth = ref(false)
const username = ref('')
const password = ref('')
const database = ref('')
const tableName = ref('')
const description = ref('')
const ttlDays = ref(90)
const isSubmitting = ref(false)
const showSchema = ref(false)
const metaTSField = ref('_timestamp')

// Table creation messages
const createTableMessage = computed(() => createTable.value
    ? 'Let LogChef create the table with optimized schema for OTLP compatible fields and efficient compression'
    : 'Connect to an existing table (must be compatible with LogChef schema requirements)'
)

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
    const db = database.value || 'your_database'
    const table = tableName.value || 'your_table'
    return tableSchema
        .replace(/{database}/g, db)
        .replace(/{table_name}/g, table)
        .replace(/{ttl_days}/g, ttlDays.value.toString())
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

const handleTableModeChange = (value: string) => {
    tableMode.value = value
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

    isSubmitting.value = true

    try {
        const success = await sourcesStore.createSource({
            meta_is_auto_created: createTable.value,
            meta_ts_field: metaTSField.value,
            connection: {
                host: host.value,
                username: enableAuth.value ? username.value : '',
                password: enableAuth.value ? password.value : '',
                database: database.value,
                table_name: tableName.value,
            },
            description: description.value,
            ttl_days: ttlDays.value,
        })

        if (success) {
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

                        <div class="grid gap-4 md:grid-cols-2">
                            <div class="grid gap-2">
                                <Label for="database" class="required">Database</Label>
                                <Input id="database" v-model="database" placeholder="logs" required />
                            </div>

                            <div class="grid gap-2">
                                <Label for="table_name" class="required">Table Name</Label>
                                <Input id="table_name" v-model="tableName" placeholder="app_logs" required />
                            </div>
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

                        <RadioGroup v-model="tableMode" class="grid grid-cols-[1fr_auto_1fr] items-start gap-4">
                            <!-- Create Table Card -->
                            <Card :class="{ 'border-primary': tableMode === 'create' }" class="cursor-pointer"
                                @click="tableMode = 'create'">
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
                                @click="tableMode = 'connect'">
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
                                                table where your logs are already being ingested. You'll just need to
                                                specify which field contains the timestamp.</p>
                                        </div>
                                    </div>

                                    <!-- Timestamp Field Input -->
                                    <div v-if="tableMode === 'connect'" class="mt-4 border-t pt-4">
                                        <Label for="meta_ts_field" class="required">Timestamp Field Name</Label>
                                        <Input id="meta_ts_field" v-model="metaTSField" placeholder="_timestamp"
                                            required />
                                        <p class="text-sm text-muted-foreground mt-1">
                                            Specify the field name that contains the timestamp in your table. Must be of
                                            type DateTime or DateTime64.
                                        </p>
                                    </div>
                                </CardContent>
                            </Card>
                        </RadioGroup>

                        <!-- Additional Settings -->
                        <div class="space-y-4">
                            <h3 class="text-lg font-medium">Additional Settings</h3>

                            <div class="grid gap-2">
                                <Label for="description">Description</Label>
                                <Textarea id="description" v-model="description" placeholder="Optional description"
                                    rows="2" />
                            </div>

                            <div class="grid gap-2">
                                <Label for="ttl_days">TTL Days</Label>
                                <Input id="ttl_days" v-model="ttlDays" type="number" min="1" />
                                <p class="text-sm text-muted-foreground">
                                    Number of days to keep logs before automatic deletion
                                </p>
                            </div>
                        </div>
                    </div>
                </div>

                <div class="flex justify-end space-x-4">
                    <Button type="button" variant="outline" @click="router.push({ name: 'Sources' })">
                        Cancel
                    </Button>
                    <Button type="submit" :disabled="isSubmitting || !isValid">
                        {{ isSubmitting ? 'Creating...' : (!createTable ? 'Import Source' : 'Create Source') }}
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
