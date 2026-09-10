(async () => {
  const afterPaint = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const measure = async action => {
    const start = performance.now();
    action();
    await afterPaint();
    return Math.round(performance.now() - start);
  };
  const samples = { expand: [], collapse: [], filter: [], clearFilter: [], nextPage: [], previousPage: [] };
  const findButton = name => {
    const button = [...document.querySelectorAll('button')].find(element => element.textContent.trim() === name);
    if (!button || button.disabled) throw new Error(`Button is missing or disabled: ${name}`);
    return button;
  };
  const input = document.querySelector('input[aria-label="Search in all columns"]');
  if (!input) throw new Error('Open Table view before benchmarking');
  if (input.value) throw new Error('Clear the table filter before benchmarking');
  const setFilter = value => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  };
  const toggleRow = () => {
    const cell = document.querySelector('tbody tr td');
    if (!cell) throw new Error('No log row was rendered');
    cell.click();
  };
  for (let i = 0; i < 6; i++) {
    samples.expand.push(await measure(toggleRow));
    if (!document.querySelector('.expanded-json-row')) throw new Error('Row did not expand');
    samples.collapse.push(await measure(toggleRow));
    samples.filter.push(await measure(() => setFilter('Synthetic log message 99')));
    samples.clearFilter.push(await measure(() => setFilter('')));
    samples.nextPage.push(await measure(() => findButton('Go to next page').click()));
    samples.previousPage.push(await measure(() => findButton('Go to previous page').click()));
  }
  return {
    samples,
    renderedRows: document.querySelectorAll('tbody tr').length,
    domNodes: document.querySelectorAll('*').length,
    tableButtons: document.querySelectorAll('tbody button').length,
  };
})()
