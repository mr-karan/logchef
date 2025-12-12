<template>
  <div :class="['query-editor', props.class]">
    <!-- Header Bar (Keep existing structure) -->
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-1.5 border border-b-0">
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')" :title="props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'
            " aria-label="Toggle fields panel">
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for Mode Switching -->
        <Tabs :model-value="props.activeMode"
          @update:model-value="(value: string | number) => $emit('update:activeMode', asEditorMode(value), true)"
          class="w-auto">
          <TabsList class="grid grid-cols-2 w-fit">
            <TabsTrigger value="logchefql">
              <div class="flex-fix">
                <Search class="w-4 h-4" />
                <span>Search</span>
              </div>
            </TabsTrigger>
            <TabsTrigger value="clickhouse-sql">
              <div class="flex-fix">
                <Code2 class="w-4 h-4" />
                <span>SQL</span>
              </div>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <!-- AI Assistant Button -->
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1.5" @click="showAiDialog = true">
                <Wand2 class="h-3.5 w-3.5 text-purple-600" />
                <span class="text-xs font-medium">AI Assistant</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Generate SQL using natural language</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Query History Button -->
        <QueryHistoryDropdown
          :team-id="props.teamId"
          :source-id="props.sourceId"
          @load-query="handleLoadQueryFromHistory"
        />

        <!-- Table name indicator (Moved & Always Visible) -->
        <div class="text-xs text-muted-foreground ml-3">
          <template v-if="props.tableName">
            <span class="mr-1">Table:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">{{
              props.tableName
            }}</code>
          </template>
          <span v-else class="italic text-orange-500">No table selected</span>
        </div>

        <!-- New: Active Query Indicator -->
        <div v-if="activeSavedQueryName"
          class="flex items-center bg-primary/10 text-primary text-xs font-medium px-2 py-0.5 rounded-md ml-3">
          <FileEdit class="h-3.5 w-3.5 mr-1.5" />
          <span>{{ activeSavedQueryName }}</span>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <!-- New: New Query Button - Only show when editing a saved query -->
        <TooltipProvider v-if="isEditingExistingQuery">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1" @click="handleNewQueryClick">
                <FilePlus2 class="h-3.5 w-3.5" />
                <span class="text-xs">New</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Create a new query</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- SQL Toggle Button - Only show when in SQL mode -->
        <TooltipProvider v-if="props.activeMode === 'clickhouse-sql'">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1" @click="toggleSqlEditorVisibility">
                <EyeOff v-if="isEditorVisible" class="h-3.5 w-3.5" />
                <Eye v-else class="h-3.5 w-3.5" />
                <span class="text-xs">{{ isEditorVisible ? "Hide" : "Show" }} SQL</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>{{ isEditorVisible ? "Hide" : "Show" }} SQL query editor</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Saved Queries Dropdown -->
        <SavedQueriesDropdown :selected-source-id="props.sourceId" :selected-team-id="props.teamId"
          @select-saved-query="(query: SavedTeamQuery) => $emit('select-saved-query', query)"
          @save="$emit('save-query')" class="h-8" />

        <!-- Run Query Button - Integrated -->
        <TooltipProvider v-if="props.showRunButton && !props.isExecuting">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button 
                variant="default" 
                size="sm" 
                class="h-7 gap-1.5 px-3 bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm"
                :disabled="!props.canExecute"
                @click="$emit('execute')"
              >
                <Play class="h-3.5 w-3.5" />
                <span class="font-medium">Run</span>
                <kbd class="ml-1 text-[10px] bg-emerald-700/50 px-1 py-0.5 rounded hidden sm:inline">⌘↵</kbd>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Execute query (Ctrl+Enter)</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Cancel Query Button - Shows when executing -->
        <TooltipProvider v-if="props.showRunButton && props.isExecuting">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button 
                variant="destructive" 
                size="sm" 
                class="h-7 gap-1.5 px-3 shadow-sm"
                :disabled="props.isCancelling"
                @click="$emit('cancel-query')"
              >
                <template v-if="props.isCancelling">
                  <RefreshCw class="h-3.5 w-3.5 animate-spin" />
                  <span class="font-medium">Cancelling...</span>
                </template>
                <template v-else>
                  <Square class="h-3.5 w-3.5" />
                  <span class="font-medium">Cancel</span>
                  <kbd class="ml-1 text-[10px] bg-red-700/50 px-1 py-0.5 rounded hidden sm:inline">Esc</kbd>
                </template>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Cancel running query (Escape)</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Help Icon -->
        <HoverCard :open-delay="200">
          <HoverCardTrigger as-child>
            <button class="p-1 text-muted-foreground hover:text-foreground" aria-label="Show syntax help">
              <HelpCircle class="h-4 w-4" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent class="w-80 backdrop-blur-md bg-card text-card-foreground border-border shadow-lg"
            side="bottom" align="end">
            <!-- Help Content (Keep existing template) -->
            <div class="space-y-2">
              <h4 class="text-sm font-semibold">
                {{ props.activeMode === "logchefql" ? "LogchefQL" : "SQL" }}
                Syntax
              </h4>
              <div v-if="props.activeMode === 'logchefql'" class="text-xs space-y-1.5">
                <div>
                  <code class="bg-muted px-1 rounded">field="value"</code> -
                  Exact match
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field!="value"</code> -
                  Not equal
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field~"pattern"</code> -
                  Regex match
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field!~"pattern"</code> -
                  Regex exclusion
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field>100</code> -
                  Comparison
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">(c1 and c2) or c3</code> -
                  Grouping
                </div>
                <div class="pt-1">
                  <em>Example:
                    <code class="bg-muted px-1 rounded">level="error" and status>=500</code></em>
                </div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                <div>
                  <code class="bg-muted px-1 rounded">SELECT count() FROM {{ tableName || "table" }}</code>
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">WHERE field = 'value' AND time > now() - interval 1
              hour</code>
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">GROUP BY user ORDER BY count() DESC</code>
                </div>
                <div class="pt-1">
                  <em>Time range & limit applied if not specified. Use standard
                    ClickHouse SQL.</em>
                </div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Compact Variable Editor -->
    <div v-if="allVariables && allVariables.length > 0" class="mb-3">
      <!-- Variables Header -->
      <div class="flex items-center justify-between mb-2 px-1">
        <div class="flex items-center gap-2">
          <div class="w-1 h-3 bg-primary rounded-full"></div>
          <span class="text-xs font-medium text-foreground">Variables</span>
          <span class="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
            {{ allVariables.length }}
          </span>
        </div>
        <Button variant="ghost" size="sm" class="h-6 px-2 text-xs" @click="openAllVariableSettings"
          title="Configure variables">
          <Settings class="h-3 w-3 mr-1" />
          Configure
        </Button>
      </div>

      <!-- Compact Variables List -->
      <div class="bg-muted/20 border border-border/30 rounded-md p-2">
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2">
          <div v-for="variable in allVariables" :key="variable.name" class="flex items-center gap-2 min-w-0">
            <!-- Variable indicator and label -->
            <div class="flex items-center gap-1.5 min-w-0 flex-shrink-0">
              <div class="w-1.5 h-1.5 bg-primary/60 rounded-full flex-shrink-0"></div>
              <Label :for="`var-${variable.name}`"
                class="text-xs font-medium text-foreground truncate cursor-pointer min-w-0"
                :title="variable.label || variable.name">
                {{ variable.label || variable.name }}
              </Label>
              <span class="text-xs px-1 py-0.5 bg-muted text-muted-foreground rounded font-mono flex-shrink-0">
                {{ variable.type[0] }}
              </span>
            </div>

            <!-- Compact input -->
            <Input :id="`var-${variable.name}`" v-model="variable.value" :type="inputTypeFor(variable.type)"
              :placeholder="getPlaceholderForType(variable.type)"
              class="h-7 text-xs flex-1 min-w-0 border-muted-foreground/20 focus:border-primary/50 transition-colors"
              :class="{
                'border-primary/30 bg-primary/5': variable.value,
                'border-dashed': !variable.value
              }" />
          </div>
        </div>
      </div>
    </div>

    <!-- Monaco Editor Container -->
    <div class="editor-wrapper" :class="{ 'is-focused': editorFocused }"
      v-show="isEditorVisible || props.activeMode === 'logchefql'">
      <div class="editor-container" :class="{
        'is-empty': isEditorEmpty,
      }" :style="{ height: `${editorHeight}px` }" :data-placeholder="currentPlaceholder" :data-mode="props.activeMode">
        <!-- Monaco Editor Component with stable key to prevent remounting -->
        <vue-monaco-editor key="monaco-editor-instance" v-model:value="editorContent" :theme="theme"
          :language="props.activeMode" :options="monacoOptions" @mount="handleMount" @update:value="handleEditorChange"
          class="h-full w-full" />
      </div>
    </div>

    <!-- SQL Preview when editor is hidden -->
    <div v-if="
      !isEditorVisible &&
      props.activeMode === 'clickhouse-sql' &&
      !isEditorEmpty
    " class="sql-preview p-3 border border-border rounded-md bg-card/60 text-sm font-mono overflow-hidden cursor-pointer dark:bg-[#111522]"
      @click="isEditorVisible = true">
      <div class="flex items-center justify-between">
        <div class="text-muted-foreground text-xs font-medium mb-1">
          SQL Query (collapsed)
        </div>
        <Button variant="ghost" size="sm" class="h-6 px-2" @click.stop="isEditorVisible = true">
          <Eye class="h-3.5 w-3.5 mr-1" />
          <span class="text-xs">Show</span>
        </Button>
      </div>
      <div class="truncate text-xs text-muted-foreground">
        {{ editorContent }}
      </div>
    </div>

    <!-- Error Message Display -->
    <div v-if="validationError"
      class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center gap-2">
      <AlertCircle class="h-4 w-4 flex-shrink-0" />
      <span>
        <span class="font-medium">Validation Error: </span>
        {{ validationError }}
        <span v-if="validationError?.includes('Missing boolean operator')" class="block mt-1 text-xs">
          Hint: Use <code class="bg-muted px-1 rounded">and</code> or
          <code class="bg-muted px-1 rounded">or</code> between conditions.
          Example:
          <code class="bg-muted px-1 rounded">field1="value" and field2="value"</code>
        </span>
      </span>
    </div>
  </div>

  <!-- Enhanced Variable Settings Sheet -->
  <Sheet :open="showVariablesConfig" @update:open="(open) => !open && closeDrawer()">
    <SheetContent class="w-[480px] max-w-[90vw]">
      <SheetHeader class="pb-6">
        <SheetTitle class="text-lg flex items-center gap-2">
          <div class="w-2 h-2 bg-primary rounded-full"></div>
          <Settings class="h-5 w-5" />
          Variable Configuration
        </SheetTitle>
        <SheetDescription class="text-sm">
          Configure variables used in your query. Variables are replaced with actual values when the query runs.
        </SheetDescription>
      </SheetHeader>

      <div v-if="allVariables && allVariables.length > 0" class="space-y-6 max-h-[70vh] overflow-y-auto pr-2">
        <div v-for="(variable, index) in allVariables" :key="variable.name" class="space-y-4">
          <!-- Enhanced Variable Card -->
          <div class="border border-border rounded-lg p-4 bg-card hover:shadow-sm transition-all duration-200">
            <!-- Variable Header -->
            <div class="flex items-center justify-between mb-4">
              <div class="flex items-center gap-3">
                <div class="w-2 h-2 bg-primary/60 rounded-full flex-shrink-0"></div>
                <div>
                  <h4 class="font-medium text-foreground">{{ variable.name }}</h4>
                  <p class="text-xs text-muted-foreground">Variable {{ index + 1 }} of {{ allVariables.length }}</p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-xs px-2 py-1 bg-muted text-muted-foreground rounded font-mono">
                  {{ variable.type }}
                </span>
              </div>
            </div>

            <!-- Variable Configuration -->
            <div class="space-y-4">
              <!-- Variable Type -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Variable Type
                </Label>
                <Select v-model="variable.type" @update:model-value="() => updateVariableType(variable)">
                  <SelectTrigger class="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="text">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-blue-500 rounded-full"></div>
                        Text
                      </div>
                    </SelectItem>
                    <SelectItem value="number">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-green-500 rounded-full"></div>
                        Number
                      </div>
                    </SelectItem>
                    <SelectItem value="date">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-purple-500 rounded-full"></div>
                        Date
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <!-- Display Label -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Display Label
                </Label>
                <Input v-model="variable.label" placeholder="Enter display name..." class="h-9"
                  @input="() => variableStore.upsertVariable(variable)" />
              </div>

              <!-- Current Value Preview -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Current Value
                </Label>
                <div class="px-3 py-2 bg-muted/30 rounded-md border text-sm font-mono min-h-[36px] flex items-center">
                  <span v-if="variable.value" class="text-foreground">
                    {{ formatVariableValue(variable) }}
                  </span>
                  <span v-else class="text-muted-foreground italic">
                    No value set
                  </span>
                </div>

              </div>
            </div>
          </div>


        </div>
      </div>

      <!-- Enhanced Empty State -->
      <div v-else class="text-center py-12 text-muted-foreground">
        <div class="w-12 h-12 bg-muted/50 rounded-full flex items-center justify-center mx-auto mb-4">
          <Settings class="h-6 w-6 opacity-50" />
        </div>
        <p class="text-sm font-medium mb-2">No variables found in your query</p>
        <p class="text-xs">Use <code class="bg-muted px-1.5 py-0.5 rounded">&#123;&#123;variable_name&#125;&#125;</code>
          syntax to create variables</p>
        <div class="mt-4 text-xs text-muted-foreground/60">
          <p>Example: <code class="bg-muted px-1.5 py-0.5 rounded">namespace=&#123;&#123;env&#125;&#125;</code></p>
        </div>
      </div>
    </SheetContent>
  </Sheet>

  <!-- AI SQL Assistant Dialog -->
  <Dialog :open="showAiDialog" @update:open="showAiDialog = $event">
    <DialogContent class="sm:max-w-3xl max-h-[90vh] overflow-hidden">
      <!-- Header -->
      <DialogHeader class="border-b pb-4">
        <DialogTitle class="flex items-center gap-3">
          <Wand2 class="h-6 w-6 text-purple-600" />
          <span class="text-xl font-semibold text-gray-800">AI SQL Assistant</span>
        </DialogTitle>
        <DialogDescription class="text-gray-600 mt-2">
          Describe the data you want to retrieve in natural language, and I'll generate SQL for you.
        </DialogDescription>
      </DialogHeader>
      
      <div class="flex flex-col gap-6 py-4 overflow-y-auto max-h-[60vh]">
        <!-- Input Section -->
        <div class="space-y-3">
          <Label class="text-sm font-medium text-gray-700">What data are you looking for?</Label>
          <Textarea 
            ref="aiTextareaRef"
            v-model="aiNaturalQuery"
            placeholder="show logs from syslog namespace for the Scarface service from the past 12 hours."
            class="min-h-[120px] resize-y border-2 border-gray-300 focus:border-purple-500 bg-white shadow-sm appearance-none outline-none"
            style="border-style: solid !important; border-width: 2px !important; border-color: rgb(209 213 219) !important;"
            @keydown.meta.enter="handleAiSubmit"
            @keydown.ctrl.enter="handleAiSubmit"
          />
          <div class="flex items-center justify-between text-xs text-gray-500">
            <div>
              Press <kbd class="px-1.5 py-0.5 bg-gray-100 rounded font-mono">Ctrl+Enter</kbd> to generate
            </div>
            <details class="text-xs">
              <summary class="cursor-pointer hover:text-gray-700 font-medium">Examples</summary>
              <div class="absolute z-10 mt-2 right-0 bg-white border border-gray-200 rounded-md shadow-lg p-3 w-80">
                <div class="space-y-2">
                  <div 
                    @click="setExamplePrompt('Show me all error logs from the past hour')"
                    class="cursor-pointer p-2 hover:bg-gray-50 rounded text-sm border border-gray-100"
                  >
                    Show me all error logs from the past hour
                  </div>
                  <div 
                    @click="setExamplePrompt('Count log entries by level for today')"
                    class="cursor-pointer p-2 hover:bg-gray-50 rounded text-sm border border-gray-100"
                  >
                    Count log entries by level for today
                  </div>
                  <div 
                    @click="setExamplePrompt('Find logs containing authentication failed in the past 24 hours')"
                    class="cursor-pointer p-2 hover:bg-gray-50 rounded text-sm border border-gray-100"
                  >
                    Find logs containing "authentication failed" in the past 24 hours
                  </div>
                  <div 
                    @click="setExamplePrompt('Show top 10 most frequent error messages this week')"
                    class="cursor-pointer p-2 hover:bg-gray-50 rounded text-sm border border-gray-100"
                  >
                    Show top 10 most frequent error messages this week
                  </div>
                </div>
              </div>
            </details>
          </div>
        </div>

        <!-- Generated SQL Section -->
        <div class="space-y-3">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-700">Generated SQL</span>
            <div v-if="isGeneratingAi" class="flex items-center gap-2 text-xs text-gray-500">
              <div class="w-3 h-3 border-2 border-gray-300 border-t-purple-500 rounded-full animate-spin"></div>
              Generating...
            </div>
          </div>
          
          <!-- SQL Preview Container -->
          <div class="bg-gray-50 border-2 border-gray-300 rounded-md overflow-hidden shadow-sm">
            <!-- Loading State -->
            <div v-if="isGeneratingAi" class="p-4 space-y-2">
              <div class="h-4 bg-gray-200 rounded animate-pulse"></div>
              <div class="h-4 bg-gray-200 rounded animate-pulse w-3/4"></div>
              <div class="h-4 bg-gray-200 rounded animate-pulse w-1/2"></div>
            </div>
            
            <!-- Empty State -->
            <div v-else-if="!generatedSql && !aiError" class="p-8 text-center text-gray-400">
              <Wand2 class="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p class="text-sm">Your generated SQL will appear here</p>
            </div>
            
            <!-- Generated SQL Display -->
            <div v-else-if="generatedSql" class="relative">
              <pre class="p-4 text-sm font-mono text-gray-800 overflow-auto max-h-60 whitespace-pre-wrap leading-relaxed"><code>{{ generatedSql }}</code></pre>
              <Button 
                variant="ghost" 
                size="sm" 
                class="absolute top-2 right-2 h-6 w-6 p-0"
                @click="copyToClipboard(generatedSql)"
                title="Copy to clipboard"
              >
                <div class="h-3 w-3">📋</div>
              </Button>
            </div>
            
            <!-- Error State -->
            <div v-else-if="aiError" class="p-4 text-sm text-red-700 bg-red-50 border-l-4 border-red-400">
              <div class="flex items-start gap-2">
                <AlertCircle class="h-4 w-4 flex-shrink-0 mt-0.5" />
                <div>
                  <div class="font-medium">Generation Failed</div>
                  <div class="text-xs mt-1 text-red-600">{{ aiError }}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer Actions -->
      <DialogFooter class="border-t pt-4 flex justify-between items-center">
        <Button variant="outline" @click="resetAiDialog">
          Cancel
        </Button>
        
        <div class="flex gap-2">
          <Button 
            variant="outline"
            @click="handleAiSubmit" 
            :disabled="!aiNaturalQuery.trim() || isGeneratingAi"
            class="border-purple-200 text-purple-700 hover:bg-purple-50"
          >
            <Wand2 v-if="!isGeneratingAi" class="h-4 w-4 mr-2" />
            <div v-if="isGeneratingAi" class="w-4 h-4 border-2 border-purple-300 border-t-purple-600 rounded-full animate-spin mr-2"></div>
            {{ isGeneratingAi ? 'Generating...' : (generatedSql ? 'Regenerate' : 'Generate SQL') }}
          </Button>
          
          <Button 
            @click="insertGeneratedSql" 
            :disabled="!generatedSql || isGeneratingAi"
            class="bg-purple-600 hover:bg-purple-700 text-white font-medium"
          >
            Insert into Editor
          </Button>
        </div>
      </DialogFooter>
    </DialogContent>
  </Dialog>


</template>

<script setup lang="ts">
import {
  ref,
  computed,
  shallowRef,
  watch,
  onMounted,
  onBeforeUnmount,
  onActivated,
  onDeactivated,
  nextTick,
  reactive,
  watchEffect,
} from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import {
  HelpCircle,
  PanelRightOpen,
  PanelRightClose,
  AlertCircle,
  XCircle,
  FileEdit,
  FilePlus2,
  Search,
  Code2,
  Eye,
  EyeOff,
  Wand2,
  History,
  Play,
  RefreshCw,
  Keyboard,
  Square,
} from "lucide-vue-next";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import SavedQueriesDropdown from "@/components/collections/SavedQueriesDropdown.vue";
import QueryHistoryDropdown from "./QueryHistoryDropdown.vue";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Settings } from "lucide-vue-next";

import {
  initMonacoSetup,
  getDefaultMonacoOptions,
  getSingleLineModeOptions,
  getOrCreateModel,
  registerEditorInstance,
  unregisterEditorInstance,
  lightweightEditorDisposal,
  reactivateEditor,
} from "@/utils/monaco";
import { logchefqlApi, type ValidateResponse } from "@/api/logchefql";

// Simple parser states for autocomplete (lightweight, no backend dependency)
enum AutocompleteState {
  INITIAL = 'INITIAL',
  KEY = 'KEY',
  OPERATOR = 'OPERATOR',
  VALUE = 'VALUE',
  EXPECT_BOOL_OP = 'EXPECT_BOOL_OP',
}

// Simple function to detect autocomplete state from text
function detectAutocompleteState(text: string): { state: AutocompleteState; key: string; value: string } {
  const trimmed = text.trim();
  if (!trimmed) return { state: AutocompleteState.INITIAL, key: '', value: '' };
  
  // Check if we're after a boolean operator
  if (/\b(and|or)\s*$/i.test(trimmed)) {
    return { state: AutocompleteState.KEY, key: '', value: '' };
  }
  
  // Check if we have a complete condition and need a boolean operator
  const conditionPattern = /(\w+)\s*([=!~><]+)\s*("[^"]*"|'[^']*'|\S+)\s*$/;
  const conditionMatch = trimmed.match(conditionPattern);
  if (conditionMatch) {
    return { state: AutocompleteState.EXPECT_BOOL_OP, key: conditionMatch[1], value: conditionMatch[3] };
  }
  
  // Check if we have key + operator, expecting value
  const keyOpPattern = /(\w+)\s*([=!~><]+)\s*$/;
  const keyOpMatch = trimmed.match(keyOpPattern);
  if (keyOpMatch) {
    return { state: AutocompleteState.VALUE, key: keyOpMatch[1], value: '' };
  }
  
  // Check if we're typing a key
  const keyPattern = /(\w+)\s*$/;
  const keyMatch = trimmed.match(keyPattern);
  if (keyMatch) {
    // Check if this looks like a partial value (after operator)
    if (/[=!~><]\s*\S*$/.test(trimmed)) {
      const fullMatch = trimmed.match(/(\w+)\s*[=!~><]+\s*(\S*)$/);
      return { state: AutocompleteState.VALUE, key: fullMatch?.[1] || '', value: fullMatch?.[2] || '' };
    }
    return { state: AutocompleteState.KEY, key: keyMatch[1], value: '' };
  }
  
  return { state: AutocompleteState.INITIAL, key: '', value: '' };
}
import {
  validateSQLWithDetails,
  SQL_KEYWORDS,
  CLICKHOUSE_FUNCTIONS,
  SQL_TYPES,
} from "@/utils/clickhouse-sql";
import { storeToRefs } from 'pinia';
import { useExploreStore } from "@/stores/explore";
import { useTeamsStore } from "@/stores/teams";
import type { VariableState as VariableSetting } from '@/stores/variables';
import { useVariableStore } from '@/stores/variables';
import { QueryService } from "@/services/QueryService";
import { useVariables } from "@/composables/useVariables.ts";
// Keep other necessary imports like types...
// --- Types ---
type EditorMode = "logchefql" | "clickhouse-sql";
type EditorChangeEvent = {
  query: string;
  mode: EditorMode;
  isUserInput?: boolean;
};
// Monaco type aliases for clarity
type MonacoEditor = monaco.editor.IStandaloneCodeEditor;
type MonacoModel = monaco.editor.ITextModel;
type MonacoDisposable = monaco.IDisposable;
type MonacoCompletionItem = monaco.languages.CompletionItem;
type MonacoPosition = monaco.Position;
type MonacoRange = monaco.IRange;

// --- Props and Emits ---
const props = defineProps({
  sourceId: { type: Number, required: true },
  schema: {
    type: Object as () => Record<string, { type: string }>,
    required: true,
  },
  activeMode: { type: String as () => EditorMode, required: true },
  value: { type: String, default: "" },
  placeholder: { type: String, default: "" },
  tsField: { type: String, default: "timestamp" },
  tableName: { type: String, required: true },
  showFieldsPanel: { type: Boolean, default: false },
  // SavedQueriesDropdown props
  teamId: { type: Number, required: true },
  useCurrentTeam: { type: Boolean, default: true },
  // Additional props to prevent Vue warnings
  class: { type: String, default: "" },
  // Run button integration
  isExecuting: { type: Boolean, default: false },
  canExecute: { type: Boolean, default: true },
  showRunButton: { type: Boolean, default: true },
  // Cancel button integration
  isCancelling: { type: Boolean, default: false },
});

const emit = defineEmits<{
  (e: "change", value: EditorChangeEvent): void;
  (e: "submit", value: EditorChangeEvent): void;
  (e: "update:activeMode", value: EditorMode, isModeSwitchOnly?: boolean): void;
  (e: "toggle-fields"): void;
  // SavedQueries events
  (e: "select-saved-query", query: SavedTeamQuery): void;
  (e: "save-query"): void;
  // Additional emits to prevent Vue warnings
  (e: "saveQueryAsNew"): void;
  (e: "generateAiSql", payload: any): void;
  // Run button
  (e: "execute"): void;
  // Cancel button
  (e: "cancel-query"): void;
}>();

// --- Core State ---
const isDark = useDark();
const exploreStore = useExploreStore();
// Access variable store
const variableStore = useVariableStore();

const editorRef = shallowRef<MonacoEditor | null>(null);
const editorContent = ref(props.value || ""); // Initialize with prop value
const editorFocused = ref(false);
const validationError = ref<string | null>(null);
const isProgrammaticChange = ref(false); // Flag to prevent update loops
const isDisposing = ref(false); // Flag to prevent operations during disposal
const activeDisposables = ref<MonacoDisposable[]>([]); // Track all disposables
const isEditorVisible = ref(true); // New state for SQL editor visibility

// dynamic variables list
const { allVariables } = storeToRefs(variableStore);
// Selected variable for sheet editing
const selectedVariable = ref<VariableSetting | null>(null);
// Show variables configuration panel
const showVariablesConfig = ref(false);

// AI SQL generation state
const showAiDialog = ref(false);
const aiNaturalQuery = ref('');

// Get AI state from store
const isGeneratingAi = computed(() => exploreStore.isGeneratingAISQL);
const aiError = computed(() => exploreStore.aiSqlError);
const generatedSql = computed(() => exploreStore.generatedAiSql);

// AI textarea ref for auto-focus
const aiTextareaRef = ref<HTMLTextAreaElement | null>(null);

// Auto-focus when dialog opens
watch(showAiDialog, (isOpen) => {
  if (isOpen) {
    nextTick(() => {
      aiTextareaRef.value?.focus();
    });
  }
});

// --- Computed Properties ---
const theme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));
const fieldNames = computed(() => Object.keys(props.schema ?? {}));
const isEditorEmpty = computed(() => !editorContent.value?.trim());

const currentPlaceholder = computed(() => {
  // Use prop placeholder if provided, otherwise generate default
  if (props.placeholder) return props.placeholder;

  return props.activeMode === "logchefql"
    ? 'Enter search criteria (e.g., lvl="ERROR" and namespace~"sys")'
    : `Enter ClickHouse SQL query (e.g., SELECT * FROM ${props.tableName || "your_table"} WHERE ...)`;
});

const editorHeight = computed(() => {
  const content = editorContent.value || "";
  const lines = (content.match(/\n/g) || []).length + 1;
  const baseLineHeight = 21; // Match monaco options
  const padding = 16; // Match monaco options (top + bottom)
  const minHeight = props.activeMode === "logchefql" ? 45 : 90;
  // Calculate height based on lines, ensuring minHeight is respected
  // Add a small buffer for better spacing, especially for single line
  const calculatedHeight =
    padding + lines * baseLineHeight + (lines > 1 ? 0 : 4);
  return Math.max(minHeight, calculatedHeight);
});

// Reactive Monaco options (base + dynamic updates)
const monacoOptions = reactive(getDefaultMonacoOptions());

// --- Editor Lifecycle & Handling ---
const handleMount = (editor: MonacoEditor) => {
  if (isDisposing.value) return; // Prevent setup if disposing

  console.log("QueryEditor: Mounting editor");
  editorRef.value = editor;
  initMonacoSetup(); // Ensure themes/languages are registered (runs once internally)

  // Register editor with global management system
  registerEditorInstance(editor);

  // Create a unique model key for this editor instance
  const modelKey = `${props.activeMode}-${props.sourceId}`;
  console.log(`Creating/reusing model with key: ${modelKey}`);

  try {
    // Use cached model if available with sourceId as identifier
    const model = getOrCreateModel(
      editorContent.value,
      props.activeMode,
      props.sourceId,
      modelKey
    );

    // Set the model to the editor
    editor.setModel(model);
    console.log(`Model set successfully: ${model.id}`);
  } catch (e) {
    console.error("Error setting up editor model:", e);
    // Create a fallback model directly if cache retrieval fails
    const fallbackModel = monaco.editor.createModel(
      editorContent.value,
      props.activeMode
    );
    editor.setModel(fallbackModel);
  }

  // Check if we should be in read-only mode
  const isLoading = exploreStore.isLoadingOperation("executeQuery");
  if (isLoading) {
    editor.updateOptions({ readOnly: true });
  }

  // Add Focus/Blur Listeners
  activeDisposables.value.push(
    editor.onDidFocusEditorWidget(() => (editorFocused.value = true)),
    editor.onDidBlurEditorWidget(() => (editorFocused.value = false))
  );

  // Add Submit Shortcut
  activeDisposables.value.push(
    editor.addAction({
      id: "submit-query",
      label: "Run Query",
      keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
      run: submitQuery,
    })
  );

  // Add minimal keyboard event listener that only prevents new lines in LogchefQL mode
  const domNode = editor.getDomNode();
  if (domNode) {
    const keyHandler = (e: KeyboardEvent) => {
      // Only prevent default when:
      // 1. It's a bare Enter keypress (no modifier keys)
      // 2. We're in LogchefQL mode
      // 3. No suggestion widget is visible (to allow autocomplete acceptance)
      if (
        e.key === "Enter" &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey &&
        props.activeMode === "logchefql"
      ) {
        // Check if suggestion widget is visible by looking at DOM
        const suggestWidgetVisible =
          document.querySelector(".suggest-widget.visible") !== null;

        // Only prevent new line if no suggestion widget is visible
        if (!suggestWidgetVisible) {
          e.preventDefault();
          // Don't execute query, just prevent the newline
        }
      }
    };

    domNode.addEventListener("keydown", keyHandler);

    // Add disposal for the event listener
    activeDisposables.value.push({
      dispose: () => {
        domNode.removeEventListener("keydown", keyHandler);
      },
    });
  }

  // Initial setup is handled by watchEffect which runs immediately

  // Attempt initial focus
  focusEditor(true);
};

// Handle editor changes (e.g., user typing query with {{variable}})
const handleEditorChange = (value: string | undefined) => {
  if (isProgrammaticChange.value || isDisposing.value) {
    return;
  }

  const currentQuery = value ?? "";
  editorContent.value = currentQuery;

  if (props.activeMode === "logchefql") {
    exploreStore.setLogchefqlCode(currentQuery);
  } else {
    exploreStore.setRawSql(currentQuery);
  }

  validationError.value = null;

  detectVariables(currentQuery); // detect dynamic variables and make variable list in dom

  emit("change", {
    query: currentQuery,
    mode: props.activeMode,
    isUserInput: true,
  });
};

// Function to programmatically update editor content (e.g., from store or clear)
const runProgrammaticUpdate = (newValue: string) => {
  if (!editorRef.value || isDisposing.value) return;

  isProgrammaticChange.value = true;
  editorContent.value = newValue; // Update internal state
  editorRef.value.setValue(newValue); // Update Monaco instance

  detectVariables(newValue); // detect dynamic variables and make variable list in dom

  // Release the flag after the update is likely processed
  nextTick(() => {
    isProgrammaticChange.value = false;
    // Emit change event marked as NOT user input (programmatic update)
    emit("change", {
      query: newValue,
      mode: props.activeMode,
      isUserInput: false,
    });
  });
};

const detectVariables = (value: string) => {
  if (typeof value !== 'string') return;

  // Extract dynamic variable names from query - handle both {{variable}} and __VAR_variable__ formats
  const bracketMatches = [...value.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]);
  const underscoreMatches = [...value.matchAll(/__VAR_(\w+)__/g)].map(m => m[1]);

  // Combine both formats and get unique variable names
  const allMatches = [...bracketMatches, ...underscoreMatches];
  const uniqueVariableNames = [...new Set(allMatches)];

  // If allVariables is not ready, just upsert all found variables
  const currentVariables = allVariables?.value ?? [];

  const existingNames = currentVariables.map(v => v.name);

  // Remove variables that are no longer in the query
  for (const variable of currentVariables) {
    if (!uniqueVariableNames.includes(variable.name)) {
      variableStore.removeVariable(variable.name);
    }
  }

  // Add new variables that appeared in query
  for (const name of uniqueVariableNames) {
    if (!existingNames.includes(name)) {
      variableStore.upsertVariable({
        name,
        type: 'text',
        label: name,
        inputType: 'input',
        value: ''
      });
    }
  }
};

// Watch for prop value changes to update editor content
watch(
  () => props.value,
  (newValue) => {
    if (newValue !== editorContent.value) {
      runProgrammaticUpdate(newValue || "");
    }
  }
);

// Watch for editor content changes to detect variables immediately
watch(
  () => editorContent.value,
  (newValue) => {
    if (newValue) {
      detectVariables(newValue);
    }
  },
  { immediate: true }
);

// --- Synchronization and Option Updates ---
watchEffect(() => {
  // 1. Sync content from store if it differs from internal state
  const storeValue =
    props.activeMode === "logchefql"
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql;
  const valueToSet = storeValue ?? "";

  // Save cursor position before making changes
  const editor = editorRef.value;
  let savedPosition = editor?.getPosition() || null;
  let savedSelection = editor?.getSelection() || null;
  let shouldRestoreCursor = false;

  // Force update on edge cases
  if (editorContent.value !== valueToSet) {
    runProgrammaticUpdate(valueToSet);
    shouldRestoreCursor = true;
  }

  // Detect variables whenever store content changes
  if (valueToSet) {
    detectVariables(valueToSet);
  }

  // 2. Update editor options and language if editor instance exists
  if (editor && !isDisposing.value) {
    let languageChanged = false; // Flag to track if mode changed
    const model = editor.getModel();
    if (model && model.getLanguageId() !== props.activeMode) {
      // Only change the language model - don't recreate the editor
      monaco.editor.setModelLanguage(model, props.activeMode);
      languageChanged = true; // Set flag if language was updated

      // Register completion provider only once per language
      if (!activeCompletionProvider.value || languageChanged) {
        registerCompletionProvider(); // Update syntax highlighting/completion
      }
    }

    // Get current loading state to ensure read-only flag is consistent
    const isLoading = exploreStore.isLoadingOperation("executeQuery");

    // Apply mode-specific options without recreating the editor
    const options = {
      ...getDefaultMonacoOptions(),
      fontSize: 13,
      lineHeight: 20,
      padding: { top: 8, bottom: 8 },
      readOnly: isLoading, // Ensure read-only state is correctly synchronized
      scrollbar: {
        vertical: "auto" as const,
        horizontal: "auto" as const,
        useShadows: false,
        verticalScrollbarSize: 8,
        horizontalScrollbarSize: 8,
      },
      minimap: { enabled: false },
      // Add mode-specific options for LogchefQL
      ...(props.activeMode === "logchefql"
        ? {
          ...getSingleLineModeOptions(),
        }
        : {
          // SQL-specific configuration - keep line numbers off
          lineNumbers: "off" as const,
          wordWrap: "on" as const,
          folding: true,
          scrollBeyondLastLine: false,
        }),
    };
    editor.updateOptions(options);

    // Refocus editor and move cursor *only* if the language actually changed
    if (languageChanged) {
      focusEditor(true);
    }
    // Otherwise, restore cursor position if we made content changes
    else if (shouldRestoreCursor && savedPosition) {
      nextTick(() => {
        if (editor && !isDisposing.value) {
          editor.setPosition(savedPosition);
          if (savedSelection) {
            editor.setSelection(savedSelection);
          }
          editor.revealPositionInCenterIfOutsideViewport(savedPosition);
        }
      });
    }
  }
});

// Watch for schema changes to update completion providers
let lastSchemaHash = "";
watch(
  () => props.schema,
  (newSchema) => {
    // Generate a simple hash to determine if schema has meaningfully changed
    const schemaKeys = Object.keys(newSchema || {})
      .sort()
      .join(",");
    if (schemaKeys === lastSchemaHash) {
      return; // Skip if schema hasn't actually changed
    }
    lastSchemaHash = schemaKeys;

    if (editorRef.value && !isDisposing.value) {
      // Use requestIdleCallback to defer non-critical operation
      if (window.requestIdleCallback) {
        window.requestIdleCallback(
          () => {
            if (editorRef.value && !isDisposing.value) {
              console.log(
                "QueryEditor: Schema changed, re-registering completions (deferred)."
              );
              registerCompletionProvider();
            }
          },
          { timeout: 1000 }
        );
      } else {
        // Fallback for browsers without requestIdleCallback
        setTimeout(() => {
          if (editorRef.value && !isDisposing.value) {
            console.log(
              "QueryEditor: Schema changed, re-registering completions (timeout)."
            );
            registerCompletionProvider();
          }
        }, 100);
      }
    }
  },
  { deep: true, flush: "post" }
);

// Watch for loading state to make editor read-only
watch(
  () => exploreStore.isLoadingOperation("executeQuery"),
  (isLoading) => {
    if (editorRef.value && !isDisposing.value) {
      // Set read-only state when query is executing
      editorRef.value.updateOptions({ readOnly: isLoading });

      // If loading is complete but editor still appears to be read-only,
      // force a refresh of the editor options
      if (!isLoading) {
        setTimeout(() => {
          if (editorRef.value && !isDisposing.value) {
            // Ensure read-only is properly reset
            editorRef.value.updateOptions({ readOnly: false });
          }
        }, 50);
      }
    }
  }
);

// Add this watch effect after the existing watchEffect to position cursor when a saved query is loaded
watch(
  () => exploreStore.selectedQueryId,
  (newQueryId, oldQueryId) => {
    if (newQueryId && newQueryId !== oldQueryId) {
      // When a new saved query is loaded, position cursor at the end of content and focus editor
      console.log("QueryEditor: Saved query loaded, positioning cursor at end");
      nextTick(() => {
        // Add a slight delay to ensure content is fully updated
        setTimeout(() => {
          focusEditor(true); // true = position cursor at end
        }, 100);
      });
    }
  }
);

// --- Monaco Options Update ---
watch(
  () => props.activeMode,
  (newMode) => {
    nextTick(() => {
      if (editorRef.value) {
        // Apply options based on mode
        const editor = editorRef.value;
        const options = {
          ...getDefaultMonacoOptions(),
          fontSize: 13,
          lineHeight: 20,
          padding: { top: 8, bottom: 8 },
          scrollbar: {
            vertical: "auto" as const,
            horizontal: "auto" as const,
            useShadows: false,
            verticalScrollbarSize: 8,
            horizontalScrollbarSize: 8,
          },
          minimap: { enabled: false },
        };

        editor.updateOptions(options);
        editor.layout();
      }
    });
  }
);

// --- Completion Providers ---
const activeCompletionProvider = ref<MonacoDisposable | null>(null);

const registerCompletionProvider = () => {
  // Save cursor position before making changes
  const editor = editorRef.value;
  let savedPosition = editor?.getPosition() || null;
  let savedSelection = editor?.getSelection() || null;

  // Dispose previous provider if it exists
  if (activeCompletionProvider.value) {
    activeCompletionProvider.value.dispose();
    activeCompletionProvider.value = null;
  }

  let provider: MonacoDisposable | null = null;
  if (props.activeMode === "logchefql") {
    provider = registerLogchefQLCompletionProvider();
  } else {
    provider = registerSQLCompletionProvider();
  }

  if (provider) {
    activeCompletionProvider.value = provider;
    // Note: We don't add this to activeDisposables because we manage it separately
    // to avoid disposing it with general listeners on unmount if not needed.
    // It will be disposed when the mode changes or on final unmount.
  }

  // Restore cursor position and selection after completion provider is registered
  nextTick(() => {
    if (editor && savedPosition && !isDisposing.value) {
      editor.setPosition(savedPosition);
      if (savedSelection) {
        editor.setSelection(savedSelection);
      }
    }
  });
};

// --- Actions ---
// Replace dynamic variables in query before submit
const submitQuery = async () => {
  const currentContent = editorContent.value;
  validationError.value = null;

  // For validation, replace variables with placeholders instead of actual values
  let queryForValidation = currentContent;
  if (props.activeMode === "logchefql") {
    // For LogchefQL, replace with placeholder values that will parse correctly
    queryForValidation = currentContent.replace(/{{(\w+)}}/g, '"placeholder"');
  } else {
    // For SQL, use the converted variables
    const { convertVariables } = useVariables();
    queryForValidation = convertVariables(currentContent);
  }

  try {
    let isValid = true;

    if (queryForValidation.trim()) {
      if (props.activeMode === "logchefql") {
        // Use backend validation API
        try {
          const teamsStore = useTeamsStore();
          const currentTeamId = teamsStore.currentTeamId;
          if (currentTeamId && props.sourceId) {
            const response = await logchefqlApi.validate(currentTeamId, props.sourceId, queryForValidation);
            if (response.data) {
              isValid = response.data.valid;
              if (!isValid) validationError.value = response.data.error?.message || "Invalid LogchefQL syntax.";
            }
          }
        } catch (validationErr) {
          // If validation API fails, allow the query to proceed (fail-open)
          console.warn("LogchefQL validation API error:", validationErr);
          isValid = true;
        }
      } else {
        const validation = validateSQLWithDetails(queryForValidation);
        isValid = validation.valid;
        console.log("Invalid SQL syntax. : " + isValid);
        if (!isValid) validationError.value = validation.error || "Invalid SQL syntax.";
      }
    }

    if (!isValid) return;

    if (props.activeMode === "logchefql") {
      if (exploreStore.logchefqlCode !== currentContent) exploreStore.setLogchefqlCode(currentContent);
    } else {
      if (exploreStore.rawSql !== currentContent) exploreStore.setRawSql(currentContent);
    }

    emit("submit", {
      query: currentContent,
      mode: props.activeMode,
      isUserInput: true,
    });
  } catch (e: any) {
    console.error("Error validating or submitting query:", e);
    validationError.value = e.message || "Error preparing query";
  }
};

const focusEditor = (revealLastPosition = false) => {
  nextTick(() => {
    // Add a slight delay to allow Monaco to fully process updates after :key change / options update
    setTimeout(() => {
      const editor = editorRef.value;
      if (
        editor &&
        !isDisposing.value &&
        document.contains(editor.getDomNode())
      ) {
        console.log("QueryEditor: Attempting focus...");
        editor.focus();

        // Explicitly set position and reveal to ensure cursor visibility and placement
        const model = editor.getModel();
        if (model) {
          let positionToSet: monaco.Position | null = null;
          if (revealLastPosition) {
            const lineCount = model.getLineCount();
            const lastCol = model.getLineMaxColumn(lineCount);
            positionToSet = new monaco.Position(lineCount, lastCol);
          } else {
            // Try to keep current position if not revealing last
            positionToSet = editor.getPosition();
          }

          if (positionToSet) {
            try {
              editor.setPosition(positionToSet);
              // Use revealPositionInCenterIfOutsideViewport for less jarring scroll
              editor.revealPositionInCenterIfOutsideViewport(
                positionToSet,
                monaco.editor.ScrollType.Smooth
              );
              console.log(
                `QueryEditor: Focused and position set to ${positionToSet.lineNumber}:${positionToSet.column}`
              );
            } catch (e) {
              console.warn("QueryEditor: Error setting position/revealing:", e);
            }
          } else {
            console.log(
              "QueryEditor: Focused, but could not determine position."
            );
          }
        } else {
          console.log(
            "QueryEditor: Focused (model not available for positioning)."
          );
        }
      } else {
        console.log(
          "QueryEditor: Focus skipped (editor not ready, disposing, or detached)."
        );
      }
    }, 50); // Small delay to allow Monaco to settle
  });
};

// --- Disposal ---
const safelyDisposeEditor = (fullDisposal = false) => {
  if (isDisposing.value) return;
  isDisposing.value = true;

  // If this is a lightweight disposal (deactivation), use our optimized method
  if (!fullDisposal) {
    const editor = editorRef.value;
    if (editor) {
      console.log(
        "QueryEditor: Performing lightweight disposal (deactivation)"
      );
      lightweightEditorDisposal(editor);

      // Dispose any active disposables except the editor itself
      [...activeDisposables.value].forEach((disposable) => {
        try {
          disposable.dispose();
        } catch (e) {
          console.warn("Error disposing resource:", e);
        }
      });
      activeDisposables.value = [];

      // Release the flag after lightweight disposal
      setTimeout(() => {
        isDisposing.value = false;
      }, 50);
    }
    return;
  }

  // Full disposal for unmount
  console.log("QueryEditor: Starting full disposal...");

  // Dispose the active completion provider
  if (activeCompletionProvider.value) {
    try {
      activeCompletionProvider.value.dispose();
    } catch (e) {
      console.warn("Error disposing completion provider:", e);
    }
    activeCompletionProvider.value = null;
  }

  // Dispose general listeners and actions
  [...activeDisposables.value].forEach((disposable) => {
    try {
      disposable.dispose();
    } catch (e) {
      console.warn("Error disposing resource:", e);
    }
  });
  activeDisposables.value = [];

  const editor = editorRef.value;
  if (editor) {
    // Unregister from global tracking
    unregisterEditorInstance(editor);

    // The model is now handled by the global cache, so we don't need to dispose it
    // Just detach it from this editor instance
    editor.setModel(null);

    // Don't dispose the editor immediately to avoid race conditions
    setTimeout(() => {
      if (editor) {
        try {
          editor.dispose();
          console.log("QueryEditor: Editor instance fully disposed.");
        } catch (e) {
          console.error("Error during editor instance disposal:", e);
        }
      }
    }, 50);
  }

  editorRef.value = null; // Clear ref
};

// Global escape key handler for query cancellation
const handleEscapeKey = (e: KeyboardEvent) => {
  if (e.key === 'Escape' && props.isExecuting && !props.isCancelling) {
    e.preventDefault();
    emit('cancel-query');
  }
};

onMounted(() => {
  // Add escape key listener for query cancellation
  document.addEventListener('keydown', handleEscapeKey);
});

// Handle full disposal on unmount
onBeforeUnmount(() => {
  // Remove escape key listener
  document.removeEventListener('keydown', handleEscapeKey);
  safelyDisposeEditor(true); // Full disposal
});

// Handle lightweight disposal on deactivation (when kept alive)
onDeactivated(() => {
  console.log("QueryEditor: Deactivated");
  if (editorRef.value && !isDisposing.value) {
    safelyDisposeEditor(false); // Lightweight disposal
  }
});

// Handle reactivation
onActivated(() => {
  console.log("QueryEditor: Component activated");
  if (editorRef.value) {
    // Use enhanced reactivation that properly handles models
    reactivateEditor(
      editorRef.value,
      props.activeMode,
      editorContent.value,
      props.sourceId
    );

    // After reactivation, refresh the editor state
    nextTick(() => {
      if (editorRef.value) {
        // Force a layout calculation
        editorRef.value.layout();

        // Try to focus the editor
        focusEditor(false); // Don't move cursor to end
        console.log("QueryEditor: Editor reactivated and focused");
      }
    });
  } else {
    // If there's no editor reference, the component might be mounting for the first time
    console.log(
      "QueryEditor: No editor ref available on activation, will be created during mount"
    );
  }
});

// Toggle SQL editor visibility
const toggleSqlEditorVisibility = () => {
  isEditorVisible.value = !isEditorVisible.value;

  // If we're showing the editor again, focus it after a brief delay
  if (isEditorVisible.value) {
    nextTick(() => {
      setTimeout(() => {
        focusEditor(false);
      }, 50);
    });
  }
};

// Watch for mode changes to reset visibility
watch(
  () => props.activeMode,
  (newMode) => {
    // Always show editor when switching modes
    if (newMode === "logchefql") {
      isEditorVisible.value = true;
    }
  }
);

// --- Expose ---
defineExpose({
  submitQuery,
  // clearEditor is now handled by the parent
  focus: focusEditor, // Expose focus method
  code: computed(() => editorContent.value),
  toggleSqlEditorVisibility, // Expose toggle method
});

// --- Completion Provider Logic (Simplified stubs - keep your detailed logic) ---
// Assume getSuggestionsFromList, getSchemaFieldValues, prepareSuggestionValues remain similar

const registerLogchefQLCompletionProvider = (): MonacoDisposable | null => {
  return monaco.languages.registerCompletionItemProvider("logchefql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);

      // Save cursor position for reliable restoration
      const currentPosition = position.clone();

      const range: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn,
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);

      // Use simplified autocomplete state detection
      const parserState = detectAutocompleteState(textBeforeCursor);

      let suggestions: MonacoCompletionItem[] = [];
      let incomplete = false;

      // Handle "and" operator specifically
      if (textBeforeCursor.trim().endsWith("and")) {
        return { suggestions: [], incomplete: false };
      }

      if (
        parserState.state === AutocompleteState.KEY ||
        parserState.state === AutocompleteState.INITIAL
      ) {
        if (fieldNames.value.includes(wordInfo.word)) {
          const operatorRange: MonacoRange = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: position.column,
            endColumn: position.column,
          };
          suggestions = getOperatorsSuggestions(wordInfo.word, operatorRange);
        } else {
          suggestions = getKeySuggestions(range);
        }
      } else if (parserState.state === AutocompleteState.EXPECT_BOOL_OP) {
        suggestions = getBooleanOperatorsSuggestions(range);
      } else if (parserState.state === AutocompleteState.VALUE || parserState.state === AutocompleteState.OPERATOR) {
        const valueRange: MonacoRange = {
          startLineNumber: position.lineNumber,
          endLineNumber: position.lineNumber,
          startColumn: wordInfo.startColumn,
          endColumn: wordInfo.endColumn,
        };
        // Standard value suggestions
        const valueResult = await getValueSuggestions(
          parserState.key,
          parserState.value,
          valueRange
        );
        suggestions = valueResult.suggestions;
        incomplete = valueResult.incomplete;
      }

      return { suggestions, incomplete };
    },
    // Control trigger characters more precisely
    triggerCharacters: ["=", "!", ">", "<", "~", " ", ".", '"', "'", "("],
  });
};

const registerSQLCompletionProvider = (): MonacoDisposable | null => {
  return monaco.languages.registerCompletionItemProvider("clickhouse-sql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn,
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);
      let suggestions: MonacoCompletionItem[] = [];

      // Add table name suggestion after FROM
      if (/\bFROM\s+$/i.test(textBeforeCursor) && props.tableName) {
        suggestions.push({
          label: props.tableName,
          kind: monaco.languages.CompletionItemKind.Folder,
          insertText: props.tableName,
          range: range,
          detail: "Current log table",
        });
      }

      // Always add field name suggestions for SQL queries
      if (fieldNames.value.length > 0) {
        suggestions = suggestions.concat(
          fieldNames.value.map((field) => ({
            label: field,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field,
            range: range,
            detail: props.schema[field]?.type || "unknown",
          }))
        );
      }

      const typedPrefix = wordInfo.word.toUpperCase();
      suggestions = suggestions.concat(
        getSuggestionsFromList({
          items: SQL_KEYWORDS.filter((kw) => kw.startsWith(typedPrefix)).map(
            (kw) => ({ label: kw, insertText: kw + " " })
          ),
          kind: monaco.languages.CompletionItemKind.Keyword,
          range: range,
        })
      );

      return { suggestions };
    },
    // Include more trigger characters for better responsiveness
    triggerCharacters: [" ", "\n", ".", "(", ",", "=", ">", "<", "!"],
  });
};

// --- Helper functions for completions (Keep your existing logic) ---
const getSuggestionsFromList = (params: {
  items: any[];
  kind: monaco.languages.CompletionItemKind;
  range: MonacoRange;
  postfix?: string;
}): MonacoCompletionItem[] => {
  const suggestions: MonacoCompletionItem[] = [];
  const defaultPostfix = params.postfix ?? "";
  const range = params.range;

  // Validate range
  if (
    !range ||
    typeof range.startLineNumber !== "number" ||
    typeof range.startColumn !== "number" ||
    typeof range.endLineNumber !== "number" ||
    typeof range.endColumn !== "number"
  ) {
    console.error("Invalid range provided to getSuggestionsFromList:", range);
    return [];
  }

  for (const item of params.items) {
    let label: string;
    let insertText: string;
    let sortText: string;
    let documentation: string | undefined;

    if (typeof item === "string") {
      label = item;
      insertText = item;
      sortText = item.toLowerCase();
    } else {
      label = item.label;
      insertText = item.insertText ?? label;
      sortText = item.sortText ?? label.toLowerCase();
      documentation = item.documentation;
    }

    suggestions.push({
      label,
      kind: params.kind,
      insertText: insertText + defaultPostfix,
      range: range,
      sortText,
      documentation,
      command: { id: "editor.action.triggerSuggest", title: "Trigger Suggest" },
      detail: documentation ? " " : undefined,
      insertTextRules:
        monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
    });
  }
  return suggestions;
};

const getKeySuggestions = (range: MonacoRange): MonacoCompletionItem[] => {
  return fieldNames.value.map((name) => ({
    label: name,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: name,
    range: range,
    detail: props.schema[name]?.type || "Unknown",
    sortText: `0 - ${name} `,
    command: { id: "editor.action.triggerSuggest", title: "Trigger Suggest" },
  }));
};

const getBooleanOperatorsSuggestions = (
  range: MonacoRange
): MonacoCompletionItem[] => {
  return [
    {
      label: "and",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "and ",
      range: range,
      sortText: "1-and",
    },
    {
      label: "or",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "or ",
      range: range,
      sortText: "1-or",
    },
  ];
};

const getSchemaFieldValues = (field: string): string[] => {
  // Example implementation
  const sampleValues: Record<string, string[]> = {
    level: ["info", "error", "warning", "debug"],
    status: ["200", "404", "500"],
    // ... add more based on your schema/data
  };
  return sampleValues[field] || [];
};

const prepareSuggestionValues = (
  items: string[],
  quoteChar?: string
): { label: string; insertText?: string }[] => {
  // ... your existing implementation ...
  const needsQuotes = quoteChar !== undefined;
  const q = quoteChar || '"'; // Default to double quotes if needed but not specified
  return items.map((item) => {
    if (!needsQuotes && isNumeric(item)) {
      return { label: item }; // Numeric values don't need quotes unless context demands it
    } else {
      // Escape existing quotes within the value
      const escapedValue = item.replace(new RegExp(`\\${q} `, "g"), `\\${q} `);
      return {
        label: item,
        insertText: `${q}${escapedValue}${q} `,
      };
    }
  });
};

const getValueSuggestions = async (
  key: string,
  currentValue: string,
  range: MonacoRange,
  quoteChar?: string
): Promise<{ suggestions: MonacoCompletionItem[]; incomplete: boolean }> => {
  const fieldValues = getSchemaFieldValues(key);
  const filteredValues = fieldValues.filter((v) =>
    v.toLowerCase().includes(currentValue?.toLowerCase() || "")
  );
  const preparedItems = prepareSuggestionValues(filteredValues, quoteChar);

  const suggestions = getSuggestionsFromList({
    items: preparedItems,
    kind: monaco.languages.CompletionItemKind.Value,
    range: range,
    postfix: " ",
  });

  return { suggestions, incomplete: false };
};

const getOperatorsSuggestions = (
  key: string,
  range: MonacoRange
): MonacoCompletionItem[] => {
  // Organize operators in the desired order: =, !=, ~, !~, >=, <=, >, <
  const operators = [
    { label: "=", insertText: "=", sortText: "1=" },
    { label: "!=", insertText: "!=", sortText: "2!=" },
    { label: "~", insertText: "~", sortText: "3~" },
    { label: "!~", insertText: "!~", sortText: "4!~" },
    { label: ">=", insertText: ">=", sortText: "5>=" },
    { label: "<=", insertText: "<=", sortText: "6<=" },
    { label: ">", insertText: ">", sortText: "7>" },
    { label: "<", insertText: "<", sortText: "8<" },
  ];

  // Filter operators based on field type if needed
  const fieldType = props.schema[key]?.type?.toLowerCase() || "";
  const filteredOperators = operators.filter((op) => {
    // For numeric fields, only allow comparison operators
    if (fieldType === "number" || fieldType === "integer") {
      return ["=", "!=", ">", "<", ">=", "<="].includes(op.label);
    }
    // For string fields, allow all operators
    return true;
  });

  return getSuggestionsFromList({
    items: filteredOperators.map((op) => ({
      ...op,
      insertText: op.insertText + " ", // Just add a space after operator, no quote
      documentation: `${key} ${op.label} value`,
    })),
    kind: monaco.languages.CompletionItemKind.Operator,
    range: range,
    postfix: "",
  });
};

// Add type assertion function
const asEditorMode = (value: string | number): EditorMode => {
  if (value === "logchefql" || value === "clickhouse-sql") {
    return value;
  }
  console.warn(
    `Received unexpected value for editor mode: ${value}. Defaulting to logchefql.`
  );
  return "logchefql";
};

// After the imports, add route and router
const route = useRoute();
const router = useRouter();

// Add these computed properties after other computed properties
const activeSavedQueryName = computed(() => exploreStore.activeSavedQueryName);
const isEditingExistingQuery = computed(() => !!route.query.query_id);

// Handler for loading query from history
const handleLoadQueryFromHistory = (mode: 'logchefql' | 'sql', query: string) => {
  console.log('Loading query from history:', { mode, queryLength: query.length, activeMode: props.activeMode });

  // Convert history mode to editor mode
  const editorMode = mode === 'logchefql' ? 'logchefql' : 'clickhouse-sql';

  // Emit the change event with the correct structure
  emit('change', {
    query: query,
    mode: editorMode,
    isUserInput: false,
  });

  // Switch mode if needed (after content update to avoid timing issues)
  if (editorMode !== props.activeMode) {
    nextTick(() => {
      emit('update:activeMode', editorMode, true);
    });
  }
};

// Add handler for New Query button
const handleNewQueryClick = () => {
  console.log("Creating new query state...");

  // First, copy the current query parameters
  const currentQuery = { ...route.query };

  // Remove query_id from URL
  delete currentQuery.query_id;

  // Use the centralized reset function in the store
  exploreStore.resetQueryToDefaults();

  // Explicitly clear the selectedQueryId in the store
  exploreStore.setSelectedQueryId(null);

  // Explicitly clear active saved query name
  exploreStore.setActiveSavedQueryName(null);

  // Wait for the state reset to complete
  nextTick(() => {
    // Explicitly remove the query_id parameter again to ensure it's gone
    const finalQuery = { ...currentQuery };
    delete finalQuery.query_id;

    // Replace URL with new parameters in router, disabling any redirect
    // This is important to prevent the URL sync from adding back query_id
    router
      .replace({ query: finalQuery })
      .then(() => {
        // Focus the editor and position cursor at the end after URL change is complete
        setTimeout(() => {
          focusEditor(true); // true = position cursor at end
        }, 50);
      })
      .catch((err) => {
        console.error("Error updating URL:", err);
        // Still try to focus even if URL update fails
        focusEditor(true);
      });
  });
};
// Open sheet to edit all variables
const openAllVariableSettings = () => {
  showVariablesConfig.value = true;
};

// Open sheet to edit selected variable (kept for backward compatibility)
const openVariableSettings = (variable: VariableSetting) => {
  selectedVariable.value = { ...variable }; // Create a copy to avoid direct mutation
};

// Close the sheet UI
const closeDrawer = () => {
  selectedVariable.value = null;
  showVariablesConfig.value = false;
};

// Update default value based on variable type
const setDefaultValueByType = () => {
  if (!selectedVariable.value) return;

  // Update the variable in the store
  const updatedVariable = { ...selectedVariable.value };

  switch (updatedVariable.type) {
    case 'text':
      updatedVariable.value = '';
      break;
    case 'number':
      updatedVariable.value = 0;
      break;
    case 'date':
      updatedVariable.value = new Date().toISOString();
      break;
  }

  // Update both local state and store
  selectedVariable.value = updatedVariable;
  variableStore.upsertVariable(updatedVariable);
};

// Update variable type for multi-variable panel
const updateVariableType = (variable: VariableSetting) => {
  // Update default value based on new type
  switch (variable.type) {
    case 'text':
      variable.value = '';
      break;
    case 'number':
      variable.value = 0;
      break;
    case 'date':
      variable.value = new Date().toISOString();
      break;
  }

  // Update in store
  variableStore.upsertVariable(variable);
};

// Determine input type for a given variable type
const inputTypeFor = (type: string) => {
  if (type === 'number') return 'number';
  if (type === 'date') return 'datetime-local';
  return 'text';
};

// Get placeholder text for variable type
const getPlaceholderForType = (type: string) => {
  switch (type) {
    case 'number':
      return 'Enter a number...';
    case 'date':
      return 'Select date and time...';
    case 'text':
    default:
      return 'Enter text value...';
  }
};

// Format variable value for display
const formatVariableValue = (variable: VariableSetting) => {
  if (!variable.value) return '';

  switch (variable.type) {
    case 'number':
      return `Value: ${variable.value} `;
    case 'date':
      try {
        const date = new Date(variable.value);
        return `Date: ${date.toLocaleDateString()} ${date.toLocaleTimeString()} `;
      } catch {
        return `Date: ${variable.value} `;
      }
    case 'text':
    default:
      const value = String(variable.value);
      return value.length > 20 ? `"${value.substring(0, 20)}..."` : `"${value}"`;
  }
};

// AI SQL generation handler
const handleAiSubmit = async () => {
  if (!aiNaturalQuery.value.trim()) return;
  
  try {
    // Emit the generate-ai-sql event with the natural language query
    emit('generateAiSql', { 
      naturalLanguageQuery: aiNaturalQuery.value.trim(),
      currentQuery: editorContent.value || '' 
    });
  } catch (error) {
    console.error('Failed to emit generateAiSql event:', error);
  }
};

// Insert generated SQL into editor
const insertGeneratedSql = () => {
  if (!generatedSql.value) return;
  
  // Update editor content with generated SQL
  editorContent.value = generatedSql.value;
  
  // Emit the change event
  handleEditorChange(generatedSql.value);
  
  // Close dialog and reset state
  resetAiDialog();
  
  // Focus the editor after insertion
  nextTick(() => {
    focusEditor(true);
  });
};

// Reset AI dialog state
const resetAiDialog = () => {
  showAiDialog.value = false;
  aiNaturalQuery.value = '';
  // Clear store state
  exploreStore.clearAiSqlState();
};

// Set example prompt
const setExamplePrompt = (prompt: string) => {
  aiNaturalQuery.value = prompt;
  // Auto-focus the textarea after setting example
  nextTick(() => {
    aiTextareaRef.value?.focus();
  });
};

// Copy to clipboard helper
const copyToClipboard = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
  } catch (error) {
    console.error('Failed to copy to clipboard:', error);
    const { toast } = useToast();
    toast({
      title: "Copy failed",
      description: "Unable to copy to clipboard",
      variant: "destructive",
      duration: 3000,
    });
  }
};

// Watch for changes to selected variable and update the store
watch(
  () => selectedVariable.value,
  (newVariable) => {
    if (newVariable) {
      // Update the variable in the store when it changes
      variableStore.upsertVariable(newVariable);
    }
  },
  { deep: true }
);

</script>

<style scoped>
.query-editor {
  position: relative;
  height: 100%;
  width: 100%;
}

/* Wrapper to handle border-radius and overflow together */
.editor-wrapper {
  position: relative;
  border-radius: 0 0 6px 6px;
  border: 1px solid hsl(var(--border));
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  overflow: hidden;
}

.editor-wrapper:hover:not(.is-focused) {
  border-color: hsl(var(--border-hover, var(--border)));
}

/* Focus state - use box-shadow for a cleaner look that doesn't conflict */
.editor-wrapper.is-focused {
  border-color: hsl(var(--primary));
  box-shadow: 0 0 0 1px hsl(var(--primary) / 0.3);
}

.dark .editor-wrapper.is-focused {
  border-color: hsl(var(--primary));
  box-shadow: 0 0 0 1px hsl(var(--primary) / 0.4),
    0 0 0 2px hsl(var(--primary) / 0.15);
}

/* Clean editor container styling */
.editor-container {
  position: relative;
  width: 100%;
  height: 100%;
  background-color: transparent;
  /* Make transparent to let Monaco background show through */
  padding-left: 16px;
  /* Add padding to container to position cursor */
}

/* Basic placeholder implementation */
.editor-container.is-empty::before {
  content: attr(data-placeholder);
  color: hsl(var(--muted-foreground) / 0.8);
  position: absolute;
  top: 12px;
  left: 16px;
  font-size: 13px;
  pointer-events: none;
  z-index: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
    "Liberation Mono", "Courier New", monospace;
}

/* Adjust placeholder position - keep it consistent in both modes */
.editor-container.is-empty[data-mode="clickhouse-sql"]::before {
  left: 16px;
}

/* Override Monaco's internal borders if any */
:deep(.monaco-editor),
:deep(.monaco-editor .overflow-guard) {
  border: none !important;
  outline: none !important;
  background-color: transparent !important;
  /* Ensure transparency */
}

/* Style just the margin/gutter */
:deep(.monaco-editor .margin) {
  border-radius: 0 0 0 5px;
  padding-right: 0 !important;
  background-color: transparent !important;
  width: 0 !important;
}

/* Ensure consistent content position */
:deep(.monaco-editor .monaco-scrollable-element) {
  left: 0 !important;
}

/* Reset any padding/margins that might interfere */
:deep(.monaco-editor .view-line) {
  margin-left: 0 !important;
}

/* Force flex layout for tab triggers */
:deep(.TabsTrigger) {
  display: flex !important;
  align-items: center !important;
}

.flex-fix {
  display: flex;
  flex-direction: row;
  align-items: center;
  width: 100%;
  justify-content: center;
}

.flex-fix svg {
  margin-right: 6px;
}

/* Force flex layout for tab triggers */
:deep(.tab) {
  display: block !important;
}

:deep([role="tab"]) {
  padding: 6px 12px !important;
}

/* Styles for SQL preview when editor is hidden */
.sql-preview {
  border-radius: 0 0 6px 6px;
  transition: background-color 0.2s;
}

.sql-preview:hover {
  background-color: hsl(var(--muted) / 0.6);
}

.dark .sql-preview:hover {
  background-color: #1c2536;
  /* Matches the bluish dark theme hover */
}

/* Remove old drawer styles as we're using shadcn-ui Sheet component now */
</style>
