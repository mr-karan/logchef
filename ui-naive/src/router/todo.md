- [x] There's no concept of offset in the API. Since we have a limit and have a paginator in the frontend.
- [x] The current toolbar doesn't feel intuitive in the Explore View. Time ranges typically are on the right side.
- [x] When the drawer is open we should have a copy button to copy the log to clipboard.
- [ ] When I click on Auto Refresh it should start refreshing the logs with the timer selected. The issue is that the refresh timer should basically set the start and end time to the current time and then refresh the logs. And when I click on auto refresh again it should stop. We should be careful that our log refresh watcher is only running when auto refresh is clicked with the current refresh interval. If interval updates we should update our refresh interval as well
- [x] Like I said Total Logs: 100 looks weird up right there, we can put it in the row where we have paginator. So paginator on right and total logs on left.
- [x] The API also reports the execution time of the query. We should display that in the toolbar as well next to the total logs
      "stats": {
      "execution_time_ms": 6.52
      }
- [ ] We can reduce the min-width if we have set for columns in our Log Table
- [ ] We should update value on close in our date picker

<template>
  <n-space vertical>
    <n-date-picker
      type="datetime"
      :default-value="Date.now()"
      :update-value-on-close="updateValueOnClose"
    />
    <n-date-picker
      :default-value="[Date.now(), Date.now()]"
      :update-value-on-close="updateValueOnClose"
      type="daterange"
    />
    <n-date-picker
      :default-value="[Date.now(), Date.now()]"
      :update-value-on-close="updateValueOnClose"
      type="datetimerange"
    />
    <n-switch v-model:value="updateValueOnClose" />
  </n-space>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'

export default defineComponent({
  setup() {
    return {
      updateValueOnClose: ref(true)
    }
  }
})
</script>
