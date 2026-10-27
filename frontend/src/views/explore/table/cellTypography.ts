export type CellValueKind = 'text' | 'identifier' | 'number' | 'structured';

const VALUE_SHAPES: { kind: CellValueKind; pattern: RegExp }[] = [
  { kind: 'structured', pattern: /^(?:\{.*\}|\[.*\])$/s },
  { kind: 'identifier', pattern: /^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/i },
  { kind: 'identifier', pattern: /^\+\d{7,15}$/ },
  { kind: 'number', pattern: /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i },
  { kind: 'identifier', pattern: /^(?:0x)?[\da-f]{16,}$/i },
  { kind: 'identifier', pattern: /^(?=\+|\()(?=(?:\D*\d){7,15}\D*$)\+?[\d(][\d ().-]*\d$/ },
  { kind: 'identifier', pattern: /^(?=.{20,}$)(?=.*[a-z])(?=.*[A-Z])(?!\/)(?=.*\+|.*=$)[A-Za-z\d+/_-]+={0,2}$/ },
  { kind: 'identifier', pattern: /^(?=.{64,}$)(?=.*[a-z])(?=.*[A-Z])(?=.*\d)[A-Za-z\d_-]+$/ },
];

export function classifyCellValue(value: unknown): CellValueKind {
  if (typeof value === 'number') return 'number';
  if (typeof value !== 'string') return 'text';
  const text = value.trim();
  return VALUE_SHAPES.find(({ pattern }) => pattern.test(text))?.kind ?? 'text';
}
