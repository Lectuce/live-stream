import { request } from '@/lib/api';
import type { ApiEnvelope, Page } from '@/lib/types';

export type TargetType = 'video' | 'post' | 'comment' | 'danmaku';
export interface LikeState {
  liked: boolean;
  like_count: number;
}
export interface FavoriteFolder {
  folder_id: string;
  name: string;
  is_default: boolean;
}
export interface FavoriteItem {
  item_id: string;
  video_id: string;
  title: string;
  cover_url: string;
  available: boolean;
}
export interface Notice {
  notification_id: string;
  actor_name: string;
  kind: string;
  url: string;
  read_at: string | null;
  created_at: string;
}
export interface Report {
  report_id: string;
  target_type: TargetType;
  target_id: string;
  reason: string;
  status: string;
  resolution: string;
  preview: string;
  version: string;
  url: string;
  created_at: string;
}
export interface ReportAction {
  action_id: string;
  actor_id: string;
  action: string;
  reason: string;
  created_at: string;
}
const body = (method: string, value?: object): RequestInit => ({
  method,
  ...(value ? { body: JSON.stringify(value) } : {}),
});
const query = (value: Record<string, string>) => new URLSearchParams(value);
export const interactions = {
  like: (
    base: string,
    kind: 'video' | 'post',
    id: string,
    liked?: boolean,
    signal?: AbortSignal,
  ) =>
    request<ApiEnvelope<LikeState>>(
      base,
      `/api/v1/${kind === 'video' ? 'videos' : 'posts'}/${id}/like`,
      {
        ...body(liked === undefined ? 'GET' : liked ? 'PUT' : 'DELETE'),
        signal,
        cache: 'no-store',
      },
    ),
  folders: (base: string, signal?: AbortSignal) =>
    request<ApiEnvelope<FavoriteFolder[]>>(
      base,
      '/api/v1/me/favorite-folders',
      { signal, cache: 'no-store' },
    ),
  createFolder: (base: string, name: string) =>
    request<ApiEnvelope<FavoriteFolder>>(
      base,
      '/api/v1/me/favorite-folders',
      body('POST', { name }),
    ),
  changeFolder: (base: string, id: string, name?: string) =>
    request(
      base,
      `/api/v1/me/favorite-folders/${id}`,
      body(
        name === undefined ? 'DELETE' : 'PATCH',
        name === undefined ? undefined : { name },
      ),
    ),
  favorites: (base: string, id: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<Page<FavoriteItem>>>(
      base,
      `/api/v1/me/favorite-folders/${id}/items?${query({ cursor })}`,
      { signal, cache: 'no-store' },
    ),
  membership: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<string[]>>(base, `/api/v1/me/favorites/${id}`, {
      signal,
      cache: 'no-store',
    }),
  favorite: (base: string, folder: string, video: string, save: boolean) =>
    request(
      base,
      `/api/v1/me/favorite-folders/${folder}/items/${video}`,
      body(save ? 'PUT' : 'DELETE'),
    ),
  notifications: (base: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<Page<Notice>>>(
      base,
      `/api/v1/me/notifications?${query({ cursor })}`,
      { signal, cache: 'no-store' },
    ),
  unread: (base: string, signal?: AbortSignal) =>
    request<ApiEnvelope<{ count: number }>>(
      base,
      '/api/v1/me/notifications/unread',
      { signal, cache: 'no-store' },
    ),
  read: (base: string, id: string, all = false) =>
    request(
      base,
      all
        ? '/api/v1/me/notifications/read'
        : `/api/v1/me/notifications/${id}/read`,
      body(all ? 'POST' : 'PUT', all ? { before: id } : undefined),
    ),
  report: (base: string, kind: TargetType, id: string, reason: string) =>
    request(
      base,
      '/api/v1/reports',
      body('POST', { target_type: kind, target_id: id, reason }),
    ),
  reports: (
    base: string,
    status: string,
    kind: string,
    cursor = '',
    signal?: AbortSignal,
  ) =>
    request<ApiEnvelope<Page<Report>>>(
      base,
      `/api/v1/admin/reports?${query({ status, target_type: kind, cursor })}`,
      { signal, cache: 'no-store' },
    ),
  resolve: (
    base: string,
    id: string,
    version: string,
    action: string,
    reason: string,
  ) =>
    request(
      base,
      `/api/v1/admin/reports/${id}/resolve`,
      body('POST', { version, action, reason }),
    ),
  history: (base: string, id: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<Page<ReportAction>>>(
      base,
      `/api/v1/admin/reports/${id}/history?${query({ cursor })}`,
      { signal, cache: 'no-store' },
    ),
};
export const interactionError = (err: unknown) =>
  err instanceof Error ? err.message : '操作失败，请稍后重试';
