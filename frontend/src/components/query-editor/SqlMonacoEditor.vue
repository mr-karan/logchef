<template>
  <component
    :is="MonacoEditorComponent"
    v-if="isMonacoReady && MonacoEditorComponent"
    v-model:value="editorValue"
    :theme="props.theme"
    :language="props.language"
    :options="monacoOptions"
    class="h-full w-full"
    @mount="handleMount"
    @update:value="handleEditorChange"
  />

  <div v-else-if="loadError" class="sql-editor-load-error">
    <p class="sql-editor-load-error__title">{{ t('ui.unableToInitializeSQLEditor') }}</p>
    <p class="sql-editor-load-error__description">{{ loadError }}</p>
    <button class="sql-editor-load-error__button" type="button" @click="retryLoadRuntimeDependencies">
      {{ t('ui.retry') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { MonacoThemeName } from "@/utils/monaco-theme";

import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, shallowRef, watch, type Component } from "vue";
import { CLICKHOUSE_FUNCTIONS, SQL_KEYWORDS, SQL_TYPES } from "@/utils/clickhouse-sql";
import { SQL_COMPLETION_KEYWORDS, classifySQLCompletionContext, getSQLCompletionSortText } from "@/utils/sql-autocomplete";
import { buildSQLValueInsertText, getSQLCompletionCursor } from "@/utils/sql-value-completion";
import { useFieldValuesStore } from "@/stores/exploreFieldValues";
import { logchefqlApi } from "@/api/logchefql";
import { prepareLogchefQLTemplate } from "@/utils/logchefql/template";
import { escapeHoverText, getLogchefQLErrorRange, getLogchefQLFields, getSchemaFieldType, getUnknownLogchefQLFields } from "@/utils/logchefql/diagnostics";
import { useCompletionFieldValues, type CompletionValuesWindowGetter } from "@/composables/useCompletionFieldValues";
import { unwrapFieldType } from "@/lib/sourceFields";
import {
  parseLogchefQLContext,
  isValueSuggestableField,
  getLogchefQLOperators,
  formatFieldDetail,
  formatCountShort,
  buildValueCompletionEdit,
  buildBooleanInsertText,
  filterValuesByPartial,
} from "@/utils/logchefql-autocomplete";

const { t } = useI18n();

interface SqlMonacoEditorProps {
  value: string;
  theme: MonacoThemeName;
  language: "clickhouse-sql" | "logchefql";
  schema: Record<string, { type: string }>;
  teamId: number;
  sourceId: number;
  fieldValuesWindow?: CompletionValuesWindowGetter;
  tableName: string;
  isExecuting: boolean;
  visible: boolean;
}

const props = defineProps<SqlMonacoEditorProps>();
const completionFieldValues = useCompletionFieldValues();

const emit = defineEmits<{
  (e: "change", value: string): void;
  (e: "submit"): void;
  (e: "ready"): void;
  (e: "focus-change", focused: boolean): void;
}>();

type MonacoModule = typeof import("monaco-editor/editor/editor.api.js");
type MonacoUtilsModule = typeof import("@/utils/monaco");
type MonacoEditor = import("monaco-editor/editor/editor.api.js").editor.IStandaloneCodeEditor;
type MonacoDisposable = import("monaco-editor/editor/editor.api.js").IDisposable;
type MonacoCompletionItem = import("monaco-editor/editor/editor.api.js").languages.CompletionItem;
type MonacoRange = import("monaco-editor/editor/editor.api.js").IRange;
type MonacoModel = import("monaco-editor/editor/editor.api.js").editor.ITextModel;
type MonacoMarker = import("monaco-editor/editor/editor.api.js").editor.IMarkerData;
type MonacoEditorContribution = import("monaco-editor/editor/editor.api.js").editor.IEditorContribution;
// Providers get CompletionTriggerKind.Invoke for both Ctrl+Space and quick suggestions; only this controller tells them apart.
// Internal SuggestController shape as of monaco-editor 0.57; every member is optional so a rename disables only this.
interface SuggestControllerContribution extends MonacoEditorContribution {
  readonly model?: { onDidTrigger?(listener: (event: { auto: boolean }) => void): MonacoDisposable };
}

const editorRef = shallowRef<MonacoEditor | null>(null);
const editorValue = ref(props.value || "");
const isMonacoReady = ref(false);
const MonacoEditorComponent = shallowRef<Component | null>(null);
const monacoModule = shallowRef<MonacoModule | null>(null);
const monacoUtilsModule = shallowRef<MonacoUtilsModule | null>(null);
const activeDisposables = ref<MonacoDisposable[]>([]);
const completionProvider = shallowRef<MonacoDisposable | null>(null);
const hoverProvider = shallowRef<MonacoDisposable | null>(null);
const fieldWarningsOwner = "logchefql-fields";
const syntaxErrorsOwner = "logchefql-syntax";
let validationTimer: ReturnType<typeof setTimeout> | undefined;
let validationController: AbortController | undefined;
let validationRevision = 0;
const isDisposing = ref(false);
const isLoadingRuntimeDependencies = ref(false);
const loadError = ref<string | null>(null);
let suggestInvoked = false;

const fieldNames = computed(() => Object.keys(props.schema ?? {}));
function schemaType(name: string): string | undefined {
  return getSchemaFieldType(props.schema, { name, baseName: name });
}
const retriggerSuggest = {
  id: "editor.action.triggerSuggest",
  get title() { return t('ui.reTrigger'); },
  arguments: [{ auto: true }],
};
function buildModelCacheKey(
  language: SqlMonacoEditorProps["language"],
  sourceId: number
) {
  return `${language}-${sourceId ?? "default"}`;
}

const modelCacheKey = computed(
  () => buildModelCacheKey(props.language, props.sourceId)
);

const monacoOptions = computed(() => {
  const monacoUtils = monacoUtilsModule.value;
  if (!monacoUtils) {
    return {};
  }

  return {
    ...monacoUtils.getDefaultMonacoOptions(),
    fontSize: 13,
    lineHeight: 20,
    padding: { top: 8, bottom: 8 },
    readOnly: props.isExecuting,
    scrollbar: {
      vertical: "auto" as const,
      horizontal: "auto" as const,
      useShadows: false,
      verticalScrollbarSize: 8,
      horizontalScrollbarSize: 8,
    },
    minimap: { enabled: false },
    lineNumbers: "off" as const,
    wordWrap: "on" as const,
    folding: true,
    scrollBeyondLastLine: false,
    suggest: { showIcons: false },
    quickSuggestions: { other: true, comments: false, strings: true },
  };
});

function getMonacoDependencies() {
  const monaco = monacoModule.value;
  const monacoUtils = monacoUtilsModule.value;

  if (!monaco || !monacoUtils) {
    return null;
  }

  return { monaco, monacoUtils };
}

const handleEditorChange = (value: string | undefined) => {
  const nextValue = value ?? "";
  editorValue.value = nextValue;
  emit("change", nextValue);
};

function saveCurrentViewState(key = modelCacheKey.value) {
  const deps = getMonacoDependencies();
  if (!editorRef.value || !deps) {
    return;
  }

  deps.monacoUtils.saveEditorViewState(key, editorRef.value.saveViewState());
}

function restoreCurrentViewState(key = modelCacheKey.value) {
  const deps = getMonacoDependencies();
  if (!deps) {
    return;
  }

  const viewState = deps.monacoUtils.restoreEditorViewState(key);
  if (!viewState || !editorRef.value) {
    return;
  }

  editorRef.value.restoreViewState(viewState);
}

function syncEditorValue(nextValue: string) {
  const editor = editorRef.value;
  if (!editor || isDisposing.value) {
    return;
  }

  const model = editor.getModel();
  if (!model || model.isDisposed() || model.getValue() === nextValue) {
    return;
  }

  const position = editor.getPosition();
  const selection = editor.getSelection();
  model.setValue(nextValue);

  nextTick(() => {
    if (!editorRef.value || isDisposing.value) {
      return;
    }

    if (position) {
      editorRef.value.setPosition(position);
    }

    if (selection) {
      editorRef.value.setSelection(selection);
    }
  });
}

function bindEditorContext(
  language: SqlMonacoEditorProps["language"],
  sourceId: number,
  value: string,
  previousKey?: string
) {
  const editor = editorRef.value;
  const deps = getMonacoDependencies();

  if (!editor || !deps || isDisposing.value) {
    return;
  }

  if (previousKey) {
    saveCurrentViewState(previousKey);
  }

  clearDiagnostics();
  const nextKey = buildModelCacheKey(language, sourceId);
  const model = deps.monacoUtils.getOrCreateModel(
    value,
    language,
    sourceId,
    nextKey
  );

  editor.setModel(model);
  editor.updateOptions(monacoOptions.value);
  registerCompletionProvider();
  registerFieldHoverProvider(deps);
  scheduleDiagnostics();
  restoreCurrentViewState(nextKey);

  nextTick(() => {
    if (!editorRef.value || isDisposing.value) {
      return;
    }

    editorRef.value.layout();
  });
}

// --- LogchefQL Autocomplete ---
type DepsType = NonNullable<ReturnType<typeof getMonacoDependencies>>;

function cancelValidation() {
  validationRevision++;
  clearTimeout(validationTimer);
  validationController?.abort();
  validationController = undefined;
}

function clearDiagnostics() {
  cancelValidation();
  const model = editorRef.value?.getModel();
  if (model && !model.isDisposed()) {
    monacoModule.value?.editor.setModelMarkers(model, fieldWarningsOwner, []);
    monacoModule.value?.editor.setModelMarkers(model, syntaxErrorsOwner, []);
  }
}

function markerRange(model: MonacoModel, start: number, end: number): MonacoRange {
  const first = model.getPositionAt(start);
  const last = model.getPositionAt(end);
  return {
    startLineNumber: first.lineNumber, startColumn: first.column,
    endLineNumber: last.lineNumber, endColumn: last.column,
  };
}

// Field warnings update on every edit; the syntax error stays until the next validation replaces it.
function scheduleDiagnostics() {
  const deps = getMonacoDependencies();
  const model = editorRef.value?.getModel();
  if (!deps || !model || props.language !== "logchefql" || !model.getValue().trim()) {
    clearDiagnostics();
    return;
  }
  cancelValidation();
  const query = model.getValue();
  deps.monaco.editor.setModelMarkers(model, fieldWarningsOwner, getUnknownLogchefQLFields(query, props.schema).map(field => ({
    ...markerRange(model, field.start, field.end),
    severity: deps.monaco.MarkerSeverity.Warning,
    message: t('queryEditor.unknownField', { field: field.name }),
  })));
  const revision = validationRevision;
  validationTimer = setTimeout(() => { void validateModel(deps, model, query, revision); }, 400);
}

async function validateModel(deps: DepsType, model: MonacoModel, query: string, revision: number) {
  const prepared = prepareLogchefQLTemplate(query);
  const controller = validationController = new AbortController();
  let errors: MonacoMarker[] = [];
  try {
    const response = await logchefqlApi.validate(props.teamId, props.sourceId, prepared, {
      signal: controller.signal, suppressErrorToast: true, suppressForbiddenRedirect: true,
    });
    if (response.status === "success" && response.data && !response.data.valid) {
      const error = response.data.error;
      const range = getLogchefQLErrorRange(query, prepared, error?.position);
      errors = [{
        ...markerRange(model, range.start, range.end),
        severity: deps.monaco.MarkerSeverity.Error,
        message: error?.message ? t('queryEditor.syntaxError', { message: error.message }) : t('queryEditor.invalidSyntax'),
      }];
    }
  } catch {
    // A validation outage must not block editing, and an error for older text would mislead.
  }
  if (revision !== validationRevision || model.isDisposed() || editorRef.value?.getModel() !== model) return;
  deps.monaco.editor.setModelMarkers(model, syntaxErrorsOwner, errors);
}

function registerFieldHoverProvider(deps: DepsType) {
  hoverProvider.value?.dispose();
  hoverProvider.value = null;
  if (props.language !== "logchefql") return;
  const fieldValuesStore = useFieldValuesStore();
  hoverProvider.value = deps.monaco.languages.registerHoverProvider("logchefql", {
    provideHover(model, position) {
      if (editorRef.value?.getModel() !== model) return null;
      const offset = model.getOffsetAt(position);
      const field = getLogchefQLFields(model.getValue()).find(field => field.start <= offset && offset < field.end);
      if (!field) return null;
      const type = getSchemaFieldType(props.schema, field);
      if (type === undefined) return null;
      const contents = [{ value: `**${escapeHoverText(field.name)}**\n\n${escapeHoverText(type)}` }];
      const summary = fieldValuesStore.getFieldSummary(props.sourceId, field.name);
      const cached = fieldValuesStore.findCachedValues(props.sourceId, field.name);
      if (summary) contents.push({ value: escapeHoverText(t('queryEditor.distinctCount', { count: summary.totalDistinct })) });
      if (cached?.values.length) contents.push({
        value: `${escapeHoverText(t('queryEditor.topValues'))}\n\n${cached.values.slice(0, 5).map(value =>
          `${escapeHoverText(value.value)} (${value.count})`
        ).join('  \n')}`,
      });
      return { range: markerRange(model, field.start, field.end), contents };
    },
  });
}

function registerCompletionProvider() {
  const deps = getMonacoDependencies();
  if (!deps) return;

  if (completionProvider.value) {
    completionProvider.value.dispose();
    completionProvider.value = null;
  }

  if (props.language === "logchefql") {
    completionProvider.value = registerLogchefQLCompletionProvider(deps);
  } else {
    completionProvider.value = registerSQLCompletionProvider(deps);
  }
}

function registerLogchefQLCompletionProvider(deps: DepsType) {
  const fieldValuesStore = useFieldValuesStore();

  return deps.monaco.languages.registerCompletionItemProvider("logchefql", {
    provideCompletionItems: async (model, position, _context, token) => {
      if (model !== editorRef.value?.getModel()) return { suggestions: [] };
      const textBeforeCursor = model.getValueInRange({
        startLineNumber: 1, startColumn: 1,
        endLineNumber: position.lineNumber, endColumn: position.column,
      });
      const wordInfo = model.getWordUntilPosition(position);
      const replaceRange: MonacoRange = {
        startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn, endColumn: wordInfo.endColumn,
      };
      const insertRange: MonacoRange = {
        startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
        startColumn: position.column, endColumn: position.column,
      };

      const ctx = parseLogchefQLContext(textBeforeCursor, fieldNames.value);

      switch (ctx.suggest) {
        case "fields":
          return {
            suggestions: fieldNames.value.map((name, i) => {
              const fieldType = props.schema[name]?.type || "Unknown";
              const summary = fieldValuesStore.getFieldSummary(props.sourceId, name);
              return {
                label: name,
                kind: deps.monaco.languages.CompletionItemKind.Field,
                insertText: name,
                range: replaceRange,
                detail: formatFieldDetail(fieldType, summary?.totalDistinct ?? null),
                sortText: String(i).padStart(3, "0"),
                command: retriggerSuggest,
              };
            }),
          };

        case "operators": {
          const fieldType = schemaType(ctx.key)?.toLowerCase() ?? "";
          const details = {
            "=": "equals", "!=": "not equals", "~": "regex match", "!~": "regex not match",
            ">": "greater than", "<": "less than", ">=": "greater or equal", "<=": "less or equal",
          };
          return {
            suggestions: getLogchefQLOperators(fieldType).map((op, i) => ({
              label: op, kind: deps.monaco.languages.CompletionItemKind.Text,
              insertText: op, range: insertRange,
              detail: details[op], sortText: String(i).padStart(2, "0"),
              command: retriggerSuggest,
            })),
          };
        }

        case "values": {
          const fieldType = schemaType(ctx.key);
          if (fieldType === undefined || !isValueSuggestableField(fieldType)) {
            return { suggestions: [] };
          }

          const version = model.getVersionId();
          const values = await completionFieldValues.getValues({
            teamId: props.teamId, sourceId: props.sourceId, fieldName: ctx.key, fieldType,
            getWindow: props.fieldValuesWindow, invoked: suggestInvoked,
          }, token);
          if (model.isDisposed() || model.getVersionId() !== version || values.length === 0) {
            return { suggestions: [] };
          }

          const textAfterCursor = model.getLineContent(position.lineNumber).slice(position.column - 1);
          return buildValueSuggestions(deps, values, ctx, fieldType, position, textAfterCursor);
        }

        case "boolean":
          return {
            suggestions: (["and", "or"] satisfies Array<'and' | 'or'>).map((keyword, i) => ({
              label: keyword, kind: deps.monaco.languages.CompletionItemKind.Keyword,
              insertText: buildBooleanInsertText(keyword, textBeforeCursor, ctx.partial),
              range: replaceRange, sortText: String(i),
            })),
          };

        case "none":
        default:
          return { suggestions: [] };
      }
    },
    triggerCharacters: [" ", "(", ")", "=", "!", "~", ">", "<", '"', "'"],
  });
}

function buildValueSuggestions(
  deps: DepsType,
  values: Array<{ value: string; count: number }>,
  ctx: { key: string; partial: string; quote: '"' | "'" | null; operator: string },
  fieldType: string,
  position: { lineNumber: number; column: number },
  textAfterCursor: string,
): { suggestions: MonacoCompletionItem[] } {
  const filtered = filterValuesByPartial(values, ctx.partial, fieldType);

  const suggestions: MonacoCompletionItem[] = filtered.map((v, i) => {
    const { insertText, replaceAfter, retrigger } = buildValueCompletionEdit(v.value, fieldType, ctx.quote, textAfterCursor);

    return {
      label: { label: v.value, description: formatCountShort(v.count) },
      kind: deps.monaco.languages.CompletionItemKind.Value,
      insertText,
      range: {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: position.column - ctx.partial.length,
        endColumn: position.column + replaceAfter,
      },
      sortText: String(i).padStart(3, "0"),
      filterText: v.value,
      detail: unwrapFieldType(fieldType),
      command: retrigger ? retriggerSuggest : undefined,
    };
  });

  return { suggestions };
}

function registerSQLCompletionProvider(deps: DepsType) {
  const terminalKeywords = new Set(["ASC", "DESC", "END", "NULL", "JSON", "TabSeparated", "CSV", "TSV"]);
  const keywordNames = new Set(SQL_KEYWORDS.map((keyword) => keyword.toUpperCase()));
  return deps.monaco.languages.registerCompletionItemProvider("clickhouse-sql", {
    provideCompletionItems: async (model, position, _context, token) => {
      if (model !== editorRef.value?.getModel()) return { suggestions: [] };

      const cursor = getSQLCompletionCursor(model.getValue(), model.getOffsetAt(position), props.schema);
      if (cursor.kind === "literal") return { suggestions: [] };
      if (cursor.kind === "value") {
        const start = model.getPositionAt(cursor.start);
        const end = model.getPositionAt(cursor.end);
        if (start.lineNumber !== position.lineNumber || end.lineNumber !== position.lineNumber) return { suggestions: [] };
        const version = model.getVersionId();
        const values = await completionFieldValues.getValues({
          teamId: props.teamId, sourceId: props.sourceId, fieldName: cursor.field, fieldType: cursor.fieldType,
          getWindow: props.fieldValuesWindow, invoked: suggestInvoked,
        }, token);
        if (model.isDisposed() || model.getVersionId() !== version) return { suggestions: [] };
        return {
          suggestions: filterValuesByPartial(values, cursor.partial, cursor.fieldType).map((value, index) => ({
            label: { label: value.value, description: formatCountShort(value.count) },
            kind: deps.monaco.languages.CompletionItemKind.Value,
            insertText: buildSQLValueInsertText(value.value),
            range: { startLineNumber: start.lineNumber, startColumn: start.column, endLineNumber: end.lineNumber, endColumn: end.column },
            filterText: buildSQLValueInsertText(value.value),
            sortText: String(index).padStart(3, "0"),
            detail: unwrapFieldType(cursor.fieldType),
          })),
        };
      }

      const wordInfo = model.getWordUntilPosition(position);
      const range: MonacoRange = {
        startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn, endColumn: wordInfo.endColumn,
      };
      const textBeforeWord = model.getValueInRange({
        startLineNumber: 1, startColumn: 1,
        endLineNumber: position.lineNumber, endColumn: wordInfo.startColumn,
      });
      const context = classifySQLCompletionContext(textBeforeWord);
      const textAfterWord = model.getLineContent(position.lineNumber).slice(wordInfo.endColumn - 1);
      const suggestions: MonacoCompletionItem[] = [];

      if (props.tableName) {
        suggestions.push({
          label: props.tableName, kind: deps.monaco.languages.CompletionItemKind.Folder,
          insertText: props.tableName, range, detail: t("ui.table"),
          sortText: getSQLCompletionSortText(context, "table", props.tableName),
        });
      }
      suggestions.push(
        ...fieldNames.value.map((field) => ({
          label: field, kind: deps.monaco.languages.CompletionItemKind.Field,
          insertText: field, range, detail: props.schema[field]?.type,
          sortText: getSQLCompletionSortText(context, "column", field),
        })),
        ...CLICKHOUSE_FUNCTIONS.map((func) => ({
          label: func, kind: deps.monaco.languages.CompletionItemKind.Function,
          insertText: `${func}($0)`, range, detail: `${func}()`,
          insertTextRules: deps.monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
          sortText: getSQLCompletionSortText(context, "function", func),
        })),
        ...SQL_COMPLETION_KEYWORDS.map((kw) => ({
          label: kw, kind: deps.monaco.languages.CompletionItemKind.Keyword,
          insertText: kw + (terminalKeywords.has(kw) || /^\s/.test(textAfterWord) ? "" : " "), range,
          sortText: getSQLCompletionSortText(context, "keyword", kw),
        })),
        ...SQL_TYPES.filter((type) => !keywordNames.has(type.toUpperCase())).map((type) => ({
          label: type, kind: deps.monaco.languages.CompletionItemKind.TypeParameter,
          insertText: type, range, detail: t("ui.dataType"),
          sortText: getSQLCompletionSortText(context, "type", type),
        })),
      );
      return { suggestions };
    },
    triggerCharacters: [" ", "\n", ".", "(", ",", "=", "!", "<", ">", "'"],
  });
}

async function initializeEditor(editor: MonacoEditor) {
  const deps = getMonacoDependencies();
  if (isDisposing.value || !deps) {
    return;
  }

  deps.monacoUtils.registerEditorInstance(editor);
  bindEditorContext(props.language, props.sourceId, editorValue.value);

  const suggestTrigger = editor.getContribution<SuggestControllerContribution>("editor.contrib.suggestController")
    ?.model?.onDidTrigger?.(({ auto }) => { suggestInvoked = !auto; });
  if (suggestTrigger) activeDisposables.value.push(suggestTrigger);
  activeDisposables.value.push(
    editor.onDidChangeModelContent(scheduleDiagnostics),
    editor.onDidFocusEditorWidget(() => emit("focus-change", true)),
    editor.onDidBlurEditorWidget(() => emit("focus-change", false)),
    editor.addAction({
      id: "submit-query",
      get label() { return t('ui.runQuery'); },
      keybindings: [deps.monaco.KeyMod.CtrlCmd | deps.monaco.KeyCode.Enter],
      run: () => emit("submit"),
    })
  );

  nextTick(() => {
    if (props.visible) {
      editor.layout();
    }
    emit("ready");
  });
}

const handleMount = (editor: MonacoEditor) => {
  editorRef.value = editor;
  void initializeEditor(editor);
};

function focus(revealLastPosition = false) {
  const deps = getMonacoDependencies();

  nextTick(() => {
    setTimeout(() => {
      const editor = editorRef.value;
      if (!editor || isDisposing.value || !deps) {
        return;
      }

      editor.focus();

      if (!revealLastPosition) {
        return;
      }

      const model = editor.getModel();
      if (!model) {
        return;
      }

      const lineCount = model.getLineCount();
      const lastColumn = model.getLineMaxColumn(lineCount);
      const position = new deps.monaco.Position(lineCount, lastColumn);

      editor.setPosition(position);
      editor.revealPositionInCenterIfOutsideViewport(
        position,
        deps.monaco.editor.ScrollType.Smooth
      );
    }, 50);
  });
}

function disposeMonacoEditor() {
  saveCurrentViewState();
  clearDiagnostics();
  hoverProvider.value?.dispose();
  hoverProvider.value = null;

  if (completionProvider.value) {
    completionProvider.value.dispose();
    completionProvider.value = null;
  }

  for (const disposable of activeDisposables.value) {
    disposable.dispose();
  }
  activeDisposables.value = [];

  const editor = editorRef.value;
  if (!editor) {
    return;
  }

  const deps = getMonacoDependencies();
  if (deps) {
    deps.monacoUtils.unregisterEditorInstance(editor);
  }

  editor.setModel(null);
  editor.dispose();
  editorRef.value = null;
}

watch(
  () => props.value,
  (newValue) => {
    const nextValue = newValue || "";
    editorValue.value = nextValue;
    syncEditorValue(nextValue);
  }
);

watch(
  () => Object.keys(props.schema ?? {}).join(","),
  () => {
    if (editorRef.value && !isDisposing.value) {
      registerCompletionProvider();
      scheduleDiagnostics();
    }
  }
);

watch(() => props.teamId, scheduleDiagnostics);

watch(
  () => props.isExecuting,
  (isExecuting) => {
    editorRef.value?.updateOptions({ readOnly: isExecuting });
  }
);

watch(
  () => [props.teamId, props.sourceId, props.language],
  () => completionFieldValues.cancelAll(),
  { flush: "sync" },
);

watch(
  () => props.visible,
  (visible) => {
    if (!editorRef.value || isDisposing.value) {
      return;
    }

    if (!visible) {
      saveCurrentViewState();
      return;
    }

    nextTick(() => {
      setTimeout(() => {
        if (!editorRef.value || isDisposing.value) {
          return;
        }

        editorRef.value.layout();
        restoreCurrentViewState();
      }, 50);
    });
  }
);

watch(
  () => props.language,
  (newLanguage, oldLanguage) => {
    if (!editorRef.value || isDisposing.value || !oldLanguage || newLanguage === oldLanguage) {
      return;
    }

    const nextValue = props.value || "";
    editorValue.value = nextValue;

    bindEditorContext(
      newLanguage,
      props.sourceId,
      nextValue,
      buildModelCacheKey(oldLanguage, props.sourceId)
    );
  }
);

watch(
  () => props.sourceId,
  (newSourceId, oldSourceId) => {
    if (!editorRef.value || isDisposing.value) {
      return;
    }

    bindEditorContext(
      props.language,
      newSourceId,
      props.value || "",
      oldSourceId !== undefined
        ? buildModelCacheKey(props.language, oldSourceId)
        : undefined
    );
  }
);

onDeactivated(() => {
  completionFieldValues.cancelAll();
  const deps = getMonacoDependencies();

  if (!editorRef.value || isDisposing.value || !deps) {
    return;
  }

  saveCurrentViewState();
  clearDiagnostics();
  deps.monacoUtils.lightweightEditorDisposal(editorRef.value);
});

onActivated(() => {
  const deps = getMonacoDependencies();

  if (!isMonacoReady.value) {
    return;
  }

  if (!editorRef.value || isDisposing.value || !deps) {
    return;
  }

  deps.monacoUtils.reactivateEditor(
    editorRef.value,
    props.language,
    editorValue.value,
    props.sourceId
  );
  restoreCurrentViewState();
  scheduleDiagnostics();

  nextTick(() => {
    if (!editorRef.value) {
      return;
    }

    editorRef.value.layout();
  });
});

onBeforeUnmount(() => {
  completionFieldValues.cancelAll();
  isDisposing.value = true;
  disposeMonacoEditor();
  isDisposing.value = false;
});

async function loadRuntimeDependencies(force = false) {
  if (
    (isMonacoReady.value && !force) ||
    isLoadingRuntimeDependencies.value
  ) {
    return;
  }

  if (force) {
    loadError.value = null;
    isMonacoReady.value = false;
    MonacoEditorComponent.value = null;
    monacoModule.value = null;
    monacoUtilsModule.value = null;
  }

  isLoadingRuntimeDependencies.value = true;

  try {
    const [editorModule, monaco, monacoUtils] = await Promise.all([
      import("@guolao/vue-monaco-editor"),
      import("monaco-editor/editor/editor.api.js"),
      import("@/utils/monaco"),
    ]);

    MonacoEditorComponent.value = editorModule.VueMonacoEditor;
    monacoModule.value = monaco;
    monacoUtilsModule.value = monacoUtils;

    await monacoUtils.ensureMonacoSetup();
    loadError.value = null;
    isMonacoReady.value = true;
  } catch (error) {
    loadError.value =
      error instanceof Error
        ? error.message
        : t('ui.theSQLEditorDependenciesCouldNotBeLoaded');
    console.error("Failed to load SQL editor", error);
  } finally {
    isLoadingRuntimeDependencies.value = false;
  }
}

function retryLoadRuntimeDependencies() {
  void loadRuntimeDependencies(true);
}

onMounted(async () => {
  await loadRuntimeDependencies();
});

defineExpose({
  focus,
});
</script>

<style scoped>
.sql-editor-load-error {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 0.75rem;
  width: 100%;
  height: 100%;
  padding: 1rem;
}

.sql-editor-load-error__title {
  font-size: 0.875rem;
  font-weight: 600;
}

.sql-editor-load-error__description {
  font-size: 0.75rem;
  color: var(--muted-foreground);
}

.sql-editor-load-error__button {
  width: fit-content;
  border: 1px solid var(--border);
  border-radius: 0.375rem;
  padding: 0.375rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 500;
}

:deep(.monaco-editor),
:deep(.monaco-editor .overflow-guard) {
  border: none !important;
  outline: none !important;
  background-color: transparent !important;
}

:deep(.monaco-editor .margin) {
  border-radius: 0 0 0 5px;
  padding-right: 0 !important;
  background-color: transparent !important;
  width: 0 !important;
}

:deep(.monaco-editor .monaco-scrollable-element) {
  left: 0 !important;
}

:deep(.monaco-editor .view-line) {
  margin-left: 0 !important;
}

:deep(.monaco-editor .suggest-widget),
:deep(.monaco-editor .monaco-resizable-hover) {
  border-radius: calc(var(--radius) + 2px);
  box-shadow: 0 4px 6px -1px rgb(0 0 0 / 10%), 0 2px 4px -2px rgb(0 0 0 / 10%);
}

/* Clip the list, not the widget: the resize sashes straddle the widget's edge. Monaco already clips the hover. */
:deep(.monaco-editor .suggest-widget > .tree) {
  border-radius: calc(var(--radius) + 1px);
  overflow: hidden;
}

:deep(.monaco-editor .suggest-widget .monaco-list-row > .contents > .main),
:deep(.monaco-editor .monaco-hover) {
  font-family: 'Geist', ui-sans-serif, system-ui, sans-serif !important;
}

:deep(.monaco-editor .suggest-widget .monaco-list-row) {
  padding-left: 6px;
  padding-right: 10px;
}

:deep(.monaco-editor .suggest-widget .monaco-list-row > .contents) {
  display: block;
  min-width: 0;
}

:deep(.monaco-editor .suggest-widget .monaco-list .monaco-list-row.focused) {
  outline: none;
}

:deep(.monaco-editor .suggest-widget .monaco-list .monaco-list-row > .contents > .main > .right) {
  margin-left: auto;
  flex-shrink: 0;
  padding-left: 16px;
}

:deep(.monaco-editor .suggest-widget .monaco-list-row .details-label) {
  font-variant-numeric: tabular-nums;
  text-align: right;
}
</style>
