import type {
  ApiEnvelope,
  HistoryPage,
  LiveFeedPage,
  ReplayPage,
  Room,
  User,
  Video,
  VideoPage,
} from '@/lib/types';
export class ApiError extends Error {
  constructor(message: string, public status: number) { super(message); }
}

async function request<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    const headers = new Headers(init?.headers);
    headers.set('Content-Type', 'application/json');
    response = await fetch(`${baseUrl}${path}`, {
      ...init,
      credentials: 'include',
      headers,
    });
  } catch {
    throw new ApiError('暂时无法加载内容，请稍后重试', 0);
  }
  const body = await response.json().catch(() => ({ message: `HTTP ${response.status}` })) as { message?: string; msg?: string };
  if (!response.ok) throw new ApiError(body.message || body.msg || '请求失败', response.status);
  return body as T;
}

export const api = {
  listRooms: (base: string) => request<ApiEnvelope<Room[]>>(base, '/api/v1/rooms'),
  roomDetail: (
    base: string,
    id: number,
    signal?: AbortSignal,
  ) => request<ApiEnvelope<Room>>(
    base,
    `/api/v1/rooms/${id}`,
    { signal },
  ),
  myRoom: (base: string) => request<ApiEnvelope<Room | null>>(base, '/api/v1/rooms/mine'),
  me: (base: string) => request<ApiEnvelope<User>>(base, '/api/v1/auth/me'),
  login: (base: string, email: string, password: string) => request<ApiEnvelope<User>>(base, '/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  register: (base: string, username: string, email: string, password: string) => request<ApiEnvelope<User>>(base, '/api/v1/auth/register', { method: 'POST', body: JSON.stringify({ username, email, password }) }),
  logout: (base: string) => request<{ message: string }>(base, '/api/v1/auth/logout', { method: 'POST' }),
  createRoom: (base: string, title: string) => request<ApiEnvelope<Room>>(base, '/api/v1/rooms', { method: 'POST', body: JSON.stringify({ title }) }),
  updateRoom: (base: string, id: number, title: string) => request<ApiEnvelope<Room>>(base, `/api/v1/rooms/${id}`, { method: 'PATCH', body: JSON.stringify({ room_title: title }) }),
  startRoom: (base: string, id: number, title: string) => request<{ message: string }>(base, `/api/v1/rooms/${id}/start`, { method: 'POST', body: JSON.stringify({ room_title: title }) }),
  stopRoom: (base: string, id: number) => request<{ message: string }>(base, `/api/v1/rooms/${id}/stop`, { method: 'POST', body: '{}' }),
  history: (base: string, id: number, before = 0, limit = 50) => request<ApiEnvelope<HistoryPage>>(base, `/api/v1/rooms/${id}/messages?before_id=${before}&limit=${limit}`),
  replay: (base: string, id: number, afterSeq: number, limit = 200) => request<ApiEnvelope<ReplayPage>>(base, `/api/v1/rooms/${id}/messages/replay?after_seq=${afterSeq}&limit=${limit}`),

  liveFeed: (
    base: string,
    cursor = '',
    limit = 20,
    signal?: AbortSignal,
  ) => {
    const params = new URLSearchParams({
      limit: String(limit),
    });

    if (cursor) params.set('cursor', cursor);

    return request<ApiEnvelope<LiveFeedPage>>(
      base,
      `/api/v1/feeds/live/hot?${params.toString()}`,
      { signal },
    );
  },

  videoDetail: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<Video>>(
      base,
      `/api/v1/videos/${encodeURIComponent(id)}`,
      { signal },
    ),

  videoList: (base: string, cursor = '', signal?: AbortSignal) => {
    const params = new URLSearchParams({ limit: '20' });
    if (cursor) params.set('cursor', cursor);

    return request<ApiEnvelope<VideoPage>>(
      base,
      `/api/v1/videos?${params.toString()}`,
      { signal },
    );
  },



};
