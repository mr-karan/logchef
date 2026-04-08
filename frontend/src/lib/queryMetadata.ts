export type QueryLanguage = "logchefql" | "clickhouse-sql" | "logsql";
export type SavedQueryEditorMode = "builder" | "native";
export type AlertEditorMode = "condition" | "native";
export type LegacySavedQueryType = "logchefql" | "sql";
export type LegacyAlertQueryType = "condition" | "sql";

type SourceDescriptor =
  | {
      source_type?: string | null;
      query_languages?: string[] | null;
      capabilities?: string[] | null;
    }
  | string
  | null
  | undefined;

function getSourceType(source: SourceDescriptor): string | null {
  return typeof source === "string" ? source : source?.source_type ?? null;
}

function getSupportedQueryLanguages(source: SourceDescriptor): QueryLanguage[] {
  const explicit = typeof source === "string" ? null : source?.query_languages;
  if (explicit && explicit.length > 0) {
    return explicit.filter(
      (language): language is QueryLanguage =>
        language === "logchefql" || language === "clickhouse-sql" || language === "logsql"
    );
  }

  return getSourceType(source) === "victorialogs"
    ? ["logsql"]
    : ["logchefql", "clickhouse-sql"];
}

export function supportsQueryLanguage(source: SourceDescriptor, language: QueryLanguage): boolean {
  return getSupportedQueryLanguages(source).includes(language);
}

export function hasSourceCapability(source: SourceDescriptor, capability: string): boolean {
  const explicit = typeof source === "string" ? null : source?.capabilities;
  if (explicit && explicit.length > 0) {
    return explicit.includes(capability);
  }

  const sourceType = getSourceType(source);
  if (sourceType === "victorialogs") {
    return capability === "histogram" || capability === "field_values" || capability === "schema_inspection";
  }
  if (sourceType === "clickhouse" || sourceType == null) {
    return (
      capability === "histogram" ||
      capability === "field_values" ||
      capability === "schema_inspection" ||
      capability === "source_stats" ||
      capability === "ai_sql_generation"
    );
  }
  return false;
}

export function isVictoriaLogsSource(source: SourceDescriptor): boolean {
  return getNativeQueryLanguageForSource(source) === "logsql";
}

export function getNativeQueryLanguageForSource(source: SourceDescriptor): QueryLanguage {
  return supportsQueryLanguage(source, "logsql") ? "logsql" : "clickhouse-sql";
}

export function legacySavedQueryTypeFromLanguage(language: QueryLanguage): LegacySavedQueryType {
  return language === "logchefql" ? "logchefql" : "sql";
}

export function legacyAlertQueryTypeFromEditorMode(mode: AlertEditorMode): LegacyAlertQueryType {
  return mode === "condition" ? "condition" : "sql";
}

export function resolveSavedQueryMetadata(input: {
  query_language?: string | null;
  editor_mode?: string | null;
  query_type?: string | null;
  source_type?: string | null;
}): { queryLanguage: QueryLanguage; editorMode: SavedQueryEditorMode; queryType: LegacySavedQueryType } {
  const sourceType = input.source_type ?? null;
  const explicitLanguage = input.query_language;
  const explicitEditorMode = input.editor_mode;
  const legacyType = input.query_type;

  let queryLanguage: QueryLanguage;
  if (explicitLanguage === "logchefql" || explicitLanguage === "clickhouse-sql" || explicitLanguage === "logsql") {
    queryLanguage = explicitLanguage;
  } else if (legacyType === "logchefql") {
    queryLanguage = "logchefql";
  } else {
    queryLanguage = getNativeQueryLanguageForSource(sourceType);
  }

  let editorMode: SavedQueryEditorMode;
  if (explicitEditorMode === "builder" || explicitEditorMode === "native") {
    editorMode = explicitEditorMode;
  } else {
    editorMode = queryLanguage === "logchefql" ? "builder" : "native";
  }

  return {
    queryLanguage,
    editorMode,
    queryType: legacySavedQueryTypeFromLanguage(queryLanguage),
  };
}

export function resolveAlertMetadata(input: {
  query_language?: string | null;
  editor_mode?: string | null;
  query_type?: string | null;
  source_type?: string | null;
}): { queryLanguage: QueryLanguage; editorMode: AlertEditorMode; queryType: LegacyAlertQueryType } {
  const sourceType = input.source_type ?? null;
  const explicitLanguage = input.query_language;
  const explicitEditorMode = input.editor_mode;
  const legacyType = input.query_type;

  let queryLanguage: QueryLanguage;
  if (explicitLanguage === "clickhouse-sql" || explicitLanguage === "logsql") {
    queryLanguage = explicitLanguage;
  } else if (legacyType === "condition") {
    queryLanguage = "clickhouse-sql";
  } else {
    queryLanguage = getNativeQueryLanguageForSource(sourceType);
  }

  let editorMode: AlertEditorMode;
  if (explicitEditorMode === "condition" || explicitEditorMode === "native") {
    editorMode = explicitEditorMode;
  } else if (legacyType === "condition") {
    editorMode = "condition";
  } else {
    editorMode = "native";
  }

  if (queryLanguage === "logsql") {
    editorMode = "native";
  }

  return {
    queryLanguage,
    editorMode,
    queryType: legacyAlertQueryTypeFromEditorMode(editorMode),
  };
}

export function getQueryLanguageLabel(language: string | null | undefined): string {
  switch (language) {
    case "logchefql":
      return "LogchefQL";
    case "logsql":
      return "LogsQL";
    case "clickhouse-sql":
    default:
      return "SQL";
  }
}

export function getExploreModeForQueryLanguage(language: QueryLanguage): "logchefql" | "sql" {
  return language === "logchefql" ? "logchefql" : "sql";
}
