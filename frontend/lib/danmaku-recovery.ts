// A single cancellable recovery task. Live messages never advance this cursor.
export class DanmakuRecovery<T> {
  private pending: number | null = null;
  private cursor: number | null = null;
  private target = 0;
  private running = false;
  private stopped = false;
  private sampled = false;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private controller: AbortController | null = null;
  private fetchPage: (cursor: number, signal: AbortSignal) => Promise<{items: T[]; next_seq: number; has_more: boolean}>;
  private append: (items: T[]) => void;
  private onError: (error: unknown) => void;
  private random: () => number;
  constructor(fetchPage: DanmakuRecovery<T>['fetchPage'], append: (items: T[]) => void,
    onError: (error: unknown) => void, random: () => number = Math.random) {
    this.fetchPage=fetchPage; this.append=append; this.onError=onError; this.random=random;
  }
  get checkpoint(): number | null {
    if (this.pending === null) return this.cursor;
    return Math.min(this.pending, this.cursor ?? this.pending);
  }
  request(after: number, target = 0) {
    if (this.stopped) return;
    this.pending = Math.min(this.pending ?? after, after);
    this.target = Math.max(this.target, target);
    this.schedule(this.random() * 2000);
  }
  mode(sampled: boolean, after: number) {
    if (this.sampled === sampled) return;
    this.sampled = sampled;
    this.request(this.cursor ?? after);
  }
  stop() {
    this.stopped = true;
    if (this.timer !== null) clearTimeout(this.timer);
    this.controller?.abort();
  }
  private schedule(delay: number) {
    if (this.running || this.timer !== null || this.stopped) return;
    this.timer = setTimeout(() => { this.timer = null; void this.run(); }, delay);
  }
  private async run() {
    if (this.stopped) return;
    this.running = true;
    this.controller = new AbortController();
    if (this.pending !== null) {
      this.cursor = Math.min(this.cursor ?? this.pending, this.pending);
      this.pending = null;
    }
    const timeout = setTimeout(() => this.controller?.abort(), 5000);
    let more = false;
    let delay = 3000 + this.random() * 2000;
    try {
      for (let page = 0; page < 2; page++) {
        const result = await this.fetchPage(this.cursor ?? 0, this.controller.signal);
        if (this.stopped) return;
        this.append(result.items || []);
        this.cursor = Math.max(this.cursor ?? 0, result.next_seq || 0);
        more = result.has_more || this.cursor < this.target;
        if (!result.has_more) break;
      }
      this.attempt = 0;
    } catch (error) {
      if (this.stopped) return;
      more = true;
      const retry = (error as {retryAfterMilliseconds?: number})?.retryAfterMilliseconds || 0;
      delay = Math.max(retry, Math.min(30000, 1000 * 2 ** Math.min(this.attempt++, 5))) + this.random() * 1000;
      this.onError(error);
    } finally {
      clearTimeout(timeout);
      this.running = false;
      if (more || this.pending !== null || this.sampled) this.schedule(delay);
      else { this.cursor = null; this.target = 0; }
    }
  }
}
