<template>
  <div class="sql-editor-skeleton">
    <div class="sql-editor-skeleton__header">
      <div class="sql-editor-skeleton__spinner" />
      <span>Loading SQL editor…</span>
    </div>
    <div class="sql-editor-skeleton__lines">
      <div class="sql-editor-skeleton__line sql-editor-skeleton__line--long" />
      <div class="sql-editor-skeleton__line sql-editor-skeleton__line--medium" />
      <div class="sql-editor-skeleton__line sql-editor-skeleton__line--short" />
    </div>
  </div>
</template>

<style scoped>
.sql-editor-skeleton {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 0.75rem;
  width: 100%;
  height: 100%;
  min-height: 90px;
  padding: 0.875rem 1rem;
  background: transparent;
}

.sql-editor-skeleton__header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: hsl(var(--muted-foreground));
  font-size: 0.75rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
    "Liberation Mono", "Courier New", monospace;
}

.sql-editor-skeleton__spinner {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: 9999px;
  border: 2px solid hsl(var(--muted));
  border-top-color: hsl(var(--primary));
  animation: spin 0.8s linear infinite;
}

.sql-editor-skeleton__lines {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.sql-editor-skeleton__line {
  height: 0.625rem;
  border-radius: 9999px;
  background: hsl(var(--muted) / 0.8);
}

.sql-editor-skeleton__line--long {
  width: 86%;
}

.sql-editor-skeleton__line--medium {
  width: 62%;
}

.sql-editor-skeleton__line--short {
  width: 44%;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
