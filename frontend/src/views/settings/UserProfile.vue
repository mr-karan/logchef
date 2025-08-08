<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'
import { useAPITokensStore } from '@/stores/apiTokens'
import { Loader2, Plus, Trash2, Copy, Key, Calendar, Clock, Shield, AlertTriangle } from 'lucide-vue-next'
import { formatDate } from '@/utils/format'

const authStore = useAuthStore()
const usersStore = useUsersStore()
const apiTokensStore = useAPITokensStore()
const { toast } = useToast()

// Form state
const fullName = ref('')
const isSubmitting = ref(false)

// API Token state
const showCreateTokenDialog = ref(false)
const newTokenName = ref('')
const newTokenExpiry = ref('30d') // Default to 30 days
const isCreatingToken = ref(false)
const createdTokenData = ref<{ token: string; api_token: any } | null>(null)
const showTokenDisplay = ref(false)

// Expiry options
const expiryOptions = [
    { value: '7d', label: '7 days', hours: 7 * 24 },
    { value: '30d', label: '30 days', hours: 30 * 24 },
    { value: '90d', label: '90 days', hours: 90 * 24 },
    { value: 'never', label: 'Never expires', hours: null }
]

// Load user data and API tokens
onMounted(async () => {
    if (authStore.user) {
        fullName.value = authStore.user.full_name
    }
    // Load API tokens
    await apiTokensStore.loadTokens()
})

const handleSubmit = async () => {
    if (isSubmitting.value || !authStore.user) return

    isSubmitting.value = true

    const result = await usersStore.updateUser(authStore.user.id, {
        full_name: fullName.value,
    })

    if (result.success && result.data?.user) {
        // Update the local user state in auth store
        if (authStore.user) {
            authStore.$patch({
                user: {
                    ...authStore.user,
                    full_name: result.data.user.full_name
                }
            });
        }
    }

    isSubmitting.value = false
}

// API Token management functions
const handleCreateToken = async () => {
    if (!newTokenName.value.trim()) {
        toast({
            title: "Error",
            description: "Please enter a token name",
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
        expires_at: expiresAt?.toISOString() || null
    })
    
    if (result.success && result.data) {
        createdTokenData.value = result.data
        showCreateTokenDialog.value = false
        showTokenDisplay.value = true
        newTokenName.value = ''
        newTokenExpiry.value = '30d' // Reset to default
    }
    
    isCreatingToken.value = false
}

const handleDeleteToken = async (tokenId: number) => {
    await apiTokensStore.deleteToken(tokenId)
}

const copyToClipboard = async (text: string) => {
    try {
        await navigator.clipboard.writeText(text)
        toast({
            title: "Copied!",
            description: "Token copied to clipboard"
        })
    } catch (err) {
        toast({
            title: "Error",
            description: "Failed to copy to clipboard",
            variant: "destructive"
        })
    }
}

const closeTokenDisplay = () => {
    showTokenDisplay.value = false
    createdTokenData.value = null
}

// Token expiry utility functions
const isTokenExpired = (expiresAt: string | null): boolean => {
    if (!expiresAt) return false
    return new Date(expiresAt) < new Date()
}

const getExpiryStatus = (expiresAt: string | null) => {
    if (!expiresAt) return { text: 'Never expires', variant: 'secondary', isExpired: false }
    
    const expiry = new Date(expiresAt)
    const now = new Date()
    const isExpired = expiry < now
    
    if (isExpired) {
        return { text: `Expired ${formatDate(expiresAt)}`, variant: 'destructive', isExpired: true }
    }
    
    // Check if expiring soon (within 7 days)
    const daysUntilExpiry = Math.ceil((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
    if (daysUntilExpiry <= 7) {
        return { text: `Expires in ${daysUntilExpiry} day${daysUntilExpiry === 1 ? '' : 's'}`, variant: 'outline', isExpired: false }
    }
    
    return { text: `Expires ${formatDate(expiresAt)}`, variant: 'secondary', isExpired: false }
}
</script>

<template>
    <div class="space-y-6">
        <!-- Header -->
        <div>
            <h1 class="text-2xl font-bold tracking-tight">My Profile</h1>
            <p class="text-muted-foreground mt-2">
                Manage your account information
            </p>
        </div>

        <div class="space-y-6">
            <!-- Account Info -->
            <Card>
                <CardHeader>
                    <CardTitle>Account Information</CardTitle>
                    <CardDescription>
                        View your account details
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <dl class="space-y-4 text-sm">
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Email</dt>
                            <dd class="font-medium">{{ authStore.user?.email }}</dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Role</dt>
                            <dd class="font-medium capitalize">{{ authStore.user?.role }}</dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Last Login</dt>
                            <dd class="font-medium">
                                {{ authStore.user?.last_login_at ? formatDate(authStore.user.last_login_at) :
                                    'Never'
                                }}
                            </dd>
                        </div>
                        <div class="flex flex-col space-y-1">
                            <dt class="text-muted-foreground">Account Created</dt>
                            <dd class="font-medium">{{ formatDate(authStore.user?.created_at || '') }}</dd>
                        </div>
                    </dl>
                </CardContent>
            </Card>

            <!-- Profile Settings -->
            <Card>
                <CardHeader>
                    <CardTitle>Profile Settings</CardTitle>
                    <CardDescription>
                        Update your profile information
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <form @submit.prevent="handleSubmit" class="space-y-6">
                        <div class="space-y-4">
                            <div class="grid gap-2">
                                <Label for="full_name">Full Name</Label>
                                <Input id="full_name" v-model="fullName" required />
                            </div>
                        </div>

                        <div class="flex justify-end">
                            <Button type="submit" :disabled="isSubmitting">
                                <Loader2 v-if="isSubmitting" class="mr-2 h-4 w-4 animate-spin" />
                                Save Changes
                            </Button>
                        </div>
                    </form>
                </CardContent>
            </Card>

            <!-- API Tokens -->
            <Card>
                <CardHeader>
                    <div class="flex items-center justify-between">
                        <div>
                            <CardTitle class="flex items-center gap-2">
                                <Key class="h-5 w-5" />
                                API Tokens
                            </CardTitle>
                            <CardDescription>
                                Manage your personal access tokens for API authentication
                            </CardDescription>
                        </div>
                        <Dialog v-model:open="showCreateTokenDialog">
                            <DialogTrigger asChild>
                                <Button class="flex items-center gap-2">
                                    <Plus class="h-4 w-4" />
                                    Generate Token
                                </Button>
                            </DialogTrigger>
                            <DialogContent class="sm:max-w-[425px]">
                                <DialogHeader>
                                    <DialogTitle>Create API Token</DialogTitle>
                                    <DialogDescription>
                                        Create a new personal access token for API authentication.
                                    </DialogDescription>
                                </DialogHeader>
                                <div class="grid gap-4 py-4">
                                    <div class="grid gap-2">
                                        <Label for="token-name">Token Name</Label>
                                        <Input 
                                            id="token-name" 
                                            v-model="newTokenName"
                                            placeholder="e.g., My App Integration"
                                            @keydown.enter="handleCreateToken"
                                        />
                                    </div>
                                    <div class="grid gap-2">
                                        <Label for="token-expiry">Expiration</Label>
                                        <Select v-model="newTokenExpiry">
                                            <SelectTrigger id="token-expiry">
                                                <SelectValue placeholder="Select expiration" />
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
                                    <Alert>
                                        <Shield class="h-4 w-4" />
                                        <AlertDescription>
                                            The token will be shown only once. Make sure to copy it and store it securely.
                                        </AlertDescription>
                                    </Alert>
                                </div>
                                <DialogFooter>
                                    <Button 
                                        type="button" 
                                        variant="outline" 
                                        @click="showCreateTokenDialog = false"
                                    >
                                        Cancel
                                    </Button>
                                    <Button 
                                        @click="handleCreateToken"
                                        :disabled="isCreatingToken || !newTokenName.trim()"
                                    >
                                        <Loader2 v-if="isCreatingToken" class="mr-2 h-4 w-4 animate-spin" />
                                        Create Token
                                    </Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>
                    </div>
                </CardHeader>
                <CardContent>
                    <div v-if="apiTokensStore.isLoading" class="flex items-center justify-center py-8">
                        <Loader2 class="h-6 w-6 animate-spin" />
                    </div>
                    
                    <div v-else-if="apiTokensStore.tokens.length === 0" class="text-center py-8 text-muted-foreground">
                        <Key class="h-12 w-12 mx-auto mb-4 opacity-50" />
                        <p>No API tokens found</p>
                        <p class="text-sm">Create your first token to get started</p>
                    </div>
                    
                    <div v-else class="space-y-4">
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
                                </div>
                                <div class="flex items-center gap-4 text-sm text-muted-foreground">
                                    <div class="flex items-center gap-1">
                                        <Calendar class="h-3 w-3" />
                                        Created {{ formatDate(token.created_at) }}
                                    </div>
                                    <div v-if="token.last_used_at" class="flex items-center gap-1">
                                        <Clock class="h-3 w-3" />
                                        Last used {{ formatDate(token.last_used_at) }}
                                    </div>
                                    <div v-else class="flex items-center gap-1">
                                        <Clock class="h-3 w-3" />
                                        Never used
                                    </div>
                                </div>
                            </div>
                            <AlertDialog>
                                <AlertDialogTrigger asChild>
                                    <Button 
                                        variant="ghost" 
                                        size="sm"
                                        class="text-destructive hover:text-destructive"
                                    >
                                        <Trash2 class="h-4 w-4" />
                                    </Button>
                                </AlertDialogTrigger>
                                <AlertDialogContent>
                                    <AlertDialogHeader>
                                        <AlertDialogTitle>Delete API Token</AlertDialogTitle>
                                        <AlertDialogDescription>
                                            Are you sure you want to delete the token "{{ token.name }}"? 
                                            This action cannot be undone and any applications using this token will lose access.
                                        </AlertDialogDescription>
                                    </AlertDialogHeader>
                                    <AlertDialogFooter>
                                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                                        <AlertDialogAction
                                            class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                            @click="handleDeleteToken(token.id)"
                                        >
                                            Delete Token
                                        </AlertDialogAction>
                                    </AlertDialogFooter>
                                </AlertDialogContent>
                            </AlertDialog>
                        </div>
                    </div>
                </CardContent>
            </Card>
        </div>

        <!-- Token Display Dialog -->
        <Dialog v-model:open="showTokenDisplay">
            <DialogContent class="sm:max-w-[500px]">
                <DialogHeader>
                    <DialogTitle class="flex items-center gap-2">
                        <Key class="h-5 w-5" />
                        API Token Created
                    </DialogTitle>
                    <DialogDescription>
                        Your API token has been created successfully. Copy it now as it won't be shown again.
                    </DialogDescription>
                </DialogHeader>
                <div class="space-y-4">
                    <div>
                        <Label>Token Name</Label>
                        <div class="mt-1 font-medium">{{ createdTokenData?.api_token?.name }}</div>
                    </div>
                    <div v-if="createdTokenData?.api_token?.expires_at">
                        <Label>Expires</Label>
                        <div class="mt-1 text-sm text-muted-foreground">
                            {{ formatDate(createdTokenData.api_token.expires_at) }}
                        </div>
                    </div>
                    <div v-else>
                        <Label>Expires</Label>
                        <div class="mt-1 text-sm text-muted-foreground">Never</div>
                    </div>
                    <div>
                        <Label>Your API Token</Label>
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
                            <strong>Important:</strong> This token will only be displayed once. 
                            Store it securely and treat it like a password.
                        </AlertDescription>
                    </Alert>
                </div>
                <DialogFooter>
                    <Button @click="closeTokenDisplay" class="w-full">
                        I've copied my token
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    </div>
</template>
