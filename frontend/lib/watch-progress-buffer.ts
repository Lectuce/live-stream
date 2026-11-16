// A failed write may be retried unchanged or superseded by a newer sample.
// Sequence numbers belong to samples, not HTTP attempts. Rewinds are valid.
export class WatchProgressBuffer {
  private sequence = 0;
  private lastPosition: number | null = null;
  pending: { seq: number; position_ms: number } | null = null;

  sample(position: number) {
    if (
      !Number.isSafeInteger(position) ||
      position < 0 ||
      this.lastPosition === position
    )
      return;
    this.lastPosition = position;
    this.pending = { seq: ++this.sequence, position_ms: position };
  }

  acknowledge(seq: number) {
    if (this.pending?.seq === seq) this.pending = null;
  }
}

export function resumePosition(position: number, duration: number) {
  if (!Number.isFinite(duration) || duration <= 0 || !Number.isFinite(position))
    return 0;
  const bounded = Math.max(0, Math.min(position, duration));
  // Very short videos need a proportionally smaller end threshold.
  return bounded >= duration - Math.min(5000, duration * 0.05) ? 0 : bounded;
}
