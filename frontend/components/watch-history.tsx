'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { History, Play, Trash2, Film, LockKeyhole } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { MemberGate } from '@/components/favorites';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  watchHistory,
  watchTime,
  type WatchHistoryItem,
} from '@/lib/watch-history';
import { resumePosition } from '@/lib/watch-progress-buffer';

export function WatchHistory() {
  const { apiBase, user } = useAuth();
  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-6xl px-4 py-8">
        <div className="flex items-center gap-3">
          <History className="size-7 text-[#ff786c]" />
          <h1 className="text-2xl font-semibold">观看历史</h1>
        </div>
        <p className="mt-3 flex items-center gap-2 text-sm text-white/45">
          <LockKeyhole className="size-3.5" />
          仅自己可见，接着上次的进度继续看。
        </p>
        <MemberGate message="登录后查看观看历史，在不同设备上继续播放">
          <HistoryList key={`${apiBase}:${user?.user_id}`} />
        </MemberGate>
      </main>
    </>
  );
}

function HistoryList() {
  const { apiBase } = useAuth();
  const [items, setItems] = useState<WatchHistoryItem[]>([]);
  const [cursor, setCursor] = useState('');
  const [more, setMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const [remove, setRemove] = useState<WatchHistoryItem | 'all' | null>(null);
  const [busy, setBusy] = useState(false);
  const [removeError, setRemoveError] = useState('');
  const pending = useRef(false);
  const generation = useRef(0);
  const requests = useRef<{ controller: AbortController | null }>({ controller: null });

  function refresh() {
    setLoading(true); setError(''); setItems([]); setCursor(''); setMore(false);
    setRevision((v) => v + 1);
  }

  useEffect(() => {
    const controller = new AbortController();
    const active = requests.current;
    active.controller = controller;
    const gen = ++generation.current;
    pending.current = true;
    void watchHistory
      .list(apiBase, '', controller.signal)
      .then(({ data }) => {
        if (controller.signal.aborted || gen !== generation.current) return;
        setItems(data.items);
        setCursor(data.next_cursor);
        setMore(data.has_more);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted && gen === generation.current)
          setError(err instanceof Error ? err.message : '观看历史读取失败');
      })
      .finally(() => {
        if (!controller.signal.aborted && gen === generation.current) {
          pending.current = false;
          setLoading(false);
        }
      });
    return () => {
      controller.abort();
      active.controller?.abort();
    };
  }, [apiBase, revision, requests]);

  async function loadMore() {
    if (pending.current || !more || !cursor) return;
    pending.current = true;
    setLoading(true);
    setError('');
    const controller = new AbortController();
    requests.current.controller = controller;
    const gen = generation.current;
    try {
      const { data } = await watchHistory.list(
        apiBase,
        cursor,
        controller.signal,
      );
      if (controller.signal.aborted || gen !== generation.current) return;
      if (data.has_more && (!data.next_cursor || data.next_cursor === cursor))
        throw new Error('历史分页异常，请刷新');
      setItems((old) => {
        const seen = new Set(old.map((item) => item.video_id));
        return [
          ...old,
          ...data.items.filter((item) => !seen.has(item.video_id)),
        ];
      });
      setCursor(data.next_cursor);
      setMore(data.has_more);
    } catch (err) {
      if (!controller.signal.aborted && gen === generation.current)
        setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      if (!controller.signal.aborted && gen === generation.current) {
        pending.current = false;
        setLoading(false);
      }
    }
  }
  async function confirmRemove() {
    if (!remove || busy) return;
    setBusy(true);
    setRemoveError('');
    // Prevent a response started before deletion from restoring a removed card.
    generation.current++;
    requests.current.controller?.abort();
    pending.current = false;
    setLoading(false);
    try {
      await watchHistory.remove(
        apiBase,
        remove === 'all' ? undefined : remove.video_id,
      );
      setRemove(null);
      refresh();
    } catch (err) {
      setRemoveError(err instanceof Error ? err.message : '删除失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="my-6 flex justify-end gap-3">
        <button
          className="creator-secondary"
          onClick={refresh}
          disabled={loading || busy}
        >
          刷新
        </button>
        <button
          className="creator-secondary"
          onClick={() => {
            setRemove('all');
            setRemoveError('');
          }}
          disabled={!items.length || busy}
        >
          <Trash2 className="size-4" />
          清空历史
        </button>
      </div>
      {error && (
        <p role="alert" className="mb-4 text-sm text-red-300">
          {error}{' '}
          <button
            className="underline"
            onClick={refresh}
          >
            重新加载
          </button>
        </p>
      )}
      {!loading && !error && !items.length && (
        <div className="creator-panel p-14 text-center">
          <History className="mx-auto mb-4 size-10 text-white/20" />
          <p className="text-white/60">还没有观看记录</p>
          <Link
            href="/video"
            className="mt-4 inline-block text-sm text-[#ff8a80]"
          >
            去看看视频
          </Link>
        </div>
      )}
      <div className="space-y-3">
        {items.map((item) => (
          <article
            key={item.video_id}
            className="creator-panel flex gap-4 p-3 sm:p-4"
          >
            <div className="relative aspect-video w-28 shrink-0 self-start overflow-hidden rounded-lg bg-white/5 sm:w-48">
              {item.available && item.cover_url ? (
                <Image
                  src={item.cover_url}
                  alt=""
                  fill
                  unoptimized
                  className="object-cover"
                  sizes="192px"
                />
              ) : (
                <div className="grid size-full place-items-center">
                  <Film className="size-8 text-white/20" />
                </div>
              )}
              <div className="absolute inset-x-0 bottom-0 h-1 bg-white/10">
                <div
                  className="h-full bg-[#ff786c]"
                  style={{
                    width: `${item.duration_ms ? Math.min(100, (item.position_ms / item.duration_ms) * 100) : 0}%`,
                  }}
                />
              </div>
            </div>
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-sm font-semibold sm:text-base">
                {item.available ? (
                  <Link href={`/video/${item.video_id}`}>{item.title}</Link>
                ) : (
                  '视频已不可用'
                )}
              </h2>
              {item.available && (
                <Link
                  href={`/space/${item.owner_id}`}
                  className="mt-1 block truncate text-xs text-white/40"
                >
                  {item.owner_name}
                </Link>
              )}
              <p className="mt-2 text-xs text-white/40">
                {new Date(item.last_watched_at).toLocaleString('zh-CN')}
              </p>
              {item.available && (
                <p className="mt-2 text-xs text-white/60">
                  {resumePosition(item.position_ms, item.duration_ms) === 0 &&
                  item.position_ms > 0
                    ? '已看完'
                    : `看到 ${watchTime(item.position_ms)}`}{' '}
                  / 共 {watchTime(item.duration_ms)}
                </p>
              )}
              {item.available && (
                <Link
                  href={`/video/${item.video_id}`}
                  className="mt-3 inline-flex items-center gap-1 text-xs text-[#ff8a80]"
                >
                  <Play className="size-3" />
                  {resumePosition(item.position_ms, item.duration_ms) > 0
                    ? '继续观看'
                    : '播放视频'}
                </Link>
              )}
            </div>
            <button
              className="self-start rounded-lg p-2 text-white/35 hover:bg-white/5 hover:text-red-300"
              aria-label={`删除${item.available ? item.title : '不可用视频'}的观看记录`}
              disabled={busy}
              onClick={() => {
                setRemove(item);
                setRemoveError('');
              }}
            >
              <Trash2 className="size-4" />
            </button>
          </article>
        ))}
      </div>
      {loading && (
        <output className="block py-8 text-center text-sm text-white/45">
          正在加载观看历史…
        </output>
      )}
      {more && (
        <button
          className="creator-secondary mx-auto mt-6"
          onClick={() => void loadMore()}
          disabled={loading || busy}
        >
          加载更多
        </button>
      )}
      {!more && items.length > 0 && !loading && (
        <p className="py-6 text-center text-xs text-white/30">
          已显示全部观看记录
        </p>
      )}
      <Dialog
        open={remove !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setRemove(null);
        }}
      >
        <DialogContent>
          <DialogTitle>
            {remove === 'all' ? '清空观看历史？' : '删除这条观看记录？'}
          </DialogTitle>
          <DialogDescription>
            删除后将无法恢复保存的进度。再次开始播放时，会产生新的观看记录。
          </DialogDescription>
          {removeError && (
            <p role="alert" className="text-sm text-red-300">
              {removeError}
            </p>
          )}
          <div className="flex justify-end gap-3">
            <button
              className="creator-secondary"
              disabled={busy}
              onClick={() => setRemove(null)}
            >
              取消
            </button>
            <button
              className="creator-primary"
              disabled={busy}
              onClick={() => void confirmRemove()}
            >
              {busy ? '正在删除…' : '确认删除'}
            </button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
