'use client';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';
import { ReportButton } from '@/components/report-button';
import { useWatchHistory } from '@/hooks/use-watch-history';
import { useVideoPlayBehavior } from '@/hooks/use-video-play-behavior';
import { watchTime } from '@/lib/watch-history';
import { request } from '@/lib/api';
import type { ApiEnvelope, Page, Video } from '@/lib/types';
import {
  danmakuRequestID,
  scheduleDanmakus,
  type VideoDanmaku,
} from '@/lib/video-danmaku';

export function VideoPlayer({ video }: { video: Video }) {
  const { apiBase, user } = useAuth();
  const media = useRef<HTMLVideoElement>(null),
    frame = useRef<HTMLDivElement>(null);
  const history = useWatchHistory(media, video.video_id, video.duration_ms);
  useVideoPlayBehavior(media, video.video_id, video.duration_ms);
  const [time, setTime] = useState(0),
    [enabled, setEnabled] = useState(true),
    [lanes, setLanes] = useState(4),
    [size, setSize] = useState(24),
    [opacity, setOpacity] = useState(0.9);
  const [words, setWords] = useState(''),
    [users, setUsers] = useState<string[]>([]),
    [text, setText] = useState(''),
    [color, setColor] = useState('#ffffff');
  const [login, setLogin] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [loadError, setLoadError] = useState(''),
    [mediaError, setMediaError] = useState(''),
    [refresh, setRefresh] = useState(0),
    [truncated, setTruncated] = useState(false);
  const [data, setData] = useState<{ window: number; items: VideoDanmaku[] }>({
    window: -1,
    items: [],
  });
  const pending = useRef(false),
    retry = useRef<{
      content: string;
      color: string;
      position_ms: number;
      request_id: string;
    } | null>(null);
  const window = Math.floor(time / 30000);
  const path = `/api/v1/videos/${video.video_id}/danmakus`;
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const node = frame.current;
    if (!node) return;
    const observer = new ResizeObserver(() => setHeight(node.clientHeight));
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const timer = setInterval(() => {
      if (media.current) setTime(Math.floor(media.current.currentTime * 1000));
    }, 50);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    const timer = setInterval(() => setRefresh((v) => v + 1), 10000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    const c = new AbortController();
    if (!enabled) return () => c.abort();
    async function load() {
      const starts = [window - 1, window, window + 1].filter(
        (n) => n >= 0 && n * 30000 < video.duration_ms,
      );
      const pages = await Promise.all(
        starts.map(async (n) => {
          let cursor = '';
          const items: VideoDanmaku[] = [];
          let more = false;
          for (let i = 0; i < 10; i++) {
            const q = new URLSearchParams({
              from_ms: String(n * 30000),
              to_ms: String((n + 1) * 30000),
              limit: '100',
              cursor,
            });
            const { data } = await request<ApiEnvelope<Page<VideoDanmaku>>>(
              apiBase,
              `${path}?${q}`,
              { signal: c.signal, cache: 'no-store' },
            );
            items.push(...data.items);
            more = data.has_more;
            if (!more) break;
            if (!data.next_cursor || cursor === data.next_cursor)
              throw new Error('弹幕分页异常，请重试');
            cursor = data.next_cursor;
          }
          return { items, more };
        }),
      );
      if (!c.signal.aborted) {
        setData({ window, items: pages.flatMap((p) => p.items) });
        setTruncated(pages.some((p) => p.more));
        setLoadError('');
      }
    }
    void load().catch((err) => {
      if (!c.signal.aborted) {
        setData({ window, items: [] });
        setLoadError(err instanceof Error ? err.message : '弹幕读取失败');
      }
    });
    return () => c.abort();
  }, [apiBase, path, window, refresh, enabled, video.duration_ms]);
  const items = enabled && data.window === window ? data.items : [];
  const fitLanes = Math.max(
    1,
    Math.min(lanes, Math.floor((height - 70) / (size + 8))),
  );
  const scheduled = useMemo(
    () =>
      enabled && data.window === window
        ? scheduleDanmakus(
            data.items,
            fitLanes,
            words
              .split(/[,，\n]/)
              .map((s) => s.trim())
              .filter(Boolean),
            users,
          )
        : [],
    [enabled, data, window, fitLanes, words, users],
  );
  const visible = scheduled
    .filter(
      ({ item }) => time >= item.position_ms && time < item.position_ms + 8000,
    )
    .map((row) => ({ ...row, progress: (time - row.item.position_ms) / 8000 }));
  async function send() {
    if (!user) {
      setLogin(true);
      return;
    }
    if (pending.current || !text.trim() || !media.current) return;
    if (
      !retry.current ||
      retry.current.content !== text.trim() ||
      retry.current.color !== color
    )
      retry.current = {
        content: text.trim(),
        color,
        position_ms: Math.floor(media.current.currentTime * 1000),
        request_id: danmakuRequestID(),
      };
    pending.current = true;
    setBusy(true);
    setError('');
    try {
      const { data: item } = await request<ApiEnvelope<VideoDanmaku>>(
        apiBase,
        path,
        { method: 'POST', body: JSON.stringify(retry.current) },
      );
      setData((d) => ({
        ...d,
        items: [
          ...d.items.filter((v) => v.danmaku_id !== item.danmaku_id),
          item,
        ],
      }));
      setText('');
      retry.current = null;
      setRefresh((v) => v + 1);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : '发送失败，点击发送可重试原消息',
      );
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  async function remove(id: string) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      await request(apiBase, `${path}/${id}`, { method: 'DELETE' });
      setData((d) => ({
        ...d,
        items: d.items.filter((i) => i.danmaku_id !== id),
      }));
      setRefresh((v) => v + 1);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败');
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <section className="mt-6" aria-label="视频与弹幕">
      {history.resume > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-3 rounded-xl border border-white/10 bg-white/5 px-4 py-3 text-sm">
          <span className="text-white/65">上次看到 {watchTime(history.resume)}</span>
          <button className="text-[#ff8a80] hover:text-white" onClick={history.continueWatching}>继续播放</button>
          <button className="ml-auto text-white/40 hover:text-white" onClick={history.dismiss}>忽略</button>
        </div>
      )}
      <div ref={frame} className="relative overflow-hidden rounded-xl bg-black">
        <video
          ref={media}
          src={video.play_url}
          controls
          controlsList="nofullscreen"
          playsInline
          preload="metadata"
          className="aspect-video h-full w-full object-contain"
          onSeeking={() =>
            setTime(Math.floor((media.current?.currentTime || 0) * 1000))
          }
          onLoadedMetadata={() => setMediaError('')}
          onError={() => setMediaError('视频加载失败，请检查媒体地址和编码')}
        >
          <track
            kind="captions"
            src="data:text/vtt,WEBVTT"
            srcLang="zh"
            label="暂无字幕"
          />
        </video>
        <div
          className="pointer-events-none absolute inset-x-0 top-0 bottom-14 overflow-hidden"
          aria-hidden="true"
        >
          {visible.map(({ item, lane, progress }) => (
            <span
              key={item.danmaku_id}
              className="absolute max-w-[65%] truncate whitespace-nowrap font-semibold"
              style={{
                top: `${8 + lane * (size + 8)}px`,
                left: `${(1 - progress) * 100}%`,
                transform: `translateX(-${progress * 100}%)`,
                fontSize: size,
                color: item.color,
                opacity,
                textShadow: '1px 1px 2px #000,-1px -1px 2px #000',
              }}
            >
              {item.content}
            </span>
          ))}
        </div>
      </div>
      {history.status && (
        <output className="mt-3 flex flex-wrap items-center gap-3 text-xs text-amber-200/80">
          <span>{history.status}</span>
          <button className="underline" onClick={history.retry}>重新启用保存</button>
        </output>
      )}
      {mediaError && (
        <p role="alert" className="mt-3 text-red-300">
          {mediaError}
        </p>
      )}
      <div className="creator-panel mt-3 p-4">
        <div className="flex flex-wrap items-center gap-4 text-xs text-white/65">
          <label>
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />{' '}
            弹幕
          </label>
          <button
            type="button"
            onClick={async () => {
              try {
                if (document.fullscreenElement) await document.exitFullscreen();
                else await frame.current?.requestFullscreen();
              } catch {
                setError('当前浏览器不支持弹幕全屏');
              }
            }}
          >
            弹幕全屏
          </button>
          <label>
            密度{' '}
            <select
              aria-label="弹幕密度"
              className="bg-[#12151c]"
              value={lanes}
              onChange={(e) => setLanes(Number(e.target.value))}
            >
              <option value={2}>少量</option>
              <option value={4}>适中</option>
              <option value={6}>较多</option>
            </select>
          </label>
          <label>
            字号{' '}
            <select
              aria-label="弹幕字号"
              className="bg-[#12151c]"
              value={size}
              onChange={(e) => setSize(Number(e.target.value))}
            >
              <option value={18}>小</option>
              <option value={24}>中</option>
              <option value={30}>大</option>
            </select>
          </label>
          <label className="flex items-center gap-2">
            透明度{' '}
            <input
              aria-label="弹幕透明度"
              type="range"
              min="0.3"
              max="1"
              step="0.1"
              value={opacity}
              onChange={(e) => setOpacity(Number(e.target.value))}
            />
          </label>
        </div>
        <form
          className="mt-4 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <input
            aria-label="弹幕正文"
            className="creator-input min-w-0"
            maxLength={100}
            value={text}
            disabled={busy}
            placeholder="在这一刻说点什么…"
            onChange={(e) => setText(e.target.value)}
          />
          <select
            aria-label="弹幕颜色"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="rounded-lg bg-[#12151c]"
          >
            <option value="#ffffff">白色</option>
            <option value="#ffe066">黄色</option>
            <option value="#ff8a80">珊瑚色</option>
          </select>
          <button
            className="creator-primary shrink-0"
            disabled={busy || !text.trim()}
          >
            {busy ? '发送中…' : '发送'}
          </button>
        </form>
        {error && (
          <p role="alert" className="mt-2 text-xs text-red-300">
            {error}
          </p>
        )}
        {loadError && (
          <p role="alert" className="mt-2 text-xs text-red-300">
            {loadError}
            <button
              className="ml-3 underline"
              onClick={() => setRefresh((v) => v + 1)}
            >
              重试
            </button>
          </p>
        )}
        {truncated && (
          <p className="mt-2 text-xs text-white/40">
            这一片段弹幕较多，已限制加载数量。
          </p>
        )}
        <details className="mt-4 text-xs text-white/60">
          <summary className="cursor-pointer">屏蔽设置与当前片段弹幕</summary>
          <label className="mt-3 block">
            屏蔽关键词（逗号分隔）
            <input
              className="creator-input mt-2"
              value={words}
              maxLength={500}
              onChange={(e) => setWords(e.target.value)}
            />
          </label>
          {users.length > 0 && (
            <button className="mt-3 underline" onClick={() => setUsers([])}>
              取消本次屏蔽的 {users.length} 位用户
            </button>
          )}
          <p className="my-3 text-white/35">
            设置仅在本次观看生效。隐藏或删除的弹幕将在刷新片段时移除，最长约10秒。
          </p>
          <div className="max-h-64 space-y-3 overflow-y-auto">
            {items
              .filter(
                (i) =>
                  i.position_ms >= window * 30000 &&
                  i.position_ms < (window + 1) * 30000,
              )
              .map((i) => (
                <div
                  key={i.danmaku_id}
                  className="flex flex-wrap items-center gap-3 border-t border-white/5 py-2"
                >
                  <span className="text-white/35">
                    {(i.position_ms / 1000).toFixed(1)}s
                  </span>
                  <span className="min-w-0 flex-1 break-all">{i.content}</span>
                  <button
                    onClick={() =>
                      setUsers((v) =>
                        v.includes(i.user_id) ? v : [...v, i.user_id],
                      )
                    }
                  >
                    屏蔽用户
                  </button>
                  <ReportButton kind="video_danmaku" id={i.danmaku_id} />
                  {String(user?.user_id) === i.user_id && (
                    <button
                      disabled={busy}
                      onClick={() => void remove(i.danmaku_id)}
                    >
                      删除
                    </button>
                  )}
                </div>
              ))}
          </div>
        </details>
      </div>
      <AuthDialog open={login} onOpenChange={setLogin} />
    </section>
  );
}
