// Run with agent-browser eval --stdin on the local Vite frontend.
(async () => {
  const { formatTimestamp } = await import('/src/lib/utils.ts');
  const values = Array.from({ length: 1000 }, (_, index) =>
    new Date(1767225600000 + index * 1234).toISOString());
  const samples = { utc: [], local: [] };
  for (const timezone of Object.keys(samples)) {
    for (let run = 0; run < 15; run++) {
      const start = performance.now();
      for (const value of values) formatTimestamp(value, timezone);
      samples[timezone].push(performance.now() - start);
    }
  }
  return { count: values.length, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, samples };
})()
