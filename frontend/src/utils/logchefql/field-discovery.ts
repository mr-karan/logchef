import type { FieldInfo } from './autocomplete';

export interface LogSchema {
  fields: FieldInfo[];
  lastUpdated: number;
}

export class FieldDiscoveryService {
  private cachedFields: Map<number, LogSchema> = new Map();
  private cacheTimeout = 5 * 60 * 1000; // 5 minutes

  /**
   * Discover fields from sample log entries
   */
  public discoverFieldsFromLogs(logs: any[], sourceId: number): FieldInfo[] {
    const fieldMap = new Map<string, FieldInfo>();

    // Add common fields that should always be available
    this.addCommonFields(fieldMap);

    // Analyze log entries to discover fields
    for (const log of logs.slice(0, 100)) { // Limit to 100 samples for performance
      this.analyzeLogEntry(log, fieldMap);
    }

    const fields = Array.from(fieldMap.values());

    // Cache the discovered fields
    this.cachedFields.set(sourceId, {
      fields,
      lastUpdated: Date.now()
    });

    return fields;
  }

  /**
   * Get cached fields for a source
   */
  public getCachedFields(sourceId: number): FieldInfo[] | null {
    const cached = this.cachedFields.get(sourceId);
    if (!cached) return null;

    // Check if cache is still valid
    if (Date.now() - cached.lastUpdated > this.cacheTimeout) {
      this.cachedFields.delete(sourceId);
      return null;
    }

    return cached.fields;
  }

  /**
   * Manually add custom fields for a source
   */
  public addCustomFields(sourceId: number, customFields: FieldInfo[]): void {
    const existing = this.getCachedFields(sourceId) || this.getDefaultFields();

    // Merge with existing fields, custom fields take precedence
    const fieldMap = new Map<string, FieldInfo>();

    // Add existing fields first
    for (const field of existing) {
      fieldMap.set(field.name, field);
    }

    // Override with custom fields
    for (const field of customFields) {
      fieldMap.set(field.name, field);
    }

    this.cachedFields.set(sourceId, {
      fields: Array.from(fieldMap.values()),
      lastUpdated: Date.now()
    });
  }

  /**
   * Get default fields when no logs are available
   */
  public getDefaultFields(): FieldInfo[] {
    const fieldMap = new Map<string, FieldInfo>();
    this.addCommonFields(fieldMap);
    return Array.from(fieldMap.values());
  }

  /**
   * Clear cached fields for a source
   */
  public clearCache(sourceId?: number): void {
    if (sourceId) {
      this.cachedFields.delete(sourceId);
    } else {
      this.cachedFields.clear();
    }
  }

  private addCommonFields(fieldMap: Map<string, FieldInfo>): void {
    const commonFields: FieldInfo[] = [
      {
        name: 'timestamp',
        type: 'string',
        description: 'Log entry timestamp',
        examples: ['2024-01-01T00:00:00Z', '2024-01-01 12:00:00']
      },
      {
        name: 'level',
        type: 'string',
        description: 'Log level',
        examples: ['info', 'error', 'debug', 'warn', 'trace', 'fatal']
      },
      {
        name: 'message',
        type: 'string',
        description: 'Log message content',
        examples: ['Database connection established', 'Request processed successfully']
      },
      {
        name: 'service',
        type: 'string',
        description: 'Service name',
        examples: ['api', 'worker', 'scheduler', 'database']
      },
      {
        name: 'host',
        type: 'string',
        description: 'Hostname or server identifier',
        examples: ['web-01', 'worker-02', 'db-primary']
      },
      {
        name: 'user_id',
        type: 'string',
        description: 'User identifier',
        examples: ['user123', 'admin', '12345']
      },
      {
        name: 'request_id',
        type: 'string',
        description: 'Request trace ID',
        examples: ['req_abc123', '1234-5678-9012', 'trace-xyz789']
      },
      {
        name: 'session_id',
        type: 'string',
        description: 'Session identifier',
        examples: ['sess_abc123', 'session-1234']
      },
      {
        name: 'log_attributes',
        type: 'object',
        description: 'Nested log attributes object with custom fields'
      },
      {
        name: 'error',
        type: 'object',
        description: 'Error information object',
        examples: ['{"type": "DatabaseError", "message": "Connection failed"}']
      },
      {
        name: 'duration',
        type: 'number',
        description: 'Request or operation duration in milliseconds',
        examples: ['150', '2500', '45']
      },
      {
        name: 'status_code',
        type: 'number',
        description: 'HTTP status code',
        examples: ['200', '404', '500', '301']
      },
      {
        name: 'method',
        type: 'string',
        description: 'HTTP method',
        examples: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH']
      },
      {
        name: 'path',
        type: 'string',
        description: 'Request path or endpoint',
        examples: ['/api/users', '/health', '/auth/login']
      },
      {
        name: 'ip_address',
        type: 'string',
        description: 'IP address',
        examples: ['192.168.1.1', '10.0.0.1', '127.0.0.1']
      }
    ];

    for (const field of commonFields) {
      fieldMap.set(field.name, field);
    }
  }

  private analyzeLogEntry(log: any, fieldMap: Map<string, FieldInfo>): void {
    if (!log || typeof log !== 'object') return;

    for (const [key, value] of Object.entries(log)) {
      if (this.shouldSkipField(key)) continue;

      const fieldInfo = this.inferFieldType(key, value);

      // If field already exists, merge information
      const existing = fieldMap.get(key);
      if (existing) {
        // Add examples if not already present
        if (fieldInfo.examples && fieldInfo.examples.length > 0) {
          const existingExamples = existing.examples || [];
          const newExamples = fieldInfo.examples.filter(ex => !existingExamples.includes(ex));
          existing.examples = [...existingExamples, ...newExamples].slice(0, 5); // Keep max 5 examples
        }
      } else {
        fieldMap.set(key, fieldInfo);
      }

      // Recursively analyze nested objects
      if (fieldInfo.type === 'object' && value && typeof value === 'object') {
        this.analyzeNestedObject(key, value, fieldMap);
      }
    }
  }

  private analyzeNestedObject(parentKey: string, obj: any, fieldMap: Map<string, FieldInfo>): void {
    if (!obj || typeof obj !== 'object') return;

    for (const [key, value] of Object.entries(obj)) {
      if (this.shouldSkipField(key)) continue;

      const nestedFieldName = `${parentKey}.${key}`;
      const fieldInfo = this.inferFieldType(nestedFieldName, value);

      if (!fieldMap.has(nestedFieldName)) {
        fieldMap.set(nestedFieldName, fieldInfo);
      }

      // Don't go too deep to avoid performance issues
      if (fieldInfo.type === 'object' && value && typeof value === 'object' &&
          !parentKey.includes('.')) { // Only go one level deep for nested objects
        this.analyzeNestedObject(nestedFieldName, value, fieldMap);
      }
    }
  }

  private inferFieldType(key: string, value: any): FieldInfo {
    const fieldInfo: FieldInfo = {
      name: key,
      type: 'string' // Default type
    };

    if (value === null || value === undefined) {
      fieldInfo.type = 'string';
      fieldInfo.examples = ['null'];
      return fieldInfo;
    }

    const valueType = typeof value;

    switch (valueType) {
      case 'boolean':
        fieldInfo.type = 'boolean';
        fieldInfo.examples = ['true', 'false'];
        fieldInfo.description = `Boolean field`;
        break;

      case 'number':
        fieldInfo.type = 'number';
        fieldInfo.examples = [value.toString()];
        fieldInfo.description = `Numeric field`;
        break;

      case 'string':
        fieldInfo.type = 'string';
        fieldInfo.examples = [value];
        fieldInfo.description = this.getStringFieldDescription(key, value);
        break;

      case 'object':
        if (Array.isArray(value)) {
          fieldInfo.type = 'array';
          fieldInfo.description = `Array field`;
          if (value.length > 0) {
            fieldInfo.examples = [JSON.stringify(value.slice(0, 2))]; // Show first 2 items
          }
        } else {
          fieldInfo.type = 'object';
          fieldInfo.description = `Object field`;
          fieldInfo.examples = [JSON.stringify(value)];
        }
        break;

      default:
        fieldInfo.type = 'string';
        fieldInfo.examples = [String(value)];
    }

    return fieldInfo;
  }

  private getStringFieldDescription(key: string, value: string): string {
    const lowerKey = key.toLowerCase();
    const lowerValue = value.toLowerCase();

    // Provide context-aware descriptions
    if (lowerKey.includes('timestamp') || lowerKey.includes('time')) {
      return 'Timestamp field';
    }
    if (lowerKey.includes('level')) {
      return 'Log level field';
    }
    if (lowerKey.includes('message') || lowerKey.includes('msg')) {
      return 'Message content field';
    }
    if (lowerKey.includes('error')) {
      return 'Error information field';
    }
    if (lowerKey.includes('id')) {
      return 'Identifier field';
    }
    if (lowerKey.includes('url') || lowerKey.includes('path')) {
      return 'URL or path field';
    }
    if (lowerKey.includes('ip') || lowerKey.includes('address')) {
      return 'IP address field';
    }

    // Check value patterns
    if (/^\d{4}-\d{2}-\d{2}/.test(value)) {
      return 'Date/timestamp field';
    }
    if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(value)) {
      return 'IP address field';
    }
    if (/^https?:\/\//.test(value)) {
      return 'URL field';
    }

    return 'String field';
  }

  private shouldSkipField(key: string): boolean {
    // Skip internal or overly common fields that aren't useful for querying
    const skipFields = ['_id', '__typename', 'createdAt', 'updatedAt'];
    return skipFields.includes(key) || key.startsWith('_');
  }
}