importScripts('wasm_exec.js');

const go = new Go();
const ready = WebAssembly.instantiateStreaming(fetch('compiler.wasm'), go.importObject)
  .then(({ instance }) => { go.run(instance); });

self.onmessage = async ({ data: { id, query } }) => {
  try {
    await ready;
    self.postMessage({ id, result: JSON.parse(self.compile(query, { interval: '5m' })) });
  } catch {
    self.postMessage({ id, failed: true });
  }
};
