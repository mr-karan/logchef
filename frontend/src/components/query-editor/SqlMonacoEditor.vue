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
    <p class="sql-editor-load-error__title">Unable to initialize SQL editor</p>
    <p class="sql-editor-load-error__description">{{ loadError }}</p>
    <button class="sql-editor-load-error__button" type="button" @click="retryLoadRuntimeDependencies">
      Retry
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, shallowRef, watch, type Component } from "vue";
import { SQL_KEYWORDS } from "@/utils/clickhouse-sql";
import { useFieldValuesStore } from "@/stores/exploreFieldValues";
import {
  parseLogchefQLContext,
  isValueSuggestableField,
  isNumericFieldType,
  formatFieldDetail,
  formatCountShort,
  buildValueInsertText,
  filterValuesByPartial,
} from "@/utils/logchefql-autocomplete";

interface SqlMonacoEditorProps {
  value: string;
  theme: string;
  language: "clickhouse-sql" | "logchefql";
  schema: Record<string, { type: string }>;
  teamId: number;
  sourceId: number;
  tableName: string;
  isExecuting: boolean;
  visible: boolean;
}

const props = defineProps<SqlMonacoEditorProps>();

const emit = defineEmits<{
  (e: "change", value: string): void;
  (e: "submit"): void;
  (e: "ready"): void;
  (e: "focus-change", focused: boolean): void;
}>();

type MonacoModule = typeof import("monaco-editor");
type MonacoUtilsModule = typeof import("@/utils/monaco");
type MonacoEditor = import("monaco-editor").editor.IStandaloneCodeEditor;
type MonacoDisposable = import("monaco-editor").IDisposable;
type MonacoCompletionItem = import("monaco-editor").languages.CompletionItem;
type MonacoRange = import("monaco-editor").IRange;

const editorRef = shallowRef<MonacoEditor | null>(null);
const editorValue = ref(props.value || "");
const isMonacoReady = ref(false);
const MonacoEditorComponent = shallowRef<Component | null>(null);
const monacoModule = shallowRef<MonacoModule | null>(null);
const monacoUtilsModule = shallowRef<MonacoUtilsModule | null>(null);
const activeDisposables = ref<MonacoDisposable[]>([]);
const completionProvider = shallowRef<MonacoDisposable | null>(null);
const isDisposing = ref(false);
const isLoadingRuntimeDependencies = ref(false);
const loadError = ref<string | null>(null);

const fieldNames = computed(() => Object.keys(props.schema ?? {}));
const modelCacheKey = computed(
  () => `${props.language}-${props.sourceId ?? "default"}`
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
    quickSuggestions: { other: true, comments: false, strings: props.language === "logchefql" },
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

// --- LogchefQL Autocomplete ---
type DepsType = NonNullable<ReturnType<typeof getMonacoDependencies>>;

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
    provideCompletionItems: async (model, position) => {
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
                command: { id: "editor.action.triggerSuggest", title: "Re-trigger" },
              };
            }),
          };

        case "operators": {
          const fieldType = props.schema[ctx.key]?.type?.toLowerCase() || "";
          const numeric = isNumericFieldType(fieldType);
          const ops = [
            { label: "=", detail: "equals" },
            { label: "!=", detail: "not equals" },
            ...(!numeric ? [{ label: "~", detail: "regex match" }, { label: "!~", detail: "regex not match" }] : []),
            { label: ">", detail: "greater than" },
            { label: "<", detail: "less than" },
            { label: ">=", detail: "greater or equal" },
            { label: "<=", detail: "less or equal" },
          ];
          return {
            suggestions: ops.map((op, i) => ({
              label: op.label, kind: deps.monaco.languages.CompletionItemKind.Text,
              insertText: op.label, range: insertRange,
              detail: op.detail, sortText: String(i).padStart(2, "0"),
              command: { id: "editor.action.triggerSuggest", title: "Re-trigger" },
            })),
          };
        }

        case "values": {
          const fieldType = props.schema[ctx.key]?.type || "";
          if (!isValueSuggestableField(fieldType)) {
            return { suggestions: [] };
          }

          // Only use cached data from the sidebar — never make network calls during autocomplete.
          // The sidebar populates the shared store when fields are loaded/expanded.
          const summary = fieldValuesStore.getFieldSummary(props.sourceId, ctx.key);
          if (!summary) {
            return { suggestions: [] };
          }

          // Find cached values: scan store entries for this field + source
          const cached = fieldValuesStore.findCachedValues(props.sourceId, ctx.key);
          if (!cached || cached.values.length === 0) {
            return { suggestions: [] };
          }

          return buildValueSuggestions(deps, cached.values, ctx, fieldType, position);
        }

        case "boolean":
          return {
            suggestions: [
              { label: "and", kind: deps.monaco.languages.CompletionItemKind.Keyword, insertText: "and ", range: replaceRange, sortText: "0" },
              { label: "or", kind: deps.monaco.languages.CompletionItemKind.Keyword, insertText: "or ", range: replaceRange, sortText: "1" },
            ],
          };

        case "none":
        default:
          return { suggestions: [] };
      }
    },
    triggerCharacters: [" ", "=", "!", "~", ">", "<", '"', "'"],
  });
}

function buildValueSuggestions(
  deps: DepsType,
  values: Array<{ value: string; count: number }>,
  ctx: { key: string; partial: string; quote: '"' | "'" | null; operator: string },
  fieldType: string,
  position: { lineNumber: number; column: number },
): { suggestions: MonacoCompletionItem[] } {
  const filtered = filterValuesByPartial(values, ctx.partial, fieldType);

  const suggestions = filtered.map((v, i) => {
    const insertText = buildValueInsertText(v.value, fieldType, ctx.quote);

    return {
      label: { label: v.value, description: formatCountShort(v.count) } as any,
      kind: deps.monaco.languages.CompletionItemKind.Value,
      insertText,
      range: {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        // Replace from start of partial text to cursor
        startColumn: position.column - ctx.partial.length,
        endColumn: position.column,
      } as MonacoRange,
      sortText: String(i).padStart(3, "0"),
      filterText: v.value,
      detail: fieldType.replace(/LowCardinality\(([^)]+)\)/gi, '$1').replace(/Nullable\(([^)]+)\)/gi, '$1'),
    } as MonacoCompletionItem;
  });

  return { suggestions };
}

function registerSQLCompletionProvider(deps: DepsType) {
  return deps.monaco.languages.registerCompletionItemProvider("clickhouse-sql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range: MonacoRange = {
        startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn, endColumn: wordInfo.endColumn,
      };
      const textBeforeCursor = model.getValueInRange({
        startLineNumber: position.lineNumber, startColumn: 1,
        endLineNumber: position.lineNumber, endColumn: position.column,
      });
      let suggestions: MonacoCompletionItem[] = [];

      if (/\bFROM\s+$/i.test(textBeforeCursor) && props.tableName) {
        suggestions.push({
          label: props.tableName, kind: deps.monaco.languages.CompletionItemKind.Folder,
          insertText: props.tableName, range, detail: "Current log table",
        });
      }
      if (fieldNames.value.length > 0) {
        suggestions = suggestions.concat(fieldNames.value.map((field) => ({
          label: field, kind: deps.monaco.languages.CompletionItemKind.Field,
          insertText: field, range, detail: props.schema[field]?.type || "unknown",
        })));
      }
      const typedPrefix = wordInfo.word.toUpperCase();
      suggestions = suggestions.concat(
        SQL_KEYWORDS.filter((kw) => kw.startsWith(typedPrefix)).map((kw) => ({
          label: kw, kind: deps.monaco.languages.CompletionItemKind.Keyword,
          insertText: kw + " ", range,
        }))
      );
      return { suggestions };
    },
    triggerCharacters: [" ", "\n", ".", "(", ","],
  });
}

async function initializeEditor(editor: MonacoEditor) {
  const deps = getMonacoDependencies();
  if (isDisposing.value || !deps) {
    return;
  }

  deps.monacoUtils.registerEditorInstance(editor);

  const model = deps.monacoUtils.getOrCreateModel(
    editorValue.value,
    props.language,
    props.sourceId,
    modelCacheKey.value
  );

  editor.setModel(model);
  editor.updateOptions(monacoOptions.value);
  restoreCurrentViewState();
  registerCompletionProvider();

  activeDisposables.value.push(
    editor.onDidFocusEditorWidget(() => emit("focus-change", true)),
    editor.onDidBlurEditorWidget(() => emit("focus-change", false)),
    editor.addAction({
      id: "submit-query",
      label: "Run Query",
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
    }
  }
);

watch(
  () => props.isExecuting,
  (isExecuting) => {
    editorRef.value?.updateOptions({ readOnly: isExecuting });
  }
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
  () => props.sourceId,
  (newSourceId, oldSourceId) => {
    const editor = editorRef.value;
    const deps = getMonacoDependencies();

    if (!editor || isDisposing.value || !deps) {
      return;
    }

    if (oldSourceId !== undefined) {
      saveCurrentViewState(`${props.language}-${oldSourceId ?? "default"}`);
    }

    const model = deps.monacoUtils.getOrCreateModel(
      editorValue.value,
      props.language,
      newSourceId,
      `${props.language}-${newSourceId ?? "default"}`
    );

    editor.setModel(model);
    restoreCurrentViewState();
    nextTick(() => editor.layout());
  }
);

watch(
  () => props.theme,
  () => {
    if (!editorRef.value || isDisposing.value || !props.visible) {
      return;
    }

    nextTick(() => editorRef.value?.layout());
  }
);

onDeactivated(() => {
  const deps = getMonacoDependencies();

  if (!editorRef.value || isDisposing.value || !deps) {
    return;
  }

  saveCurrentViewState();
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

  nextTick(() => {
    if (!editorRef.value) {
      return;
    }

    editorRef.value.layout();
  });
});

onBeforeUnmount(() => {
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
      import("monaco-editor"),
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
        : "The SQL editor dependencies could not be loaded.";
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
</style>
