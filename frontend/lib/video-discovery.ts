import { request } from '@/lib/api';
import type { ApiEnvelope } from '@/lib/types';

export const videoCategories = [
  ['general', '综合'],
  ['animation', '动画'],
  ['games', '游戏'],
  ['music', '音乐'],
  ['knowledge', '知识'],
  ['technology', '科技'],
  ['life', '生活'],
] as const;
export type VideoFeedMode = 'recommended' | 'hot' | 'latest';
export type VideoCardData = {
  video_id: string;
  owner_id: string;
  owner_name: string;
  title: string;
  cover_url: string;
  category: string;
  duration_ms: number;
  published_at: string;
};
export type VideoFeedPage = {
  items: VideoCardData[];
  next_cursor: string;
  has_more: boolean;
  mode: VideoFeedMode;
  degraded: boolean;
  delivery_token: string;
};
export const videoDiscovery = {
  page: (
    base: string,
    mode: VideoFeedMode,
    category: string,
    cursor: string,
    signal?: AbortSignal,
  ) =>
    request<ApiEnvelope<VideoFeedPage>>(
      base,
      `/api/v1/feeds/videos?${new URLSearchParams({ sort: mode, category, cursor, limit: '20' })}`,
      { signal, cache: 'no-store' },
    ),
  behavior: (
    base: string,
    video: string,
    token: string,
    kind: 'impression' | 'click',
  ) =>
    request(base, `/api/v1/videos/${video}/behavior`, {
      method: 'POST',
      keepalive: true,
      body: JSON.stringify({ kind, delivery_token: token }),
    }),
};
