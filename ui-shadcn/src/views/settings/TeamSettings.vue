<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Plus } from 'lucide-vue-next'
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>Team Management</CardTitle>
        <CardDescription>
          Manage your team members and their permissions
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div class="flex justify-between items-center mb-4">
          <h3 class="text-lg font-semibold">Team Members</h3>
          <Button>
            <Plus class="mr-2 h-4 w-4" />
            Invite Member
          </Button>
        </div>
        <div class="rounded-lg border p-4">
          No team members yet
        </div>
      </CardContent>
    </Card>
  </div>
</template>
