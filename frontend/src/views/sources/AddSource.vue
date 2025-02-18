<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { sourcesApi } from '@/api/sources'
import { isErrorResponse } from '@/api/types'

const router = useRouter()
const { toast } = useToast()

const schemaType = ref('managed')
const host = ref('')
const username = ref('')
const password = ref('')
const database = ref('')
const tableName = ref('')
const description = ref('')
const ttlDays = ref(0)
const isSubmitting = ref(false)

const handleSubmit = async () => {
    if (isSubmitting.value) return

    // Basic validation
    if (!host.value || !database.value || !tableName.value) {
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
        const response = await sourcesApi.createSource({
            schema_type: schemaType.value,
            connection: {
                host: host.value,
                username: username.value,
                password: password.value,
                database: database.value,
                table_name: tableName.value,
            },
            description: description.value,
            ttl_days: ttlDays.value,
        })

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

        // Redirect to sources list
        router.push({ name: 'Sources' })
    } catch (error) {
        console.error('Error creating source:', error)
        toast({
            title: 'Error',
            description: 'Failed to create source. Please try again.',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
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
                <div class="space-y-4">
                    <div class="grid gap-2">
                        <Label for="schema_type">Schema Type</Label>
                        <Select v-model="schemaType">
                            <SelectTrigger>
                                <SelectValue placeholder="Select schema type" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="managed">Managed</SelectItem>
                                <SelectItem value="unmanaged">Unmanaged</SelectItem>
                            </SelectContent>
                        </Select>
                        <p class="text-sm text-muted-foreground">
                            {{ schemaType === 'managed'
                                ? 'LogChef will create and manage the table schema'
                                : 'Use an existing table with custom schema'
                            }}
                        </p>
                    </div>

                    <div class="grid gap-2">
                        <Label for="host">Host</Label>
                        <Input id="host" v-model="host" placeholder="localhost:9000" required />
                    </div>

                    <div class="grid gap-4 md:grid-cols-2">
                        <div class="grid gap-2">
                            <Label for="username">Username</Label>
                            <Input id="username" v-model="username" placeholder="default" />
                        </div>

                        <div class="grid gap-2">
                            <Label for="password">Password</Label>
                            <Input id="password" v-model="password" type="password" />
                        </div>
                    </div>

                    <div class="grid gap-4 md:grid-cols-2">
                        <div class="grid gap-2">
                            <Label for="database">Database</Label>
                            <Input id="database" v-model="database" placeholder="logs" required />
                        </div>

                        <div class="grid gap-2">
                            <Label for="table_name">Table Name</Label>
                            <Input id="table_name" v-model="tableName" placeholder="app_logs" required />
                        </div>
                    </div>

                    <div class="grid gap-2">
                        <Label for="description">Description</Label>
                        <Textarea id="description" v-model="description" placeholder="Optional description" rows="2" />
                    </div>

                    <div class="grid gap-2">
                        <Label for="ttl_days">TTL Days</Label>
                        <Input id="ttl_days" v-model="ttlDays" type="number" min="0" placeholder="0" />
                        <p class="text-sm text-muted-foreground">
                            Number of days to keep logs. Set to 0 for no TTL.
                        </p>
                    </div>
                </div>

                <div class="flex justify-end space-x-4">
                    <Button type="button" variant="outline" @click="router.push({ name: 'Sources' })">
                        Cancel
                    </Button>
                    <Button type="submit" :disabled="isSubmitting">
                        {{ isSubmitting ? 'Creating...' : 'Create Source' }}
                    </Button>
                </div>
            </form>
        </CardContent>
    </Card>
</template>
