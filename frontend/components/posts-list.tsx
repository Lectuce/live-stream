'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { useAuth } from '@/components/auth-provider';
import { PostCard } from '@/components/post-card';
import type { Post, PostPage } from '@/lib/types';

export function PostsList({ author = '' }: { author?: string }) {
  const { apiBase, user } = useAuth();
  return <ListContent key={`${apiBase}:${author}:${user?.user_id ?? 'guest'}`} base={apiBase} author={author} />;
}

function ListContent({ base, author }: { base: string; author: string }) {
  const [items, setItems] = useState<Post[]>([]);
  const [page, setPage] = useState<PostPage | null>(null);
  const [cursor, setCursor] = useState('');
  const [retry, setRetry] = useState(0);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void api.posts(base, author, cursor, controller.signal).then(({ data }) => {
      if (controller.signal.aborted) return; setPage(data); setError('');
      setItems((old) => { const seen = new Set(old.map((p) => p.post_id)); return cursor ? [...old, ...data.items.filter((p) => !seen.has(p.post_id))] : data.items; });
    }).catch((err: unknown) => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '加载失败'); })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
  }, [base, author, cursor, retry]);
  return <div className="space-y-4">
    {items.map((post) => <PostCard key={post.post_id} post={post} mine={!author} />)}
    {!busy && !error && !items.length && <p className="creator-panel p-10 text-center text-sm text-white/40">{author ? '还没有公开图文' : '还没有图文，发布你的第一条分享吧。'}</p>}
    {error && <p role="alert" className="text-red-300">{error}</p>}
    {busy ? <p className="text-sm text-white/40">正在加载…</p> : error ? <button className="creator-secondary" onClick={() => { setBusy(true); setRetry((v) => v + 1); }}>重试</button> : page?.has_more && <button className="creator-secondary" onClick={() => { setBusy(true); setCursor(page.next_cursor); }}>加载更多图文</button>}
  </div>;
}
