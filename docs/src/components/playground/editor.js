const input = document.querySelector('#query');
const highlight = document.querySelector('#highlight');
const status = document.querySelector('#status');
const engines = [...document.querySelectorAll('[data-engine]')];
const chips = document.querySelectorAll('[data-query]');
let revision = 0;
let timer;
let worker;

function paint() {
  const tokens = input.value.split(/("(?:\\.|[^"\\])*"?|'(?:\\.|[^'\\])*'?|\b(?:and|or|as|by|sort|limit|desc|asc|count|time)\b|\b\d+\b)/gi);
  highlight.replaceChildren(...tokens.map((text, index) => {
    if (index % 2 === 0) return document.createTextNode(text);
    const span = document.createElement('span');
    span.className = /^["'\d]/.test(text) ? 'literal' : 'keyword';
    span.textContent = text;
    return span;
  }), document.createTextNode('\n'));
  highlight.scrollTop = input.scrollTop;
  chips.forEach(chip => chip.setAttribute('aria-pressed', chip.dataset.query === input.value));
}

function failed() {
  worker?.terminate();
  worker = undefined;
  status.textContent = 'Compiler unavailable. Edit the query to retry.';
  engines.forEach(card => { card.querySelector('pre').textContent = 'Compiler unavailable'; });
}

function render({ data }) {
  if (data.id !== revision) return;
  if (data.failed) return failed();
  let count = 0;
  engines.forEach(card => {
    const { query, error } = data.result[card.dataset.engine];
    const position = error?.position;
    const where = position ? `Line ${position.line}, column ${position.column}\n` : '';
    card.querySelector('pre').textContent = error ? where + error.message : query;
    card.toggleAttribute('data-error', !!error);
    card.querySelector('button').disabled = !!error;
    if (!error) count++;
  });
  if (!matchMedia('(prefers-reduced-motion: reduce)').matches) {
    document.querySelector('.wires').animate([{ opacity: .25 }, { opacity: 1 }], { duration: 350 });
  }
  status.textContent = `Compiled for ${count} of 3 engines`;
}

function update() {
  const id = ++revision;
  clearTimeout(timer);
  paint();
  engines.forEach(card => { card.querySelector('button').disabled = true; });
  status.textContent = worker ? 'Compiling…' : 'Loading the compiler…';
  timer = setTimeout(() => {
    try {
      worker ??= new Worker('/playground-runtime/worker.js');
      worker.onmessage = render;
      worker.onerror = failed;
      worker.postMessage({ id, query: input.value });
    } catch { failed(); }
  }, 130);
}

async function copy(text, message) {
  try {
    await navigator.clipboard.writeText(text);
    status.textContent = message;
  } catch {
    status.textContent = 'Clipboard unavailable. Select the text to copy it.';
  }
}

input.readOnly = false;
input.parentElement.classList.add('enhanced');
paint();
input.addEventListener('input', update);
input.addEventListener('scroll', () => { highlight.scrollTop = input.scrollTop; });
chips.forEach(chip => {
  chip.disabled = false;
  chip.onclick = () => { input.value = chip.dataset.query; update(); };
});
engines.forEach(card => {
  const button = card.querySelector('button');
  button.disabled = false;
  button.onclick = () => copy(card.querySelector('pre').textContent, 'Query copied');
});
const share = document.querySelector('#share');
share.disabled = false;
share.onclick = () => {
  const url = new URL(location.href);
  url.hash = new URLSearchParams({ q: input.value }).toString();
  history.replaceState(null, '', url);
  copy(url.href, 'Share link copied');
};
const shared = new URLSearchParams(location.hash.slice(1)).get('q');
if (shared !== null && shared.length <= input.maxLength) {
  input.value = shared;
  update();
}
