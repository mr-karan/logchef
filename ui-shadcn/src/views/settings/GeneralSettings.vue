<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>General Settings</CardTitle>
        <CardDescription>
          Manage your general application settings
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form class="space-y-4">
          <div class="space-y-2">
            <Label for="org-name">Organization Name</Label>
            <Input id="org-name" placeholder="Your Organization" />
          </div>
          <div class="space-y-2">
            <Label for="timezone">Timezone</Label>
            <Input id="timezone" placeholder="Select timezone" />
          </div>
        </form>
      </CardContent>
      <CardFooter>
        <Button>Save Changes</Button>
      </CardFooter>
    </Card>
  </div>
</template>
