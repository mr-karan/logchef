# LogChefQL Autocomplete Integration Guide

## Overview

The LogChefQL autocomplete system provides intelligent suggestions for writing LogChefQL queries in Monaco Editor. It includes:

- **Context-aware suggestions**: Field names, operators, values, boolean operators, and pipe operators
- **Position-aware errors**: Structured error reporting with exact cursor positions
- **Field discovery**: Automatic field detection from log samples
- **Custom field support**: Add domain-specific fields for your data

## Basic Integration

### 1. Register Enhanced LogChefQL Support

```typescript
import { registerEnhancedLogChefQL } from '@/utils/logchefql/monaco-adapter';
import { initMonacoSetup } from '@/utils/monaco';

// Initialize Monaco first
initMonacoSetup();

// Register enhanced LogChefQL with autocomplete
registerEnhancedLogChefQL();
```

### 2. Using with Custom Fields

```typescript
import { registerEnhancedLogChefQL, updateLogChefQLFields } from '@/utils/logchefql/monaco-adapter';
import { FieldDiscoveryService } from '@/utils/logchefql/field-discovery';

// Create field discovery service
const fieldDiscovery = new FieldDiscoveryService();

// Discover fields from sample logs
const sampleLogs = [
  { level: 'info', service: 'api', user_id: '123', message: 'Request processed' },
  { level: 'error', service: 'db', error_code: 500, message: 'Connection failed' }
];

const fields = fieldDiscovery.discoverFieldsFromLogs(sampleLogs, sourceId);

// Register with discovered fields
registerEnhancedLogChefQL(fields);

// Or update fields later
updateLogChefQLFields(fields);
```

### 3. Create Monaco Editor with LogChefQL

```typescript
import monaco from 'monaco-editor';
import { getOrCreateModel } from '@/utils/monaco';

// Create or get cached model
const model = getOrCreateModel('level = "error"', 'logchefql', sourceId);

// Create editor
const editor = monaco.editor.create(container, {
  model,
  language: 'logchefql',
  theme: 'logchef-dark', // or 'logchef-light'
  ...getDefaultMonacoOptions(),
  ...getSingleLineModeOptions() // for single-line query inputs
});
```

## Features

### Context-Aware Autocomplete

The autocomplete system provides different suggestions based on cursor context:

- **Start of query**: Field names and parentheses
- **After field name**: Comparison operators (=, !=, ~, !~, >, <, etc.)
- **After operator**: Values (with examples when available)
- **After value**: Boolean operators (and, or) and pipe operator (|)
- **After pipe**: SELECT field names

### Field Discovery

Automatic field discovery analyzes log samples to suggest relevant fields:

```typescript
import { FieldDiscoveryService } from '@/utils/logchefql/field-discovery';

const fieldDiscovery = new FieldDiscoveryService();

// Analyze logs and cache results
const fields = fieldDiscovery.discoverFieldsFromLogs(logSamples, sourceId);

// Get cached fields (returns null if expired)
const cachedFields = fieldDiscovery.getCachedFields(sourceId);

// Add custom fields manually
fieldDiscovery.addCustomFields(sourceId, [
  {
    name: 'trace_id',
    type: 'string',
    description: 'Distributed tracing ID',
    examples: ['abc123', 'xyz789']
  }
]);
```

### Error Handling

Structured error reporting with position information:

```typescript
import { LogChefQLErrorHandler } from '@/utils/logchefql/errors';

// Get user-friendly error from parser
const parseError = {
  code: 'UNEXPECTED_END',
  message: 'Unexpected end of input',
  position: { line: 1, column: 10 }
};

const userError = LogChefQLErrorHandler.getUserFriendlyError(parseError);
// Returns: {
//   code: 'UNEXPECTED_END',
//   message: 'Unexpected end of input',
//   suggestion: 'Complete your query - it appears to be cut off'
// }
```

## Advanced Usage

### Custom Field Types

Define custom field types with rich metadata:

```typescript
const customFields: FieldInfo[] = [
  {
    name: 'request.duration_ms',
    type: 'number',
    description: 'Request duration in milliseconds',
    examples: ['150', '2500', '45']
  },
  {
    name: 'user.profile',
    type: 'object',
    description: 'User profile information object'
  },
  {
    name: 'tags',
    type: 'array',
    description: 'Log tags array',
    examples: ['["api", "production"]', '["debug", "test"]']
  }
];
```

### Hover Information

Rich hover information for LogChefQL elements:

- **Operators**: Documentation and usage examples
- **Keywords**: Boolean operator behavior
- **Fields**: Field type and description

### Integration with Vue Components

```vue
<template>
  <div ref="editorContainer" class="monaco-editor"></div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import monaco from 'monaco-editor';
import { getOrCreateModel, registerEditorInstance } from '@/utils/monaco';
import { registerEnhancedLogChefQL } from '@/utils/logchefql/monaco-adapter';

const editorContainer = ref();
let editor = null;

onMounted(() => {
  // Register enhanced autocomplete
  registerEnhancedLogChefQL();

  // Create model and editor
  const model = getOrCreateModel('', 'logchefql');
  editor = monaco.editor.create(editorContainer.value, {
    model,
    language: 'logchefql'
  });

  registerEditorInstance(editor);
});

onUnmounted(() => {
  if (editor) {
    editor.dispose();
  }
});
</script>
```

## Performance Considerations

- **Field caching**: Discovered fields are cached for 5 minutes
- **Model reuse**: Monaco models are cached and reused across navigation
- **Lazy loading**: Suggestions are computed on-demand
- **Error tolerance**: Graceful handling of malformed queries

## Troubleshooting

### No Autocomplete Suggestions

1. Ensure `registerEnhancedLogChefQL()` is called after Monaco initialization
2. Verify the editor language is set to 'logchefql'
3. Check console for any errors during registration

### Field Suggestions Missing

1. Verify custom fields are properly formatted
2. Check if field discovery service has cached data
3. Ensure field names don't contain invalid characters

### Performance Issues

1. Limit log sample size for field discovery (max 100 entries)
2. Clear field cache periodically: `fieldDiscovery.clearCache()`
3. Use appropriate debouncing for real-time field updates