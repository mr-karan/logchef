<template>
    <Dialog v-model:open="showDialog">
        <DialogTrigger as-child>
            <slot>
                <Button>
                    <Plus class="mr-2 h-4 w-4" />
                    {{ t('access.addTeam') }}
                </Button>
            </slot>
        </DialogTrigger>
        <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
                <DialogTitle>{{ t('access.addNewTeam') }}</DialogTitle>
                <DialogDescription>
                    {{ t('access.createTeamDescription') }}
                </DialogDescription>
            </DialogHeader>
            <form @submit.prevent="handleSubmit">
                <div class="grid gap-4 py-4">
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="teamName" class="text-right">
                            {{ t('access.teamName') }}
                        </Label>
                        <Input id="teamName" v-model="teamName" :placeholder="t('access.enterTeamName')" class="col-span-3"
                            required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="description" class="text-right">
                            {{ t('ui.description') }}
                        </Label>
                        <Textarea id="description" v-model="description" :placeholder="t('access.enterTeamDescription')"
                            class="col-span-3" :disabled="isLoading" />
                    </div>
                </div>
                <DialogFooter>
                    <Button type="submit" :disabled="isLoading">
                        <Loader2 v-if="isLoading" class="mr-2 h-4 w-4 animate-spin" />
                        {{ isLoading ? t('access.creating') : t('access.createTeam') }}
                    </Button>
                </DialogFooter>
            </form>
        </DialogContent>
    </Dialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
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
import { Plus, Loader2 } from 'lucide-vue-next'
import { useTeamsStore } from '@/stores/teams'

const emit = defineEmits<{
    (e: 'team-created', teamId?: number): void
}>()

const teamsStore = useTeamsStore()
const { t } = useI18n()
const teamName = ref('')
const description = ref('')
const showDialog = ref(false)

const { isLoading, error: _formError } = storeToRefs(teamsStore)

const isFormValid = computed(() => {
    return !!teamName.value.trim()
})

const handleSubmit = async () => {
    if (!isFormValid.value) return
    
    const result = await teamsStore.createTeam({
        name: teamName.value,
        description: description.value
    })
    
    // Check for success
    if (result && result.success) {
        // Reset form
        teamName.value = ''
        description.value = ''
        showDialog.value = false
        
        // Emit event to notify parent component
        const lastCreatedTeam = teamsStore.getLastCreatedTeam()
        emit('team-created', lastCreatedTeam?.id)
    }
}
</script>

<!-- Removed unnecessary CSS styles -->
