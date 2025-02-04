Below is a set of specific instructions and sample code for building your **Explore.vue** page inspired by Grafana Loki and Kibana, using shadcn components. The design will feature a compact, one-line query editor and a toolbar (with multiple filter options, a calendar placeholder, and a Run Query button) at the top—separated from the main log display area by a clear separator.

---

## Overall UI Description

- **Inspiration & Style:**
  The design is clean, modern, and flat—taking cues from Grafana and Kibana. It uses a minimal color palette, ample whitespace, and a balanced use of primary (accent) colors to highlight interactive elements.

- **Layout Structure:**

  - **Top Toolbar:**

    - A horizontally laid-out toolbar with a one-line query input that stretches full width.
    - Additional controls include filter dropdowns, a calendar button (as a placeholder for future calendar functionality), and a “Run Query” button.
    - These elements are aligned using flexbox for consistent spacing and responsiveness.

  - **Separator:**

    - A subtle horizontal separator that visually divides the toolbar from the main log display area.

  - **Main Content Area:**
    - Below the toolbar and separator, a scrollable area will show the log results. This area will be built using simple table markup (or a custom LogsTable component) since all filtering and querying is done server side.

- **Visual Language:**
  - **Typography:** Use a clean sans-serif font (such as Inter or Roboto) with consistent sizing.
  - **Colors:**
    - **Primary/Accent:** Use a strong color (e.g., indigo or blue) for primary actions like the Run Query button.
    - **Neutral:** Soft backgrounds (light gray or off-white) with subtle borders to define areas.
  - **Spacing & Sizing:**
    - Use tight spacing for the toolbar elements. For example, the query input should have just enough height (one line, e.g., 2rem or 32px) to feel lightweight.
    - Elements like buttons and selects should have a consistent size to maintain a cohesive look.

---

## Specific Implementation Instructions with shadcn Components

### 1. **Create the Explore.vue File**

Set up your file with two major sections: the Top Toolbar and the Main Log Table area.

### 2. **Implementing the Top Toolbar**

#### **Components to Use:**

- **Input:**
  Use the shadcn `<Input>` component for the query editor.

- **Select/Dropdown:**
  Use the shadcn `<Select>` component (or an equivalent dropdown) for filter options.

- **Button:**
  Use `<Button>` for the Calendar (as a placeholder) and Run Query controls.

- **Separator:**
  Use `<Separator>` to divide the toolbar from the main content area.

#### **Instructions & Code Sample:**

```vue
<template>
  <div class="p-4 space-y-4">
    <!-- Top Toolbar -->
    <div class="flex items-center space-x-4">
      <!-- One-line Query Input: full width, fixed height -->
      <Input
        v-model="query"
        placeholder="Enter your query (e.g., SELECT * FROM logs WHERE ...)"
        class="w-full h-10"
      />

      <!-- Filter Option: a simple dropdown select -->
      <Select
        v-model="selectedFilter"
        :options="filterOptions"
        placeholder="Filter"
        class="w-40"
      />

      <!-- Calendar Placeholder: Button with outline variant -->
      <Button variant="outline" class="w-32"> Calendar </Button>

      <!-- Run Query Button: primary action -->
      <Button variant="primary" class="w-32" @click="runQuery">
        Run Query
      </Button>
    </div>

    <!-- Separator: subtle horizontal rule -->
    <Separator />

    <!-- Main Content Area: Logs Table -->
    <div class="overflow-auto">
      <!-- Replace this with your actual LogsTable component or table markup -->
      <LogsTable :logs="logData" />
    </div>
  </div>
</template>

<script setup>
import { ref } from "vue";
import { Input } from "@/components/ui/input"; // adjust path as needed
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import LogsTable from "@/components/LogsTable.vue"; // your custom logs table component

// Reactive state for the query and filter
const query = ref("");
const selectedFilter = ref(null);

// Sample filter options
const filterOptions = ref([
  { label: "Error", value: "error" },
  { label: "Warning", value: "warning" },
  { label: "Info", value: "info" },
]);

// Dummy log data
const logData = ref([]);

// Function to run the query
function runQuery() {
  // Validate query (e.g., ensure non-empty)
  if (!query.value.trim()) {
    // Use a toast or alert component from shadcn to display error (not shown here)
    console.error("Query cannot be empty.");
    return;
  }
  // Trigger your API call to fetch log data based on the query and filter selections
  console.log(
    "Running query:",
    query.value,
    "with filter:",
    selectedFilter.value
  );
  // Populate logData with API response (for now, we simulate with dummy data)
  logData.value = [
    {
      timestamp: "2025-02-03T12:00:00Z",
      level: "INFO",
      message: "Sample log entry 1",
    },
    {
      timestamp: "2025-02-03T12:01:00Z",
      level: "ERROR",
      message: "Sample log entry 2",
    },
    // ...more entries
  ];
}
</script>

<style scoped>
/* Additional styling adjustments, if needed */
</style>
```

#### **Key Points:**

- **Query Input:**

  - Uses the shadcn `<Input>` component.
  - Has a fixed height (`h-10` which is roughly 2.5rem) to enforce the one-liner look.
  - Takes full width of the available space (`w-full`).

- **Filter Dropdown:**

  - Provides a concise width (`w-40`) to select filtering criteria.

- **Calendar Button:**

  - Serves as a placeholder for future calendar integration.
  - Uses an outline variant to indicate a secondary action.

- **Run Query Button:**

  - Uses a primary variant to signal the main action.
  - Triggers the `runQuery` function when clicked.

- **Separator:**
  - The `<Separator>` component visually divides the toolbar from the main content.

---

### 3. **Implementing the Main Log Table**

While you mentioned that filtering and querying are server side, the log table is still a key display element. Use a simple table or your custom `<LogsTable>` component styled with shadcn and Tailwind CSS.

- **Component Example:**
  Your `<LogsTable>` might look something like this (a simplified example):

```vue
<template>
  <table class="w-full text-sm">
    <thead class="border-b">
      <tr>
        <th class="p-2 text-left">Timestamp</th>
        <th class="p-2 text-left">Level</th>
        <th class="p-2 text-left">Message</th>
      </tr>
    </thead>
    <tbody>
      <tr
        v-for="(log, index) in logs"
        :key="index"
        class="border-b hover:bg-gray-50"
      >
        <td class="p-2">{{ log.timestamp }}</td>
        <td class="p-2">{{ log.level }}</td>
        <td class="p-2">{{ log.message }}</td>
      </tr>
    </tbody>
  </table>
</template>

<script setup>
const props = defineProps({
  logs: {
    type: Array,
    default: () => [],
  },
});
</script>

<style scoped>
/* Additional styling as needed */
</style>
```

---

## Final Notes

- **Cohesion & Consistency:**
  By relying solely on shadcn components and Tailwind, the design remains consistent across the entire Explore page. Use the provided spacing (`p-4`, `space-x-4`, etc.) and responsive classes to ensure the UI looks polished on all devices.

- **Extensibility:**
  The top toolbar is designed to be easily extended—when the calendar is ready to be implemented, you can replace or augment the Calendar button with a proper date picker component from shadcn.

- **User Experience:**
  The one-line query input minimizes clutter while still providing enough space for typical one-liner queries. Combined with the filter options and a clearly defined run action, the interface encourages quick and effective log querying.

Following these instructions should help you build an Explore.vue page that is not only functionally robust but also visually inspired by industry-leading tools like Grafana Loki and Kibana—all while using shadcn’s cohesive component library.
