<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { teamsApi } from '@/api/users'
import { isErrorResponse, getErrorMessage } from '@/api/types'

const router = useRouter()
const { toast } = useToast()

const name = ref('')
const description = ref('')
const isSubmitting = ref(false)

const handleSubmit = async () => {
    if (isSubmitting.value) return

    // Basic validation
    if (!name.value) {
        toast({
            title: 'Error',
            description: 'Please enter a team name',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    isSubmitting.value = true

    try {
        const response = await teamsApi.createTeam({
            name: name.value,
            description: description.value,
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
            description: 'Team created successfully',
            duration: TOAST_DURATION.SUCCESS,
        })

        // Redirect to manage teams
        router.push({ name: 'ManageTeams' })
    } catch (error) {
        console.error('Error creating team:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
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
            <CardTitle>Add Team</CardTitle>
            <CardDescription>
                Create a new team for your organization
            </CardDescription>
        </CardHeader>
        <CardContent>
            <form @submit.prevent="handleSubmit" class="space-y-6">
                <div class="space-y-4">
                    <div class="grid gap-2">
                        <Label for="name">Team Name</Label>
                        <Input id="name" v-model="name" placeholder="Engineering Team" required />
                    </div>

                    <div class="grid gap-2">
                        <Label for="description">Description</Label>
                        <Textarea id="description" v-model="description" placeholder="Team description" rows="3" />
                    </div>
                </div>

                <div class="flex justify-end space-x-4">
                    <Button type="button" variant="outline" @click="router.push({ name: 'ManageTeams' })">
                        Cancel
                    </Button>
                    <Button type="submit" :disabled="isSubmitting">
                        {{ isSubmitting ? 'Creating...' : 'Create Team' }}
                    </Button>
                </div>
            </form>
        </CardContent>
    </Card>
</template>