/** Run tasks through one shared concurrency limit, including tasks added later. */
export function createConcurrencyLimiter(concurrency: number) {
  const limit = Math.max(1, Math.floor(concurrency));
  let running = 0;
  const waiting: Array<() => void> = [];

  return async function run<T>(task: () => Promise<T>): Promise<T> {
    await new Promise<void>((resolve) => {
      const start = () => {
        running += 1;
        resolve();
      };
      if (running < limit) start();
      else waiting.push(start);
    });

    try {
      return await task();
    } finally {
      running -= 1;
      waiting.shift()?.();
    }
  };
}

/** Run async tasks with bounded concurrency and return results in input order. */
export async function runWithConcurrency<T>(
  tasks: Array<() => Promise<T>>,
  concurrency: number
): Promise<T[]> {
  const run = createConcurrencyLimiter(concurrency);
  return Promise.all(tasks.map(task => run(task)));
}
