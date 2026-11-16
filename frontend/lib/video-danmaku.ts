export interface VideoDanmaku {
  danmaku_id: string;
  video_id: string;
  user_id: string;
  position_ms: number;
  content: string;
  color: string;
  version: string;
}
export function danmakuRequestID() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join(
    '',
  );
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export function scheduleDanmakus(
  items: VideoDanmaku[],
  lanes: number,
  words: string[],
  users: string[],
) {
  const seen = new Set<string>();
  const slots = new Map<string, VideoDanmaku>();
  const result: { item: VideoDanmaku; lane: number }[] = [];
  const sorted = [...items].sort(
    (a, b) =>
      a.position_ms - b.position_ms ||
      (BigInt(a.danmaku_id) < BigInt(b.danmaku_id) ? -1 : 1),
  );
  for (const item of sorted) {
    if (seen.has(item.danmaku_id)) continue;
    seen.add(item.danmaku_id);
    if (
      users.includes(item.user_id) ||
      words.some((w) => item.content.includes(w))
    )
      continue;
    const lane = Number(BigInt(item.danmaku_id) % BigInt(lanes));
    const key = `${Math.floor(item.position_ms / 8000)}:${lane}`;
    if (!slots.has(key)) slots.set(key, item);
  }
  for (const item of slots.values()) {
    const lane = Number(BigInt(item.danmaku_id) % BigInt(lanes));
    const previous = slots.get(
      `${Math.floor(item.position_ms / 8000) - 1}:${lane}`,
    );
    if (previous && previous.position_ms + 8000 > item.position_ms) continue;
    result.push({ item, lane });
  }
  return result;
}
export function timelineRows(
  items: VideoDanmaku[],
  time: number,
  lanes: number,
  words: string[],
  users: string[],
) {
  return scheduleDanmakus(items, lanes, words, users)
    .filter(
      ({ item }) => time >= item.position_ms && time < item.position_ms + 8000,
    )
    .map((row) => ({ ...row, progress: (time - row.item.position_ms) / 8000 }));
}
