export function formatSpeed(bytes: number) {
  if (bytes < 1024) return `${Math.round(bytes)} B/s`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB/s`;
  return `${Math.round(bytes / (1024 * 1024))} MB/s`;
}

export function parseLatencyMs(latency: unknown) {
  if (typeof latency === 'number') return latency;
  if (typeof latency !== 'string') return 0;
  const match = latency.match(/^(\d+(?:\.\d+)?)\s*(ns|us|µs|ms|s)?$/i);
  if (!match) return 0;
  const value = parseFloat(match[1]);
  const unit = (match[2] || 'ms').toLowerCase();
  if (unit === 's') return value * 1000;
  if (unit === 'us' || unit === 'µs') return value / 1000;
  if (unit === 'ns') return value / 1000000;
  return value;
}

export function extractErrorMessage(err: any): string {
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (typeof err?.message === 'string' && err.message.trim()) return err.message;
  if (typeof err?.cause === 'string' && err.cause.trim()) return err.cause;
  if (typeof err?.cause?.message === 'string' && err.cause.message.trim()) return err.cause.message;
  try { return JSON.stringify(err); }
  catch { return String(err); }
}

// Backend wraps probe timeouts with a "timeout: ..." prefix (net.Error.Timeout
// classification) so the UI can show 超时 instead of a generic failure.
export function isTimeoutError(message: string): boolean {
  return /^timeout\b/i.test(message) || /\bi\/o timeout\b/i.test(message);
}

export function latencyColor(latency: number | null): 'success.main' | 'warning.main' | 'error.main' {
  if (latency === null) return 'error.main';
  if (latency < 300) return 'success.main';
  if (latency < 1000) return 'warning.main';
  return 'error.main';
}

// runWithConcurrency runs probe tasks with a bounded number of workers so a
// batch test cannot flood the backend. Each task owns its error handling;
// isCancelled lets callers stop scheduling new tasks (in-flight ones finish).
export async function runWithConcurrency(
  tasks: (() => Promise<void>)[],
  limit: number,
  isCancelled?: () => boolean,
): Promise<void> {
  let next = 0;
  const workers = Array.from({ length: Math.max(1, Math.min(limit, tasks.length)) }, async () => {
    while (next < tasks.length) {
      if (isCancelled?.()) return;
      const index = next++;
      await tasks[index]();
    }
  });
  await Promise.all(workers);
}

export function splitListInput(value: string) {
  return value.split(/[\n,;]+/).map(s => s.trim()).filter(Boolean);
}

export function joinListInput(items: string[] | undefined) {
  return (items || []).join('\n');
}
