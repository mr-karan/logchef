import { describe, expect, it } from 'vitest';
import { isClickToLoadField, isFilterableField, isPriorityField } from '../sourceFields';

describe('source field loading', () => {
  it('loads the values of a Loki stream label on click', () => {
    expect(isPriorityField('label')).toBe(false);
    expect(isClickToLoadField('label')).toBe(true);
    expect(isFilterableField('label')).toBe(true);
  });
});
