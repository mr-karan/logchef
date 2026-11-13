const NativeNumberFormat = Intl.NumberFormat;
Intl.NumberFormat = new Proxy(NativeNumberFormat, {
  construct(target, args) { return Reflect.construct(target, [args[0] ?? "en-US", args[1]]); },
  apply(target, self, args) { return Reflect.apply(target, self, [args[0] ?? "en-US", args[1]]); },
});

// Vitest global setup.
//
// Node 22+ ships an experimental built-in `localStorage`/`sessionStorage`
// global that is disabled unless `--localstorage-file` is passed; it then
// shadows the jsdom-provided Web Storage, so `localStorage` reads as undefined
// inside tests (breaking anything that persists to it). Install a small
// in-memory Storage so Web Storage behaves consistently across Node versions
// and CI regardless of that experimental global.

class MemoryStorage implements Storage {
  private store = new Map<string, string>()

  get length(): number {
    return this.store.size
  }
  clear(): void {
    this.store.clear()
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null
  }
  removeItem(key: string): void {
    this.store.delete(key)
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value))
  }
}

for (const name of ["localStorage", "sessionStorage"] as const) {
  Object.defineProperty(globalThis, name, {
    value: new MemoryStorage(),
    writable: true,
    configurable: true,
  })
}
