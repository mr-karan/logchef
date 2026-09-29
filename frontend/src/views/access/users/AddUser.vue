<template>
    <Dialog v-model:open="showDialog">
        <DialogTrigger as-child>
            <slot>
                <Button>
                    <Plus class="mr-2 h-4 w-4" />
                    {{ t('access.addUser') }}
                </Button>
            </slot>
        </DialogTrigger>
        <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
                <DialogTitle>{{ t('access.addNewUser') }}</DialogTitle>
                <DialogDescription>
                    {{ t('access.createUserDescription') }}
                </DialogDescription>
            </DialogHeader>
            <form @submit.prevent="handleSubmit">
                <div class="grid gap-4 py-4">
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="full_name" class="text-right">{{ t('ui.fullName') }}</Label>
                        <Input id="full_name" v-model="formData.full_name" :placeholder="t('access.enterFullName')"
                            class="col-span-3" required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="email" class="text-right">{{ t('ui.email') }}</Label>
                        <Input id="email" v-model="formData.email" type="email" :placeholder="t('access.enterEmail')"
                            class="col-span-3" required :disabled="isLoading" />
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="role" class="text-right">{{ t('ui.role') }}</Label>
                        <div class="col-span-3">
                            <Select v-model="formData.role">
                                <SelectTrigger>
                                    <SelectValue :placeholder="t('access.selectRole')" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="admin">{{ t('access.roleAdmin') }}</SelectItem>
                                    <SelectItem value="member">{{ t('access.roleMember') }}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                    <div class="grid grid-cols-4 items-center gap-4">
                        <Label for="status" class="text-right">{{ t('ui.status') }}</Label>
                        <div class="col-span-3">
                            <Select v-model="formData.status">
                                <SelectTrigger>
                                    <SelectValue :placeholder="t('access.selectStatus')" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="active">{{ t('access.statusActive') }}</SelectItem>
                                    <SelectItem value="inactive">{{ t('access.statusInactive') }}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                </div>
                <DialogFooter>
                    <Button type="submit" :disabled="isLoading">
                        {{ isLoading ? t('access.creating') : t('access.createUser') }}
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
import { Plus } from 'lucide-vue-next'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import type { CreateUserRequest } from '@/api/users'
import { useUsersStore } from '@/stores/users'


const usersStore = useUsersStore()
const { t } = useI18n()
const showDialog = ref(false)

interface FormData extends CreateUserRequest {
    status: 'active' | 'inactive'
}

const formData = ref<FormData>({
    email: '',
    full_name: '',
    role: 'member',
    status: 'active',
})

const { isLoading, error: _formError } = storeToRefs(usersStore)

const isFormValid = computed(() => {
    return formData.value.email && formData.value.full_name
})

async function handleSubmit() {
    if (!isFormValid.value) return

    await usersStore.createUser({
        email: formData.value.email,
        full_name: formData.value.full_name,
        role: formData.value.role,
    })

    // Store handles success/failure states

    // Reset form on success - check if store operation succeeded
    if (!usersStore.error) {
        formData.value = {
            email: '',
            full_name: '',
            role: 'member',
            status: 'active',
        }
        showDialog.value = false
    }
}
</script>
