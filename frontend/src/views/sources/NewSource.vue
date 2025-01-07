<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>Add New Source</CardTitle>
        <CardDescription>
          Configure a new log source connection
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form class="space-y-4">
          <div class="space-y-2">
            <Label for="name">Source Name</Label>
            <Input id="name" placeholder="Production Logs" />
          </div>
          <div class="space-y-2">
            <Label for="type">Source Type</Label>
            <Input id="type" placeholder="Select source type" />
          </div>
        </form>
      </CardContent>
      <CardFooter class="flex justify-between">
        <Button variant="outline" @click="router.push({ name: 'SourcesList' })">Cancel</Button>
        <Button>Add Source</Button>
      </CardFooter>
    </Card>
  </div>
</template>
