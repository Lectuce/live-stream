import { request } from '@/lib/api';
import type { ApiEnvelope, Page } from '@/lib/types';

export type WatchProgress = {
  video_id: string;
  position_ms: number;
  duration_ms: number;
  history_epoch: string;
  session_version: string;
  session_id: string;
  last_seq: string;
  has_history: boolean;
};
export type WatchHistoryItem = {
  id: string;
  video_id: string;
  position_ms: number;
  last_watched_at: string;
  title: string;
  cover_url: string;
  duration_ms: number;
  owner_id: string;
  owner_name: string;
  available: boolean;
};
export const watchHistory = {
  progress: (base: string, video: string, signal?: AbortSignal) =>
    request<ApiEnvelope<WatchProgress>>(base, `/api/v1/me/history/${video}`, {
      signal,
      cache: 'no-store',
    }),
  list: (base: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<Page<WatchHistoryItem>>>(
      base,
      `/api/v1/me/history?${new URLSearchParams({ cursor, limit: '20' })}`,
      { signal, cache: 'no-store' },
    ),
  remove: (base: string, video?: string) =>
    request<{ message: string }>(
      base,
      `/api/v1/me/history${video ? `/${video}` : ''}`,
      { method: 'DELETE' },
    ),
};

export function watchTime(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(seconds / 3600);
  return `${h ? `${h}:` : ''}${String(Math.floor(seconds / 60) % 60).padStart(h ? 2 : 1, '0')}:${String(seconds % 60).padStart(2, '0')}`;
}
