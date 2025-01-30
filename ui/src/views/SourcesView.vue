<template>
    <n-card>
        <n-tabs type="segment" v-model:value="activeTab">
            <n-tab-pane name="manage" tab="Manage Sources">
                <n-space vertical :size="24">
                    <n-spin :show="loading">
                        <div v-if="sources.length === 0" class="empty-state">
                            <n-empty description="No sources found">
                                <template #icon>
                                    <n-icon size="48" color="#ccc">
                                        <server-outline />
                                    </n-icon>
                                </template>
                                <template #extra>
                                    <n-button type="primary" @click="activeTab = 'add'">
                                        Add Your First Source
                                    </n-button>
                                </template>
                            </n-empty>
                        </div>
                        <n-data-table 
                            v-else 
                            :columns="columns" 
                            :data="sources" 
                            :pagination="pagination" 
                            :bordered="false" 
                        />
                    </n-spin>
                </n-space>
            </n-tab-pane>
            <n-tab-pane name="add" tab="Add Source">
                <n-space vertical :size="24">
                    <n-form
                        ref="formRef"
                        :model="formModel"
                        :rules="formRules"
                        label-placement="left"
                        label-width="120"
                        require-mark-placement="right-hanging"
                        style="max-width: 80%; margin: 0 auto;"
                    >
                        <n-card title="Connection Details" class="form-card">
                            <n-grid :cols="2" :x-gap="24">
                                <n-grid-item>
                                    <n-form-item label="Schema Type" path="schema_type" required>
                                        <n-select
                                            v-model:value="formModel.schema_type"
                                            :options="[
                                                { label: 'Managed', value: 'managed' },
                                                { label: 'Unmanaged', value: 'unmanaged' }
                                            ]"
                                            placeholder="Select schema type"
                                        />
                                    </n-form-item>
                                </n-grid-item>
                                <n-grid-item>
                                    <n-form-item label="Host" path="connection.host" required>
                                        <n-input
                                            v-model:value="formModel.connection.host"
                                            placeholder="localhost:9000"
                                        />
                                    </n-form-item>
                                </n-grid-item>
                            </n-grid>
                            <n-grid :cols="2" :x-gap="24">
                                <n-grid-item>
                                    <n-form-item label="Database" path="connection.database" required>
                                        <n-input
                                            v-model:value="formModel.connection.database"
                                            placeholder="Enter database name"
                                        />
                                    </n-form-item>
                                </n-grid-item>
                                <n-grid-item>
                                    <n-form-item label="Table Name" path="connection.table_name" required>
                                        <n-input
                                            v-model:value="formModel.connection.table_name"
                                            placeholder="Enter table name"
                                        />
                                    </n-form-item>
                                </n-grid-item>
                            </n-grid>
                        </n-card>

                        <n-card title="Authentication" class="form-card">
                            <n-form-item label="Authentication" path="auth_required">
                                <n-checkbox v-model:checked="formModel.auth_required">
                                    Enable Authentication
                                </n-checkbox>
                            </n-form-item>

                            <n-collapse-transition :show="formModel.auth_required">
                                <n-grid :cols="2" :x-gap="24">
                                    <n-grid-item>
                                        <n-form-item label="Username" path="connection.username">
                                            <n-input
                                                v-model:value="formModel.connection.username"
                                                placeholder="Enter username"
                                                :disabled="!formModel.auth_required"
                                                :status="formModel.auth_required && !formModel.connection.username ? 'error' : undefined"
                                            />
                                        </n-form-item>
                                    </n-grid-item>
                                    <n-grid-item>
                                        <n-form-item label="Password" path="connection.password">
                                            <n-input
                                                v-model:value="formModel.connection.password"
                                                type="password"
                                                show-password-on="click"
                                                placeholder="Enter password"
                                                :disabled="!formModel.auth_required"
                                                :status="formModel.auth_required && !formModel.connection.password ? 'error' : undefined"
                                            />
                                        </n-form-item>
                                    </n-grid-item>
                                </n-grid>
                            </n-collapse-transition>
                        </n-card>

                        <n-card title="Additional Settings" class="form-card">
                            <n-grid :cols="2" :x-gap="24">
                                <n-grid-item>
                                    <n-form-item label="TTL Days" path="ttl_days" required>
                                        <n-input-number
                                            v-model:value="formModel.ttl_days"
                                            :min="-1"
                                            placeholder="Enter TTL in days (-1 for no TTL)"
                                        />
                                    </n-form-item>
                                </n-grid-item>
                                <n-grid-item>
                                    <n-form-item label="Description" path="description">
                                        <n-input
                                            v-model:value="formModel.description"
                                            type="textarea"
                                            placeholder="Enter description (max 50 characters)"
                                            :maxlength="50"
                                            show-count
                                        />
                                    </n-form-item>
                                </n-grid-item>
                            </n-grid>
                        </n-card>

                        <n-space justify="end">
                            <n-button
                                type="primary"
                                @click="handleSubmit"
                                :loading="submitting"
                            >
                                Create Source
                            </n-button>
                        </n-space>
                    </n-form>
                </n-space>
            </n-tab-pane>
        </n-tabs>
    </n-card>
</template>

<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { NTag, NButton, useNotification, useDialog } from 'naive-ui'
import { EyeOutline, EyeOffOutline, ServerOutline } from '@vicons/ionicons5'
import api from '@/api/sources'
import type { Source } from '@/api/types'

const sources = ref<Source[]>([])
const loading = ref(true)
const submitting = ref(false)
const notification = useNotification()
const dialog = useDialog()
const formRef = ref()
const activeTab = ref('manage')

const formModel = ref({
    schema_type: '',
    connection: {
        host: '',
        username: '',
        password: '',
        database: '',
        table_name: ''
    },
    description: '',
    ttl_days: 7,
    auth_required: false
})

function isValidTableName(name: string): boolean {
    if (name.length === 0 || !/^[a-zA-Z]/.test(name)) {
        return false
    }
    return /^[a-zA-Z0-9_]+$/.test(name)
}

const formRules = {
    schema_type: {
        required: true,
        message: 'Schema type is required',
        trigger: ['blur', 'change']
    },
    connection: {
        host: {
            required: true,
            message: 'Host is required',
            trigger: ['blur', 'change']
        },
        username: {
            required: (rule: any, value: string) => {
                return formModel.value.auth_required
                    ? 'Username is required when authorization is enabled'
                    : true
            },
            trigger: ['blur', 'change']
        },
        password: {
            required: (rule: any, value: string) => {
                return formModel.value.auth_required
                    ? 'Password is required when authorization is enabled'
                    : true
            },
            trigger: ['blur', 'change']
        },
        database: {
            required: true,
            message: 'Database is required',
            trigger: ['blur', 'change']
        },
        table_name: {
            required: true,
            message: 'Table name is required',
            trigger: ['blur', 'change'],
            validator: (rule: any, value: string) => {
                if (!value) return true
                if (!isValidTableName(value)) {
                    return new Error('Table name must start with a letter and contain only letters, numbers, and underscores')
                }
                return true
            }
        }
    },
    description: {
        max: 50,
        message: 'Description must not exceed 50 characters',
        trigger: ['blur', 'change']
    },
    ttl_days: {
        required: true,
        type: 'number',
        message: 'TTL days must be -1 (no TTL) or a non-negative number',
        trigger: ['blur', 'change'],
        validator: (rule: any, value: number) => {
            if (value < -1) {
                return new Error('TTL days must be -1 (no TTL) or a non-negative number')
            }
            return true
        }
    }
}

async function handleDelete(id: string) {
    dialog.warning({
        title: 'Confirm Delete',
        content: 'Are you sure you want to delete this source? This action cannot be undone.',
        positiveText: 'Delete',
        negativeText: 'Cancel',
        onPositiveClick: async () => {
            try {
                await api.deleteSource(id)
                notification.success({
                    title: 'Success',
                    content: 'Source deleted successfully',
                    duration: 3000
                })
                loadSources()
            } catch (error) {
                console.error('Failed to delete source:', error)
            }
        }
    })
}

async function handleSubmit() {
    try {
        await formRef.value?.validate()
        submitting.value = true
        
        await api.createSource({
            schema_type: formModel.value.schema_type,
            connection: formModel.value.connection,
            description: formModel.value.description,
            ttl_days: formModel.value.ttl_days
        })

        notification.success({
            title: 'Success',
            content: 'Source created successfully',
            duration: 3000
        })

        // Reset form and reload sources
        formModel.value = {
            schema_type: '',
            connection: {
                host: '',
                username: '',
                password: '',
                database: '',
                table_name: ''
            },
            description: '',
            ttl_days: 7,
            auth_required: false
        }
        await loadSources()
        activeTab.value = 'manage'
    } catch (error) {
        console.error('Failed to create source:', error)
    } finally {
        submitting.value = false
    }
}

const pagination = {
    pageSize: 10
}

const columns = [
    {
        title: 'Table Name',
        key: 'connection.table_name',
        width: 200
    },
    {
        title: 'Database',
        key: 'connection.database',
        width: 150
    },
    {
        title: 'Status',
        key: 'is_connected',
        width: 120,
        render(row: Source) {
            return h(
                NTag,
                {
                    type: row.is_connected ? 'success' : 'error',
                    round: true
                },
                { default: () => row.is_connected ? 'Connected' : 'Disconnected' }
            )
        }
    },
    {
        title: 'Created',
        key: 'created_at',
        width: 150,
        render(row: Source) {
            return new Date(row.created_at).toLocaleDateString()
        }
    },
    {
        title: 'Description',
        key: 'description',
        render(row: Source) {
            if (!row.description) return null
            return h(
                'div',
                {
                    style: {
                        display: 'flex',
                        alignItems: 'center'
                    }
                },
                [
                    h(
                        NTag,
                        {
                            type: 'info',
                            bordered: false,
                            style: {
                                maxWidth: '300px',
                                textOverflow: 'ellipsis',
                                overflow: 'hidden',
                                whiteSpace: 'nowrap'
                            }
                        },
                        { default: () => row.description }
                    )
                ]
            )
        }
    },
    {
        title: 'Actions',
        key: 'actions',
        width: 100,
        render(row: Source) {
            return h(
                NButton,
                {
                    type: 'error',
                    size: 'small',
                    onClick: () => handleDelete(row.id)
                },
                { default: () => 'Delete' }
            )
        }
    }
]

async function loadSources() {
    try {
        loading.value = true
        sources.value = await api.listSources()
    } catch (error) {
        console.error('Failed to load sources:', error)
        sources.value = []
    } finally {
        loading.value = false
    }
}

onMounted(() => {
    loadSources()
})
</script>

<style scoped>
.form-card {
    margin-bottom: 24px;
}

.form-card :deep(.n-card-header) {
    padding: 12px 24px;
    font-size: 14px;
    font-weight: 600;
}

.form-card :deep(.n-card__content) {
    padding: 24px;
}
</style>

<style scoped>
.empty-state {
    padding: 48px;
    text-align: center;
}
</style>
