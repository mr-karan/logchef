declare module "@/components/editor/QueryEditor.vue" {
  import { DefineComponent } from "vue";

  const QueryEditor: DefineComponent<
    {
      modelValue: string;
      availableFields?: Array<{
        name: string;
        type: string;
        isTimestamp?: boolean;
        isSeverity?: boolean;
      }>;
      placeholder?: string;
      error?: string;
      tableName?: string;
      height?: string | number;
    },
    {},
    any
  >;

  export default QueryEditor;
}
