'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { Bell, CheckCheck, MessageSquare } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { MemberGate } from '@/components/favorites';
import {
  interactions,
  interactionError,
  type Notice,
} from '@/lib/interactions';
import type { Page } from '@/lib/types';
const noticeText: Record<string, string> = {
  follow: '关注了你',
  'comment.reply': '回复了你的评论',
  'comment.review': '更新了评论审核结果',
  'video.review': '更新了视频审核结果',
};
export function NotificationLink() {
  const { user, apiBase } = useAuth();
  return user ? <UnreadLink key={`${apiBase}:${user.user_id}`} /> : null;
}
function UnreadLink() {
  const { apiBase } = useAuth();
  const [count, setCount] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    const load = () => {
      void interactions
        .unread(apiBase, c.signal)
        .then(({ data }) => {
          if (!c.signal.aborted) setCount(data.count);
        })
        .catch(() => {});
    };
    load();
    const timer = setInterval(load, 30000);
    window.addEventListener('notifications-read', load);
    return () => {
      c.abort();
      clearInterval(timer);
      window.removeEventListener('notifications-read', load);
    };
  }, [apiBase]);
  return (
    <Link
      href="/messages"
      aria-label={`消息，${count}条未读`}
      title="消息中心"
      className="relative grid size-9 place-items-center rounded-lg border border-transparent text-white/55 transition hover:border-white/8 hover:bg-white/5 hover:text-white"
    >
      <Bell className="size-[18px]" />
      {count > 0 && (
        <span className="absolute -right-1 -top-1 min-w-4 rounded-full bg-[#ff4d3d] px-1 text-center text-[9px] leading-4 text-white shadow-[0_0_0_2px_#0c0f15]">
          {count > 99 ? '99+' : count}
        </span>
      )}
    </Link>
  );
}
export function Notifications() {
  const { user, apiBase } = useAuth();
  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-3xl px-4 py-8">
        <h1 className="flex items-center gap-3 text-2xl font-semibold">
          <Bell className="size-7 text-[#ff786c]" />
          消息中心
        </h1>
        <p className="mt-3 text-sm text-white/45">
          回复、关注和审核结果，都在这里。
        </p>
        <MemberGate>
          <Inbox key={`${apiBase}:${user?.user_id}`} />
        </MemberGate>
      </main>
    </>
  );
}
function Inbox() {
  const { apiBase } = useAuth();
  const [page, setPage] = useState<Page<Notice> | null>(null),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [retry, setRetry] = useState(0);
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .notifications(apiBase, '', c.signal)
      .then(({ data }) => {
        if (!c.signal.aborted) {
          setPage(data);
          setError('');
        }
      })
      .catch((err) => {
        if (!c.signal.aborted) setError(interactionError(err));
      });
    return () => c.abort();
  }, [apiBase, retry]);
  async function mark(id: string, all = false) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      await interactions.read(apiBase, id, all);
      setPage((p) =>
        p
          ? {
              ...p,
              items: p.items.map((n) =>
                all || n.notification_id === id
                  ? { ...n, read_at: new Date().toISOString() }
                  : n,
              ),
            }
          : p,
      );
      window.dispatchEvent(new Event('notifications-read'));
      setError('');
    } catch (err) {
      setError(interactionError(err));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  async function more() {
    if (!page?.has_more || pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const { data } = await interactions.notifications(
        apiBase,
        page.next_cursor,
      );
      setPage((p) => ({
        ...data,
        items: [
          ...(p?.items || []),
          ...data.items.filter(
            (n) =>
              !p?.items.some((v) => v.notification_id === n.notification_id),
          ),
        ],
      }));
      setError('');
    } catch (err) {
      setError(interactionError(err));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <>
      <div className="my-6 flex flex-wrap gap-3">
        <button
          className="creator-secondary"
          disabled={busy}
          onClick={() => setRetry((v) => v + 1)}
        >
          刷新消息
        </button>
        <button
          className="creator-secondary"
          disabled={busy || !page?.items.length}
          onClick={() =>
            page?.items[0] && void mark(page.items[0].notification_id, true)
          }
        >
          <CheckCheck className="size-4" />
          全部标为已读
        </button>
      </div>
      {error && (
        <p role="alert" className="mb-4 text-sm text-red-300">
          {error}
        </p>
      )}
      {!page && !error && <p>正在加载消息…</p>}
      {page?.items.length === 0 && (
        <div className="creator-panel p-14 text-center text-white/45">
          <MessageSquare className="mx-auto mb-4 size-8" />
          暂无消息
        </div>
      )}
      <div className="space-y-3">
        {page?.items.map((n) => (
          <article
            key={n.notification_id}
            className="creator-panel flex gap-4 p-5"
          >
            <div
              className={`mt-2 size-2 shrink-0 rounded-full ${n.read_at ? 'bg-white/10' : 'bg-[#ff786c]'}`}
            />
            <div className="min-w-0 flex-1">
              <p className="text-sm">
                <span className="font-medium">{n.actor_name}</span>
                <span className="ml-2 text-white/65">
                  {noticeText[n.kind] || '发送了一条通知'}
                </span>
              </p>
              <p className="mt-2 text-xs text-white/35">
                {new Date(n.created_at).toLocaleString('zh-CN')}
              </p>
              <div className="mt-3 flex flex-wrap gap-4 text-xs">
                {n.url ? (
                  <Link
                    href={n.url}
                    className="text-[#ff8a80]"
                    onClick={() => {
                      if (!n.read_at) void mark(n.notification_id);
                    }}
                  >
                    查看详情 →
                  </Link>
                ) : (
                  <span className="text-white/35">相关内容已不可用</span>
                )}
                {!n.read_at && (
                  <button
                    disabled={busy}
                    onClick={() => void mark(n.notification_id)}
                    className="text-white/50"
                  >
                    标为已读
                  </button>
                )}
              </div>
            </div>
          </article>
        ))}
      </div>
      {page?.has_more && (
        <button
          disabled={busy}
          className="creator-secondary mx-auto mt-6"
          onClick={() => void more()}
        >
          {busy ? '加载中…' : '加载更多'}
        </button>
      )}
    </>
  );
}
