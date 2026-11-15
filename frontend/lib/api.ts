import type {
  ApiEnvelope,
  HistoryPage,
  LiveFeedPage,
  ReplayPage,
  Room,
  User,
  Video,
  VideoPage,
  OwnerVideo,
  OwnerVideoPage,
  ReviewDetail,
  ReviewPage,
  PublicUser,
  FollowState,
  FollowingFeedPage,
  Post,
  PostPage,
  PostImage,
  CommentPage,
  CommentTargetType,
  Page, CommentReview, CommentReport,
} from '@/lib/types';
export class ApiError extends Error {
  constructor(message: string, public status: number) { super(message); }
}

export async function request<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    const headers = new Headers(init?.headers);
    if (!(init?.body instanceof FormData)) {
      headers.set('Content-Type', 'application/json');
    }
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
  myComments: (base: string, targetType: CommentTargetType, targetID: string, cursor = '', signal?: AbortSignal) => request<ApiEnvelope<CommentPage>>(base, `/api/v1/comments/mine?${new URLSearchParams({target_type: targetType, target_id: targetID, cursor})}`, {signal, cache:'no-store'}),
  editComment: (base: string, id: string, version: string, content: string) => request<{message:string}>(base, `/api/v1/comments/${id}`, {method:'PATCH', body:JSON.stringify({version,content})}),
  likeComment: (base: string, id: string, liked: boolean) => request<ApiEnvelope<{liked:boolean;like_count:number}>>(base, `/api/v1/comments/${id}/like`, {method:liked?'PUT':'DELETE'}),
  reportComment: (base: string, id: string, reason: string) => request<{message:string}>(base, `/api/v1/comments/${id}/reports`, {method:'POST',body:JSON.stringify({reason})}),
  reviewComments: (base: string, status: string, cursor = '', signal?: AbortSignal) => request<ApiEnvelope<CommentPage>>(base, `/api/v1/admin/comments?${new URLSearchParams({status,cursor})}`, {signal,cache:'no-store'}),
  reviewComment: (base: string, id: string, version: string, decision: string, reason: string) => request<{message:string}>(base, `/api/v1/admin/comments/${id}/review`, {method:'POST',body:JSON.stringify({version,decision,reason})}),
  commentHistory: (base: string, id: string, cursor = '', signal?: AbortSignal) => request<ApiEnvelope<Page<CommentReview>>>(base, `/api/v1/admin/comments/${id}/history?${new URLSearchParams({cursor})}`, {signal,cache:'no-store'}),
  commentReports: (base: string, status: string, cursor = '', signal?: AbortSignal) => request<ApiEnvelope<Page<CommentReport>>>(base, `/api/v1/admin/comment-reports?${new URLSearchParams({status,cursor})}`, {signal,cache:'no-store'}),
  resolveCommentReport: (base: string, id: string, status: string, reason: string) => request<{message:string}>(base, `/api/v1/admin/comment-reports/${id}/resolve`, {method:'POST',body:JSON.stringify({status,reason})}),

  comments: (base: string, targetType: CommentTargetType, targetID: string, root = '', cursor = '', signal?: AbortSignal, sort: 'latest' | 'hot' = 'latest') =>
    request<ApiEnvelope<CommentPage>>(base, `/api/v1/${targetType === 'video' ? 'videos' : 'posts'}/${encodeURIComponent(targetID)}/comments${root ? `/${encodeURIComponent(root)}/replies` : ''}?${new URLSearchParams({ cursor, limit: '20', sort })}`, { signal, cache: 'no-store' }),
  createComment: (base: string, targetType: CommentTargetType, targetID: string, content: string, parentID: string, requestID: string, signal?: AbortSignal) =>
    request<ApiEnvelope<{ comment_id: string }>>(base, `/api/v1/${targetType === 'video' ? 'videos' : 'posts'}/${encodeURIComponent(targetID)}/comments`, { method: 'POST', body: JSON.stringify({ content, parent_id: parentID, request_id: requestID }), signal }),
  deleteComment: (base: string, targetType: CommentTargetType, targetID: string, id: string, signal?: AbortSignal) =>
    request<{ message: string }>(base, `/api/v1/${targetType === 'video' ? 'videos' : 'posts'}/${encodeURIComponent(targetID)}/comments/${encodeURIComponent(id)}`, { method: 'DELETE', signal }),
  followingFeed: (base: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<FollowingFeedPage>>(base, `/api/v1/feeds/following?${new URLSearchParams({ cursor, limit: '20' })}`, { signal, cache: 'no-store' }),
  createPost: (base: string, signal?: AbortSignal) => request<ApiEnvelope<Post>>(base, '/api/v1/posts', { method: 'POST', signal }),
  post: (base: string, id: string, mine = false, signal?: AbortSignal) => request<ApiEnvelope<Post>>(base, `/api/v1/posts/${mine ? 'mine/' : ''}${encodeURIComponent(id)}`, { signal, cache: 'no-store' }),
  posts: (base: string, author: string, cursor = '', signal?: AbortSignal) => request<ApiEnvelope<PostPage>>(base, `${author ? `/api/v1/users/${encodeURIComponent(author)}/posts` : '/api/v1/posts/mine'}?${new URLSearchParams({ cursor, limit: '20' })}`, { signal, cache: 'no-store' }),
  editPost: (base: string, id: string, version: string, content: string, imageIDs: string[], signal?: AbortSignal) => request<ApiEnvelope<Post>>(base, `/api/v1/posts/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ version, content, image_ids: imageIDs }), signal }),
  publishPost: (base: string, id: string, version: string, signal?: AbortSignal) => request<ApiEnvelope<Post>>(base, `/api/v1/posts/${encodeURIComponent(id)}/publish`, { method: 'POST', body: JSON.stringify({ version }), signal }),
  deletePost: (base: string, id: string) => request<{ message: string }>(base, `/api/v1/posts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  uploadPostImage: (base: string, file: File, signal?: AbortSignal) => {
    const body = new FormData(); body.append('file', file);
    return request<ApiEnvelope<PostImage>>(base, '/api/v1/posts/images', { method: 'POST', body, signal });
  },
  publicUser: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<PublicUser>>(base, `/api/v1/users/${encodeURIComponent(id)}`, { signal, cache: 'no-store' }),
  authorVideos: (base: string, id: string, cursor = '', signal?: AbortSignal) =>
    request<ApiEnvelope<VideoPage>>(base, `/api/v1/users/${encodeURIComponent(id)}/videos?${new URLSearchParams({ cursor, limit: '20' })}`, { signal, cache: 'no-store' }),
  followState: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<FollowState>>(base, `/api/v1/users/${encodeURIComponent(id)}/follow`, { signal, cache: 'no-store' }),
  setFollow: (base: string, id: string, follow: boolean, signal?: AbortSignal) =>
    request<ApiEnvelope<FollowState>>(base, `/api/v1/users/${encodeURIComponent(id)}/follow`, { method: follow ? 'PUT' : 'DELETE', signal }),
  reviewList: (base: string, status: string, cursor: string, signal?: AbortSignal) =>
    request<ApiEnvelope<ReviewPage>>(base, `/api/v1/admin/videos?${new URLSearchParams({ status, cursor, limit: '20' })}`, { signal, cache: 'no-store' }),
  reviewDetail: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<ReviewDetail>>(base, `/api/v1/admin/videos/${encodeURIComponent(id)}`, { signal, cache: 'no-store' }),
  submitReview: (base: string, id: string, version: string, decision: 'approved' | 'rejected', reason: string) =>
    request<{ message: string }>(base, `/api/v1/admin/videos/${encodeURIComponent(id)}/review`, {
      method: 'POST', body: JSON.stringify({ content_version: version, decision, reason }),
    }),
  editVideo: (base: string, id: string, title: string, description: string) =>
    request<ApiEnvelope<OwnerVideo>>(base, `/api/v1/videos/${encodeURIComponent(id)}`, {
      method: 'PATCH', body: JSON.stringify({ title, description }),
    }),
  unpublishVideo: (base: string, id: string) =>
    request<ApiEnvelope<OwnerVideo>>(base, `/api/v1/videos/${encodeURIComponent(id)}/unpublish`, {
      method: 'POST', body: '{}',
    }),
  deleteVideo: (base: string, id: string) =>
    request<{ message: string }>(base, `/api/v1/videos/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  myVideos: (base: string, cursor = '', signal?: AbortSignal) => {
    const params = new URLSearchParams({ limit: '20' });
    if (cursor) params.set('cursor', cursor);
    return request<ApiEnvelope<OwnerVideoPage>>(base, `/api/v1/videos/mine?${params}`, {
      signal, cache: 'no-store',
    });
  },
  myVideoDetail: (base: string, id: string, signal?: AbortSignal) =>
    request<ApiEnvelope<OwnerVideo>>(base, `/api/v1/videos/mine/${encodeURIComponent(id)}`, {
      signal, cache: 'no-store',
    }),
  createVideo: (base: string, title: string, description: string) =>
    request<ApiEnvelope<Video>>(base, '/api/v1/videos', {
      method: 'POST', body: JSON.stringify({ title, description }),
    }),
  uploadVideo: (base: string, id: string, file: File, signal?: AbortSignal) => {
    const body = new FormData();
    body.append('file', file);
    return request<ApiEnvelope<Video>>(base, `/api/v1/videos/${encodeURIComponent(id)}/upload`, {
      method: 'POST', body, signal,
    });
  },
  publishVideo: (base: string, id: string) =>
    request<{ message: string }>(base, `/api/v1/videos/${encodeURIComponent(id)}/publish`, {
      method: 'POST', body: '{}',
    }),
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
