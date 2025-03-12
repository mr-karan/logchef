<template>
    <div
        :style="{ height: `${editorHeight}px` }"
        class="editor border rounded pl-2 pr-2 mb-2 dark:border-neutral-600"
        :class="{ 'border-sky-800 dark:border-sky-700': editorFocused }"
    >
        <vue-monaco-editor
            v-model:value="code"
            :theme="theme"
            language="flyql"
            :options="getDefaultMonacoOptions()"
            @mount="handleMount"
            @change="onChange"
        />
    </div>
</template>

<script setup>
import { ref, computed, shallowRef, nextTick, watch } from "vue";

import * as monaco from "monaco-editor";

import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";

import {
    Parser,
    State,
    Operator,
    VALID_KEY_VALUE_OPERATORS,
} from "@/utils/flyql.js";
import { isNumeric } from "@/utils/utils.js";
import { getDefaultMonacoOptions } from "@/utils/monaco.js";
import { SourceService } from "@/sdk/services/Source.js";

const sourceSrv = new SourceService();

const emit = defineEmits(["change", "submit"]);
const props = defineProps(["source", "value", "from", "to"]);

const isDark = useDark();
const sourceFieldsNames = Object.keys(props.source.fields);

const editorFocused = ref(false);

const editorHeight = computed(() => {
    const lines = (code.value.match(/\n/g) || "").length + 1;
    return 14 + lines * 20;
});

const theme = computed(() => {
    if (isDark.value) {
        return "telescope-dark";
    } else {
        return "telescope";
    }
});

const code = ref(props.value);
const editorRef = shallowRef();

const getSuggestionsFromList = (params) => {
    const suggestions = [];
    let defaultPostfix = params.postfix === undefined ? "" : params.postfix;

    const range =
        params.range === undefined
            ? {
                  startLineNumber: params.position.lineNumber,
                  endLineNumber: params.position.lineNumber,
                  startColumn: params.position.column,
                  endColumn: params.position.column,
              }
            : params.range;

    for (const item of params.items) {
        let label = null;
        let sortText = null;
        let postfix = defaultPostfix;
        if (typeof item === "string" || item instanceof String) {
            label = item;
            sortText = label;
        } else {
            label = item.label;
            sortText = item.sortText;
            sortText = item.sortText === undefined ? label : item.sortText;
            postfix =
                item.postfix === undefined ? defaultPostfix : item.postfix;
        }
        let insertText =
            item.insertText === undefined ? label : item.insertText;
        suggestions.push({
            label: label,
            kind: params.kind,
            range: range,
            sortText: sortText,
            insertText: insertText + postfix,
            command: {
                id: "editor.action.triggerSuggest",
            },
        });
    }
    return suggestions;
};

const getOperatorsSuggestions = (field, position) => {
    let operators = [
        { label: Operator.EQUALS, sortText: "a" },
        { label: Operator.NOT_EQUALS, sortText: "b" },
        { label: Operator.GREATER_THAN, sortText: "e" },
        { label: Operator.GREATER_OR_EQUALS_THAN, sortText: "f" },
        { label: Operator.LOWER_THAN, sortText: "g" },
        { label: Operator.LOWER_OR_EQUALS_THAN, sortText: "h" },
    ];

    if (props.source.fields[field].type != "enum") {
        operators = operators.concat([
            { label: Operator.EQUALS_REGEX, sortText: "c" },
            { label: Operator.NOT_EQUALS_REGEX, sortText: "d" },
        ]);
    }
    return getSuggestionsFromList({
        position: position,
        items: operators,
        kind: monaco.languages.CompletionItemKind.Operator,
    });
};

const getBooleanOperatorsSuggestions = (range) => {
    return getSuggestionsFromList({
        range: range,
        items: ["and", "or"],
        kind: monaco.languages.CompletionItemKind.Enum,
        postfix: " ",
    });
};

const getKeySuggestions = (range) => {
    const suggestions = [];
    for (const name of sourceFieldsNames) {
        if (props.source.fields[name].suggest) {
            let documentation =
                "Field (" + props.source.fields[name].type + ")";
            suggestions.push({
                label: name,
                kind: monaco.languages.CompletionItemKind.Keyword,
                range: range,
                documentation: documentation,
                insertText: name,
                command: {
                    id: "editor.action.triggerSuggest",
                },
            });
        }
    }
    return suggestions;
};

const prepareSuggestionValues = (items, quoteChar) => {
    const quoted = quoteChar === undefined ? false : true;
    const defaultQuoteChar = quoteChar === undefined ? '"' : quoteChar;
    const result = [];
    for (const item of items) {
        if (isNumeric(item)) {
            result.push({ label: item });
        } else {
            let newItem = "";
            if (!quoted) {
                newItem = defaultQuoteChar;
            }
            for (const char of item) {
                if (char == defaultQuoteChar) {
                    newItem += `\${defaultQuoteChar}`;
                } else {
                    newItem += char;
                }
            }
            newItem += defaultQuoteChar;
            result.push({ label: item, insertText: newItem });
        }
    }
    return result;
};

const getValueSuggestions = async (key, value, range, quoteChar) => {
    const result = {
        suggestions: [],
        incomplete: false,
    };
    let items = [];
    if (sourceFieldsNames.includes(key)) {
        if (props.source.fields[key].autocomplete) {
            if (props.source.fields[key].values.length > 0) {
                items = prepareSuggestionValues(
                    props.source.fields[key].values,
                    quoteChar,
                );
            } else {
                let resp = await sourceSrv.autocomplete(props.source.slug, {
                    field: key,
                    value: value,
                    from: props.from,
                    to: props.to,
                });
                items = prepareSuggestionValues(resp.data.items, quoteChar);
                result.incomplete = resp.data.incomplete;
            }
            result.suggestions = getSuggestionsFromList({
                range: range,
                items: items,
                kind: monaco.languages.CompletionItemKind.Text,
                postfix: " ",
            });
        }
    }
    return result;
};

const getSuggestions = async (word, position, textBeforeCursor) => {
    let incomplete = false;
    let suggestions = [];
    let range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
    };

    const parser = new Parser();
    parser.parse(textBeforeCursor, false, true);

    if (
        parser.state == State.KEY ||
        parser.state == State.INITIAL ||
        parser.state == State.BOOL_OP_DELIMITER
    ) {
        if (sourceFieldsNames.includes(word.word)) {
            suggestions = getOperatorsSuggestions(word.word, position);
        } else {
            suggestions = getKeySuggestions(range);
        }
    } else if (parser.state == State.KEY_VALUE_OPERATOR) {
        if (VALID_KEY_VALUE_OPERATORS.includes(parser.keyValueOperator)) {
            range.startColumn = word.endColumn - parser.value.length;
            let result = await getValueSuggestions(
                parser.key,
                parser.value,
                range,
            );
            incomplete = result.incomplete;
            suggestions = result.suggestions;
        }
    } else if (
        parser.state == State.VALUE ||
        parser.state == State.DOUBLE_QUOTED_VALUE ||
        parser.state == State.SINGLE_QUOTED_VALUE
    ) {
        range.startColumn = word.endColumn - parser.value.length;
        let quoteChar = "";
        if (parser.state == State.DOUBLE_QUOTED_VALUE) {
            quoteChar = '"';
        } else if (parser.state == State.SINGLE_QUOTED_VALUE) {
            quoteChar = "'";
        }
        let result = await getValueSuggestions(
            parser.key,
            parser.value,
            range,
            quoteChar,
        );
        incomplete = result.incomplete;
        suggestions = result.suggestions;
    } else if (parser.state == State.EXPECT_BOOL_OP) {
        suggestions = getBooleanOperatorsSuggestions(range);
    } else {
    }

    return {
        suggestions: suggestions,
        incomplete: incomplete,
    };
};

const handleMount = (editor) => {
    monaco.languages.registerCompletionItemProvider("flyql", {
        provideCompletionItems: async (model, position) => {
            let word = model.getWordUntilPosition(position);
            const textBeforeCursorRange = {
                startLineNumber: 1,
                endLineNumber: position.lineNumber,
                startColumn: 1,
                endColumn: position.column,
            };
            const textBeforeCursor = model.getValueInRange(
                textBeforeCursorRange,
            );
            return await getSuggestions(word, position, textBeforeCursor);
        },
        triggerCharacters: ["=", "!=", ">", "<", "=~", "!~", " "],
    });
    editorRef.value = editor;
    editor.updateOptions({ placeholder: props.source.generateFlyQLExample() });
    editor.addAction({
        id: "submit",
        label: "submit",
        keybindings: [
            monaco.KeyMod.chord(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter),
            monaco.KeyMod.chord(monaco.KeyMod.Shift | monaco.KeyCode.Enter),
        ],
        run: (e) => {
            emit("submit");
        },
    });
    editor.addAction({
        id: "triggerSugggest",
        label: "triggerSuggest",
        keybindings: [monaco.KeyCode.Tab],
        run: (e) => {
            editor.trigger(
                "triggerSuggest",
                "editor.action.triggerSuggest",
                {},
            );
        },
    });
    monaco.editor.addKeybindingRule({
        keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyF,
        command: null,
    });
    editor.onDidFocusEditorWidget(() => {
        editorFocused.value = true;
    });
    editor.onDidBlurEditorWidget(() => {
        editorFocused.value = false;
    });
    nextTick(() => {
        editor.focus();
    });
    editor
        .getContribution("editor.contrib.suggestController")
        .widget.value._setDetailsVisible(true);
};

const onChange = () => {
    emit("change", code.value);
};

watch(props, () => {
    code.value = props.value;
});
</script>
