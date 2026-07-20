/**
 * Run a set of async tasks with a bounded number in flight at once.
 *
 * A dashboard can hold up to 24 panels; firing every panel's request at the same
 * moment would stampede the backend. This keeps at most `concurrency` tasks
 * running concurrently while still starting the next task the instant a slot
 * frees up. Results are returned in the same order as the input tasks.
 *
 * Tasks are expected to handle their own errors (each dashboard panel resolves
 * to a state object rather than throwing); if a task does reject, the returned
 * promise rejects with that error.
 */
export async function runWithConcurrency<T>(
  tasks: Array<() => Promise<T>>,
  concurrency: number
): Promise<T[]> {
  const results: T[] = new Array(tasks.length);
  if (tasks.length === 0) {
    return results;
  }

  const limit = Math.max(1, Math.floor(concurrency));
  let nextIndex = 0;

  async function worker(): Promise<void> {
    while (true) {
      const current = nextIndex;
      nextIndex += 1;
      if (current >= tasks.length) {
        return;
      }
      results[current] = await tasks[current]();
    }
  }

  const workerCount = Math.min(limit, tasks.length);
  const workers: Array<Promise<void>> = [];
  for (let i = 0; i < workerCount; i += 1) {
    workers.push(worker());
  }
  await Promise.all(workers);
  return results;
}
