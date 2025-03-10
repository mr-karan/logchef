<template>
    <Dialog v-model:open="showDialog">
        <DialogTrigger as-child>
            <slot>
                <Button>
                    <Plus class="mr-2 h-4 w-4" />
                    Add Team
                </Button>
            </slot>
        </DialogTrigger>
        <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
                <DialogTitle>Add New Team</DialogTitle>
                <DialogDescription>
                    Create a new team by providing a name and description.
                </DialogDescription>
            </DialogHeader>
            <form @submit.prevent="handleSubmit">
                <div class="grid gap-4 py-4">
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="teamName" class="text-right">
                            Team Name
                        </Label>
                        <Input id="teamName" v-model="teamName" placeholder="Enter team name" class="col-span-3"
                            required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="description" class="text-right">
                            Description
                        </Label>
                        <Textarea id="description" v-model="description" placeholder="Enter team description"
                            class="col-span-3" :disabled="isLoading" />
                    </div>
                </div>
                <DialogFooter>
                    <Button type="submit" :disabled="isLoading">
                        <span v-if="isLoading" class="loader mr-2"></span>
                        {{ isLoading ? 'Creating...' : 'Create Team' }}
                    </Button>
                </DialogFooter>
            </form>
        </DialogContent>
    </Dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Plus } from 'lucide-vue-next'
import { useTeamsStore } from '@/stores/teams'

const emit = defineEmits<{
    (e: 'team-created', teamId?: number): void
}>()

const teamsStore = useTeamsStore()
const teamName = ref('')
const description = ref('')
const isLoading = ref(false)
const showDialog = ref(false)

const handleSubmit = async () => {
    if (isLoading.value) return

    isLoading.value = true
    try {
        const result = await teamsStore.createTeam({
            name: teamName.value,
            description: description.value
        })

        if (result.success && result.data) {
            // Reset form
            teamName.value = ''
            description.value = ''
            showDialog.value = false

            // Force reload teams to ensure we have the latest data
            await teamsStore.loadTeams(true)

            // Get the newly created team ID from the store - it should be the last one
            const newTeamId = teamsStore.teams.length > 0
                ? teamsStore.teams[teamsStore.teams.length - 1].id
                : undefined

            // Emit event to refresh teams list with the new team ID
            emit('team-created', newTeamId)
        }
        // No need for else block - errors are handled by the store's callApi function
    } catch (error) {
        // This catch block handles unexpected errors not caught by the store
        console.error('Unexpected error creating team:', error)
    } finally {
        isLoading.value = false
    }
}
</script>

<style scoped>
.add-team {
    max-width: 600px;
    margin: 2rem auto;
    padding: 1rem;
}

.form-group {
    margin-bottom: 1rem;
}

.form-group label {
    display: block;
    margin-bottom: 0.5rem;
}

.form-control {
    width: 100%;
    padding: 0.5rem;
    border: 1px solid #ddd;
    border-radius: 4px;
}

.btn {
    padding: 0.5rem 1rem;
    border: none;
    border-radius: 4px;
    cursor: pointer;
}

.btn-primary {
    background-color: #4CAF50;
    color: white;
}

.btn-primary:hover {
    background-color: #45a049;
}

.loader {
    width: 16px;
    height: 16px;
    border: 2px solid #FFF;
    border-bottom-color: transparent;
    border-radius: 50%;
    display: inline-block;
    animation: rotation 1s linear infinite;
}

@keyframes rotation {
    0% {
        transform: rotate(0deg);
    }

    100% {
        transform: rotate(360deg);
    }
}
</style>
