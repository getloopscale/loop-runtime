export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function retry<T>(
  fn: () => Promise<T>,
  opts: { attempts: number; delayMs: number } = { attempts: 3, delayMs: 1000 },
): Promise<T> {
  let lastError: Error | undefined;

  for (let i = 0; i < opts.attempts; i++) {
    try {
      return await fn();
    } catch (e) {
      lastError = e instanceof Error ? e : new Error(String(e));
      if (i < opts.attempts - 1) {
        await sleep(opts.delayMs * Math.pow(2, i));
      }
    }
  }

  throw lastError;
}
