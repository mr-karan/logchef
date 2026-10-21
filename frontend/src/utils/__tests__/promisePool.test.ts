import { describe, it, expect, vi } from "vitest";
import { createConcurrencyLimiter, runWithConcurrency } from "@/utils/promisePool";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

describe("runWithConcurrency", () => {
  it("returns results in input order regardless of resolution order", async () => {
    const tasks = [10, 5, 1, 8].map((ms, i) => async () => {
      await new Promise((r) => setTimeout(r, ms));
      return i;
    });
    const results = await runWithConcurrency(tasks, 2);
    expect(results).toEqual([0, 1, 2, 3]);
  });

  it("never runs more than `concurrency` tasks at once", async () => {
    let inFlight = 0;
    let maxInFlight = 0;
    const tasks = Array.from({ length: 12 }, () => async () => {
      inFlight += 1;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await new Promise((r) => setTimeout(r, 5));
      inFlight -= 1;
      return true;
    });

    const results = await runWithConcurrency(tasks, 4);
    expect(results).toHaveLength(12);
    expect(results.every(Boolean)).toBe(true);
    expect(maxInFlight).toBeLessThanOrEqual(4);
    expect(maxInFlight).toBe(4);
  });

  it("starts a queued task as soon as a slot frees up", async () => {
    const gates = Array.from({ length: 4 }, () => deferred<void>());
    const started: number[] = [];
    const tasks = gates.map((gate, i) => async () => {
      started.push(i);
      await gate.promise;
      return i;
    });

    const all = runWithConcurrency(tasks, 2);
    await Promise.resolve();
    // Only the first two start under a cap of 2.
    expect(started).toEqual([0, 1]);

    gates[0].resolve();
    await vi.waitFor(() => expect(started).toContain(2));

    gates[1].resolve();
    gates[2].resolve();
    gates[3].resolve();
    expect(await all).toEqual([0, 1, 2, 3]);
  });

  it("handles an empty task list", async () => {
    expect(await runWithConcurrency([], 4)).toEqual([]);
  });

  it("rejects if a task rejects", async () => {
    const tasks = [
      async () => 1,
      async () => {
        throw new Error("boom");
      },
    ];
    await expect(runWithConcurrency(tasks, 2)).rejects.toThrow("boom");
  });
});

describe("createConcurrencyLimiter", () => {
  it("releases a slot after a rejected task", async () => {
    const run = createConcurrencyLimiter(1);
    const first = run(async () => { throw new Error("failed request"); });
    const second = run(async () => "next result");
    await expect(first).rejects.toThrow("failed request");
    await expect(second).resolves.toBe("next result");
  });
});
