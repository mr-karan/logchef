<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { ref, onMounted } from 'vue'
import { PageHeader, PageSection, EmptyState, LoadingState } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import TokenScopePicker from '@/components/tokens/TokenScopePicker.vue'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'
import { useAPITokensStore } from '@/stores/apiTokens'
import { Loader2, Plus, Trash2, Copy, Key, Calendar, Clock, Shield, AlertTriangle } from 'lucide-vue-next'
import { formatDate } from '@/utils/format'
import { formatScopes, READ_ONLY_SCOPES, type TokenScope } from '@/lib/tokenScopes'
import { getExpiryStatus, isTokenExpired } from '@/lib/tokenExpiry'

const { t } = useI18n();

const authStore = useAuthStore()
const usersStore = useUsersStore()
const apiTokensStore = useAPITokensStore()
const { toast } = useToast()

const fullName = ref('')
const isSubmitting = ref(false)

const showCreateTokenDialog = ref(false)
const newTokenName = ref('')
const newTokenExpiry = ref('30d')
const isCreatingToken = ref(false)
const newTokenScopes = ref<TokenScope[]>([...READ_ONLY_SCOPES])
const createdTokenData = ref<{ token: string; api_token: any } | null>(null)
const showTokenDisplay = ref(false)
const tokenToDelete = ref<{ id: number; name: string } | null>(null)

const expiryOptions = [
    { value: '7d', get label() { return t('settings.days', { count: 7 }); }, hours: 7 * 24 },
    { value: '30d', get label() { return t('settings.days', { count: 30 }); }, hours: 30 * 24 },
    { value: '90d', get label() { return t('settings.days', { count: 90 }); }, hours: 90 * 24 },
    { value: 'never', get label() { return t('ui.neverExpires'); }, hours: null }
]

onMounted(async () => {
    if (authStore.user) {
        fullName.value = authStore.user.full_name
    }
    await apiTokensStore.loadTokens()
})

const handleSubmit = async () => {
    if (isSubmitting.value || !authStore.user) return

    isSubmitting.value = true

    const result = await usersStore.updateUser(authStore.user.id, {
        full_name: fullName.value,
    })

    if (result.success && result.data) {
        // Update the local user state in auth store
        const userData = result.data as { user: { full_name: string } };
        if (authStore.user && userData.user) {
            authStore.$patch({
                user: {
                    ...authStore.user,
                    full_name: userData.user.full_name
                }
            });
        }
    }

    isSubmitting.value = false
}

const handleCreateToken = async () => {
    if (!newTokenName.value.trim()) {
        toast({
            get title() { return t('ui.error'); },
            get description() { return t('ui.pleaseEnterATokenName'); },
            variant: "destructive"
        })
        return
    }

    isCreatingToken.value = true
    
    // Calculate expiry date if not "never"
    let expiresAt = null
    const selectedOption = expiryOptions.find(opt => opt.value === newTokenExpiry.value)
    if (selectedOption?.hours) {
        const now = new Date()
        expiresAt = new Date(now.getTime() + selectedOption.hours * 60 * 60 * 1000)
    }
    
    const result = await apiTokensStore.createToken({
        name: newTokenName.value.trim(),
        expires_at: expiresAt?.toISOString() ?? undefined,
        scopes: newTokenScopes.value,
    })
    
    if (result.success && result.data) {
        createdTokenData.value = result.data as { token: string; api_token: any }
        showCreateTokenDialog.value = false
        showTokenDisplay.value = true
        newTokenName.value = ''
        newTokenExpiry.value = '30d' // Reset to default
        newTokenScopes.value = [...READ_ONLY_SCOPES]
    }
    
    isCreatingToken.value = false
}

const confirmDeleteToken = async () => {
    const target = tokenToDelete.value
    tokenToDelete.value = null
    if (!target) return
    await apiTokensStore.deleteToken(target.id)
}

const copyToClipboard = async (text: string) => {
    try {
        await navigator.clipboard.writeText(text)
    } catch (err) {
        toast({
            get title() { return t('ui.error'); },
            get description() { return t('ui.failedToCopyToClipboard'); },
            variant: "destructive"
        })
    }
}

const closeTokenDisplay = () => {
    showTokenDisplay.value = false
    createdTokenData.value = null
}

</script>

<template>
    <div class="space-y-6">
        <PageHeader :title="t('ui.profile')" :description="t('ui.manageYourAccountInformation')" />

        <PageSection :title="t('ui.accountInformation')" :description="t('ui.viewYourAccountDetails')">
            <dl class="space-y-4 text-sm">
                <div class="flex flex-col space-y-1">
                    <dt class='text-muted-foreground'>{{ t('ui.email') }}</dt>
                    <dd class="font-medium">{{ authStore.user?.email }}</dd>
                </div>
                <div class="flex flex-col space-y-1">
                    <dt class='text-muted-foreground'>{{ t('ui.role') }}</dt>
                    <dd class="font-medium capitalize">{{ authStore.user?.role }}</dd>
                </div>
                <div class="flex flex-col space-y-1">
                    <dt class='text-muted-foreground'>{{ t('ui.lastLogin') }}</dt>
                    <dd class="font-medium">
                        {{ authStore.user?.last_login_at ? formatDate(authStore.user.last_login_at) : t('ui.never') }}
                    </dd>
                </div>
                <div class="flex flex-col space-y-1">
                    <dt class='text-muted-foreground'>{{ t('ui.accountCreated') }}</dt>
                    <dd class="font-medium">{{ formatDate(authStore.user?.created_at || '') }}</dd>
                </div>
            </dl>
        </PageSection>

        <PageSection :title="t('ui.profileSettings')" :description="t('ui.updateYourProfileInformation')">
            <form @submit.prevent="handleSubmit" class="space-y-6">
                <div class="grid gap-2">
                    <Label for="full_name">{{ t('ui.fullName') }}</Label>
                    <Input id="full_name" v-model="fullName" required />
                </div>
                <div class="flex justify-end">
                    <Button type="submit" :disabled="isSubmitting">
                        <Loader2 v-if="isSubmitting" class="mr-2 h-4 w-4 animate-spin" />
                        {{ t('ui.saveChanges') }}
                    </Button>
                </div>
            </form>
        </PageSection>

        <PageSection :title="t('ui.aPITokens')" :description="t('ui.manageYourPersonalAccessTokensForAPIAuthentication')">
            <template #actions>
                <Dialog v-model:open="showCreateTokenDialog">
                            <DialogTrigger asChild>
                                <Button class="flex items-center gap-2">
                                    <Plus class="h-4 w-4" />
                                    {{ t('ui.generateToken') }}
                                </Button>
                            </DialogTrigger>
                            <DialogContent class="sm:max-w-[760px] max-h-[90vh] overflow-y-auto">
                                <DialogHeader>
                                    <DialogTitle>{{ t('ui.createAPIToken') }}</DialogTitle>
                                    <DialogDescription>
                                        {{ t('ui.createANewPersonalAccessTokenForAPIAuthentication') }}
                                    </DialogDescription>
                                </DialogHeader>
                                <div class="grid gap-4 py-4">
                                    <div class="grid gap-2">
                                        <Label for="token-name">{{ t('ui.tokenName') }}</Label>
                                        <Input 
                                            id="token-name" 
                                            v-model="newTokenName"
                                            :placeholder="t('ui.eGMyAppIntegration')"
                                            @keydown.enter="handleCreateToken"
                                        />
                                    </div>
                                    <div class="grid gap-2">
                                        <Label for="token-expiry">{{ t('ui.expiration') }}</Label>
                                        <Select v-model="newTokenExpiry">
                                            <SelectTrigger id="token-expiry">
                                                <SelectValue :placeholder="t('ui.selectExpiration')" />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem 
                                                    v-for="option in expiryOptions" 
                                                    :key="option.value" 
                                                    :value="option.value"
                                                >
                                                    {{ option.label }}
                                                </SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div class="grid gap-2">
                                        <Label>{{ t('ui.scopes') }}</Label>
                                        <TokenScopePicker v-model="newTokenScopes" />
                                    </div>
                                    <Alert>
                                        <Shield class="h-4 w-4" />
                                        <AlertDescription>
                                            {{ t('ui.theTokenWillBeShownOnlyOnceMakeSureToCopyIt') }}
                                        </AlertDescription>
                                    </Alert>
                                </div>
                                <DialogFooter>
                                    <Button 
                                        type="button" 
                                        variant="outline" 
                                        @click="showCreateTokenDialog = false"
                                    >
                                        {{ t('ui.cancel') }}
                                    </Button>
                                    <Button 
                                        @click="handleCreateToken"
                                        :disabled="isCreatingToken || !newTokenName.trim() || newTokenScopes.length === 0"
                                    >
                                        <Loader2 v-if="isCreatingToken" class="mr-2 h-4 w-4 animate-spin" />
                                        {{ t('ui.createToken') }}
                                    </Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>
            </template>

            <LoadingState v-if="apiTokensStore.isLoading" />

            <EmptyState
                v-else-if="apiTokensStore.tokens.length === 0"
                :icon="Key"
                :title="t('ui.noAPITokens')"
                :description="t('ui.createYourFirstTokenToGetStarted')"
            />

            <div v-else class="space-y-3">
                        <div 
                            v-for="token in apiTokensStore.tokens" 
                            :key="token.id"
                            class="flex items-center justify-between p-4 border rounded-lg"
                        >
                            <div class="flex-1">
                                <div class="flex items-center gap-3 mb-2">
                                    <h4 class="font-medium" :class="{ 'text-muted-foreground line-through': isTokenExpired(token.expires_at) }">
                                        {{ token.name }}
                                    </h4>
                                    <Badge variant="secondary" class="font-mono text-xs">
                                        {{ token.prefix }}...
                                    </Badge>
                                    <Badge 
                                        :variant="getExpiryStatus(token.expires_at).variant"
                                        class="text-xs"
                                        :class="{ 
                                            'bg-destructive text-destructive-foreground': getExpiryStatus(token.expires_at).isExpired,
                                            'border-amber-500 text-amber-700': getExpiryStatus(token.expires_at).variant === 'outline'
                                        }"
                                    >
                                        <AlertTriangle v-if="getExpiryStatus(token.expires_at).isExpired" class="h-3 w-3 mr-1" />
                                        {{ getExpiryStatus(token.expires_at).text }}
                                    </Badge>
                                    <Badge variant="outline" class="text-xs">
                                        {{ formatScopes(token.scopes) }}
                                    </Badge>
                                </div>
                                <div class="flex items-center gap-4 text-sm text-muted-foreground">
                                    <div class="flex items-center gap-1">
                                        <Calendar class="h-3 w-3" />
                                        {{ t('ui.created') }} {{ formatDate(token.created_at) }}
                                    </div>
                                    <div v-if="token.last_used_at" class="flex items-center gap-1">
                                        <Clock class="h-3 w-3" />
                                        {{ t('ui.lastUsed') }} {{ formatDate(token.last_used_at) }}
                                    </div>
                                    <div v-else class="flex items-center gap-1">
                                        <Clock class="h-3 w-3" />
                                        {{ t('ui.neverUsed') }}
                                    </div>
                                </div>
                            </div>
                            <Button
                                variant="ghost"
                                size="sm"
                                class="text-destructive hover:text-destructive"
                                @click="tokenToDelete = { id: token.id, name: token.name }"
                            >
                                <Trash2 class="h-4 w-4" />
                            </Button>
                        </div>
                    </div>
        </PageSection>

        <!-- Token Display Dialog -->
        <Dialog v-model:open="showTokenDisplay">
            <DialogContent class="sm:max-w-[500px]">
                <DialogHeader>
                    <DialogTitle class="flex items-center gap-2">
                        <Key class="h-5 w-5" />
                        {{ t('ui.aPITokenCreated') }}
                    </DialogTitle>
                    <DialogDescription>
                        {{ t('ui.yourAPITokenHasBeenCreatedSuccessfullyCopyItNowAsIt') }}
                    </DialogDescription>
                </DialogHeader>
                <div class="space-y-4">
                    <div>
                        <Label>{{ t('ui.tokenName') }}</Label>
                        <div class="mt-1 font-medium">{{ createdTokenData?.api_token?.name }}</div>
                    </div>
                    <div v-if="createdTokenData?.api_token?.expires_at">
                        <Label>{{ t('ui.expires') }}</Label>
                        <div class="mt-1 text-sm text-muted-foreground">
                            {{ formatDate(createdTokenData.api_token.expires_at) }}
                        </div>
                    </div>
                    <div v-else>
                        <Label>{{ t('ui.expires') }}</Label>
                        <div class="mt-1 text-sm text-muted-foreground">{{ t('ui.never') }}</div>
                    </div>
                    <div>
                        <Label>{{ t('ui.yourAPIToken') }}</Label>
                        <div class="mt-2 p-3 bg-muted rounded-md">
                            <div class="flex items-center justify-between">
                                <code class="text-sm font-mono break-all">{{ createdTokenData?.token }}</code>
                                <Button 
                                    size="sm" 
                                    variant="outline"
                                    @click="copyToClipboard(createdTokenData?.token || '')"
                                    class="ml-2 flex-shrink-0"
                                >
                                    <Copy class="h-4 w-4" />
                                </Button>
                            </div>
                        </div>
                    </div>
                    <Alert>
                        <Shield class="h-4 w-4" />
                        <AlertDescription>
                            <strong>{{ t('ui.important') }}</strong> {{ t('ui.thisTokenWillOnlyBeDisplayedOnceStoreItSecurelyAndTreat') }}
                        </AlertDescription>
                    </Alert>
                </div>
                <DialogFooter>
                    <Button @click="closeTokenDisplay" class="w-full">
                        {{ t('ui.iVeCopiedMyToken') }}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>

        <ConfirmDialog
            :open="tokenToDelete !== null"
            :title="t('ui.deleteAPIToken')"
            :description="tokenToDelete ? t('settings.deleteToken', { name: tokenToDelete.name }) : undefined"
            :confirm-text="t('ui.delete')"
            destructive
            @update:open="(v) => { if (!v) tokenToDelete = null }"
            @confirm="confirmDeleteToken"
        />
    </div>
</template>
