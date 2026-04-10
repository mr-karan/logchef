import type { Source } from "@/api/sources";

export interface SourceFieldLike {
  name: string;
  type: string;
  isTimestamp?: boolean;
  isSeverity?: boolean;
}

export type SourceFieldGroupId =
  | "core"
  | "context"
  | "filterable"
  | "system"
  | "other";

export interface SourceFieldGroup<TField extends SourceFieldLike = SourceFieldLike> {
  id: SourceFieldGroupId;
  label: string;
  description: string;
  fields: TField[];
  filterableFields: TField[];
  plainFields: TField[];
}

export const MESSAGE_FIELD_ALIASES = [
  "message",
  "msg",
  "log",
  "text",
  "body",
  "content",
] as const;

const CONTEXT_FIELD_ALIASES = [
  "service",
  "app",
  "application",
  "env",
  "environment",
  "namespace",
  "kubernetesnamespace",
  "pipeline",
  "cluster",
  "host",
  "pod",
  "container",
  "method",
  "status",
  "route",
  "path",
] as const;

const VICTORIALOGS_SYSTEM_FIELDS = new Set(["stream", "streamid"]);

const GROUP_METADATA: Record<SourceFieldGroupId, { label: string; description: string }> = {
  core: {
    label: "Core Fields",
    description: "Primary timestamp, message, and severity fields for reading logs quickly.",
  },
  context: {
    label: "Context Fields",
    description: "Operational dimensions such as service, environment, host, and namespace.",
  },
  filterable: {
    label: "Filterable Fields",
    description: "Additional scalar fields that are useful for narrowing queries.",
  },
  system: {
    label: "System Fields",
    description: "Datasource-managed or internal fields that are usually noisy in the main view.",
  },
  other: {
    label: "Other Fields",
    description: "Remaining fields that are available for inspection and ad hoc exploration.",
  },
};

export function normalizeFieldName(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, "");
}

export function isPrimaryMessageField(name: string): boolean {
  return MESSAGE_FIELD_ALIASES.includes(
    normalizeFieldName(name) as (typeof MESSAGE_FIELD_ALIASES)[number],
  );
}

export function unwrapFieldType(type: string): string {
  return type
    .replace(/LowCardinality\(([^)]+)\)/gi, "$1")
    .replace(/Nullable\(([^)]+)\)/gi, "$1");
}

export function isComplexFieldType(type: string): boolean {
  const lowerType = unwrapFieldType(type).toLowerCase();
  return (
    lowerType.startsWith("map(") ||
    lowerType.startsWith("array(") ||
    lowerType.startsWith("tuple(") ||
    lowerType === "json" ||
    lowerType.startsWith("json(")
  );
}

export function isPriorityField(type: string): boolean {
  if (isComplexFieldType(type)) {
    return false;
  }
  if (type.includes("LowCardinality") || type.startsWith("Enum")) {
    return true;
  }

  const lower = unwrapFieldType(type).toLowerCase();
  return /^u?int\d/.test(lower) || /^float\d/.test(lower) || /^decimal/.test(lower);
}

export function isClickToLoadField(type: string): boolean {
  if (isComplexFieldType(type)) {
    return false;
  }

  const lower = unwrapFieldType(type).toLowerCase();
  return (
    lower === "string" ||
    /^u?int\d/.test(lower) ||
    /^float\d/.test(lower) ||
    /^decimal/.test(lower)
  );
}

export function isFilterableField(type: string): boolean {
  return isPriorityField(type) || isClickToLoadField(type);
}

export function isSystemField(
  source: Pick<Source, "source_type"> | null | undefined,
  fieldName: string,
): boolean {
  const normalized = normalizeFieldName(fieldName);
  if (source?.source_type === "victorialogs") {
    if (VICTORIALOGS_SYSTEM_FIELDS.has(normalized)) {
      return true;
    }

    return fieldName.startsWith("_") && !isPrimaryMessageField(fieldName) && normalized !== "time";
  }

  return false;
}

export function isContextFieldName(fieldName: string): boolean {
  return CONTEXT_FIELD_ALIASES.includes(
    normalizeFieldName(fieldName) as (typeof CONTEXT_FIELD_ALIASES)[number],
  );
}

function isCoreField(
  source: Pick<Source, "_meta_ts_field" | "_meta_severity_field" | "source_type"> | null | undefined,
  field: SourceFieldLike,
): boolean {
  if (field.isTimestamp || field.isSeverity) {
    return true;
  }
  if (source?._meta_ts_field && field.name === source._meta_ts_field) {
    return true;
  }
  if (source?._meta_severity_field && field.name === source._meta_severity_field) {
    return true;
  }
  return isPrimaryMessageField(field.name);
}

export function classifySourceField(
  source: Pick<Source, "source_type" | "_meta_ts_field" | "_meta_severity_field"> | null | undefined,
  field: SourceFieldLike,
): SourceFieldGroupId {
  if (isCoreField(source, field)) {
    return "core";
  }
  if (isSystemField(source, field.name)) {
    return "system";
  }
  if (isContextFieldName(field.name)) {
    return "context";
  }
  if (isFilterableField(field.type)) {
    return "filterable";
  }
  return "other";
}

const GROUP_ORDER: SourceFieldGroupId[] = ["core", "context", "filterable", "system", "other"];

export function buildSourceFieldGroups<TField extends SourceFieldLike>(
  fields: TField[],
  source: Pick<Source, "source_type" | "_meta_ts_field" | "_meta_severity_field"> | null | undefined,
): SourceFieldGroup<TField>[] {
  const grouped = new Map<SourceFieldGroupId, TField[]>();

  for (const field of fields) {
    const groupId = classifySourceField(source, field);
    const existing = grouped.get(groupId) ?? [];
    existing.push(field);
    grouped.set(groupId, existing);
  }

  return GROUP_ORDER
    .map((groupId) => {
      const groupFields = grouped.get(groupId) ?? [];
      if (groupFields.length === 0) {
        return null;
      }

      return {
        id: groupId,
        label: GROUP_METADATA[groupId].label,
        description: GROUP_METADATA[groupId].description,
        fields: groupFields,
        filterableFields: groupFields.filter((field) => isFilterableField(field.type)),
        plainFields: groupFields.filter((field) => !isFilterableField(field.type)),
      } satisfies SourceFieldGroup<TField>;
    })
    .filter((group): group is SourceFieldGroup<TField> => group !== null);
}
