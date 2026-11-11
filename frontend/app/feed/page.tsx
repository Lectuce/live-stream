'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { ArrowDown, ArrowUp, Radio, RefreshCw } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { LiveFeedItem } from '@/components/live-feed-item';
import { Button } from '@/components/ui/button';
import { useAuth } from '@/components/auth-provider';
import { ApiError, api } from '@/lib/api';
import type { Room } from '@/lib/types';

export default function LiveFeedPage() {
  const { apiBase } = useAuth();

  const [rooms, setRooms] = useState<Room[]>([]);
  const [activeIndex, setActiveIndex] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(true);
  const [nextCursor, setNextCursor] = useState('');
  const [error, setError] = useState('');

  const scrollerRef = useRef<HTMLDivElement>(null);
  const activeRoomIdRef = useRef<number | null>(null);
  const activeIndexRef = useRef(0);
  const scrollFrameRef = useRef<number | null>(null);

  const nextCursorRef = useRef('');
  const hasMoreRef = useRef(true);
  const loadingMoreRef = useRef(false);
  const requestGenerationRef = useRef(0);
  const refreshAbortRef = useRef<AbortController | null>(null);
  const loadMoreAbortRef = useRef<AbortController | null>(null);

  const cancelFeedRequests = useCallback(() => {
    requestGenerationRef.current++;
    refreshAbortRef.current?.abort();
    loadMoreAbortRef.current?.abort();

    refreshAbortRef.current = null;
    loadMoreAbortRef.current = null;
    loadingMoreRef.current = false;
  }, []);



  const refreshFeed = useCallback(async (showLoading = false) => {
    const generation = ++requestGenerationRef.current;

    refreshAbortRef.current?.abort();
    loadMoreAbortRef.current?.abort();
    loadMoreAbortRef.current = null;
    loadingMoreRef.current = false;
    setLoadingMore(false);

    const controller = new AbortController();
    refreshAbortRef.current = controller;

    if (showLoading) setLoading(true);

    try {
      const result = await api.liveFeed(
        apiBase,
        '',
        20,
        controller.signal,
      );

      if (
        controller.signal.aborted ||
        generation !== requestGenerationRef.current
      ) {
        return;
      }

      const items = result.data.items ?? [];
      const cursor = result.data.next_cursor ?? '';
      const more = Boolean(result.data.has_more);

      setRooms(items);
      setNextCursor(cursor);
      setHasMore(more);

      nextCursorRef.current = cursor;
      hasMoreRef.current = more;

      activeIndexRef.current = 0;
      activeRoomIdRef.current = items[0]?.room_id ?? null;
      setActiveIndex(0);
      setError('');
    } catch (err) {
      if (controller.signal.aborted) return;
      if (generation !== requestGenerationRef.current) return;

      setError(
        err instanceof Error ? err.message : '读取直播 Feed 失败',
      );
    } finally {
      if (generation === requestGenerationRef.current) {
        if (showLoading) setLoading(false);
        if (refreshAbortRef.current === controller) {
          refreshAbortRef.current = null;
        }
      }
    }
  }, [apiBase]);

  const loadMore = useCallback(async () => {
    const cursor = nextCursorRef.current;

    if (
      loadingMoreRef.current ||
      !hasMoreRef.current ||
      !cursor
    ) {
      return;
    }

    loadingMoreRef.current = true;
    setLoadingMore(true);

    const generation = requestGenerationRef.current;
    const controller = new AbortController();
    loadMoreAbortRef.current = controller;

    try {
      const result = await api.liveFeed(
        apiBase,
        cursor,
        20,
        controller.signal,
      );

      if (
        controller.signal.aborted ||
        generation !== requestGenerationRef.current
      ) {
        return;
      }

      const incoming = result.data.items ?? [];
      const newCursor = result.data.next_cursor ?? '';
      const more = Boolean(result.data.has_more);

      setRooms((current) => {
        const existingIDs = new Set(
          current.map((room) => room.room_id),
        );

        const additions = incoming.filter(
          (room) => !existingIDs.has(room.room_id),
        );

        return [...current, ...additions];
      });

      nextCursorRef.current = newCursor;
      hasMoreRef.current = more;
      setNextCursor(newCursor);
      setHasMore(more);
      setError('');
    } catch (err) {
      if (controller.signal.aborted) return;
      if (generation !== requestGenerationRef.current) return;

      if (err instanceof ApiError && err.status === 410) {
        setError('直播列表已经更新，正在重新加载');
        void refreshFeed(false);
        return;
      }

      setError(
        err instanceof Error ? err.message : '加载更多直播失败',
      );
    } finally {
      if (loadMoreAbortRef.current === controller) {
        loadMoreAbortRef.current = null;
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  }, [apiBase, refreshFeed]);

  const removeEndedRoom = useCallback((roomID: number) => {
    setRooms((current) =>
      current.filter((room) => room.room_id !== roomID),
    );
  }, []);

  useEffect(() => {
    let disposed = false;

    queueMicrotask(() => {
      if (!disposed) void refreshFeed(true);
    });

    return () => {
      disposed = true;
      requestGenerationRef.current++;
      refreshAbortRef.current?.abort();
      loadMoreAbortRef.current?.abort();
    };
  }, [refreshFeed]);

  useEffect(() => {
    if (rooms.length === 0) {
      const previousIndex = activeIndexRef.current;

      activeRoomIdRef.current = null;
      activeIndexRef.current = 0;

      if (previousIndex !== 0) {
        queueMicrotask(() => {
          setActiveIndex(0);
        });
      }

      if (
        hasMoreRef.current &&
        nextCursorRef.current &&
        !loadingMoreRef.current
      ) {
        void loadMore();
      }

      return;
    }

    const previousIndex = activeIndexRef.current;

    const previousRoomIndex = rooms.findIndex(
      (room) => room.room_id === activeRoomIdRef.current,
    );

    const nextIndex =
      previousRoomIndex >= 0
        ? previousRoomIndex
        : Math.min(previousIndex, rooms.length - 1);

    activeIndexRef.current = nextIndex;
    activeRoomIdRef.current = rooms[nextIndex].room_id;

    if (previousIndex !== nextIndex) {
      queueMicrotask(() => {
        setActiveIndex(nextIndex);
      });
    }

    const frame = requestAnimationFrame(() => {
      const scroller = scrollerRef.current;

      if (scroller) {
        scroller.scrollTop = nextIndex * scroller.clientHeight;
      }
    });

    return () => {
      cancelAnimationFrame(frame);
    };
  }, [loadMore, rooms]);

  useEffect(() => {
    if (
      rooms.length > 0 &&
      activeIndex >= rooms.length - 3
    ) {
      void loadMore();
    }
  }, [activeIndex, loadMore, rooms.length, nextCursor]);

  useEffect(() => () => {
    if (scrollFrameRef.current !== null) {
      cancelAnimationFrame(scrollFrameRef.current);
    }
  }, []);

  const onScroll = useCallback(() => {
    if (scrollFrameRef.current !== null) return;

    scrollFrameRef.current = requestAnimationFrame(() => {
      scrollFrameRef.current = null;

      const scroller = scrollerRef.current;
      if (!scroller || !scroller.clientHeight || rooms.length === 0) {
        return;
      }

      const index = Math.max(
        0,
        Math.min(
          rooms.length - 1,
          Math.round(scroller.scrollTop / scroller.clientHeight),
        ),
      );

      if (index === activeIndexRef.current) return;

      activeIndexRef.current = index;
      activeRoomIdRef.current = rooms[index].room_id;
      setActiveIndex(index);
    });
  }, [rooms]);

  const scrollToIndex = useCallback((index: number) => {
    const scroller = scrollerRef.current;
    if (!scroller || rooms.length === 0) return;

    const nextIndex = Math.max(
      0,
      Math.min(rooms.length - 1, index),
    );

    scroller.scrollTo({
      top: nextIndex * scroller.clientHeight,
      behavior: 'smooth',
    });
  }, [rooms.length]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;

      if (
        target?.closest(
          'input, textarea, button, a, [contenteditable="true"]',
        )
      ) {
        return;
      }

      if (event.key === 'ArrowDown') {
        event.preventDefault();
        scrollToIndex(activeIndexRef.current + 1);
      } else if (event.key === 'ArrowUp') {
        event.preventDefault();
        scrollToIndex(activeIndexRef.current - 1);
      }
    };

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [scrollToIndex]);

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-[#080a0f]">
      <AppHeader />

      <main className="relative min-h-0 flex-1">
        {rooms.length > 0 ? (
          <>
            <div
              ref={scrollerRef}
              onScroll={onScroll}
              className="h-full snap-y snap-mandatory overflow-y-auto overscroll-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              {rooms.map((room, index) => (
                <LiveFeedItem
                  key={room.room_id}
                  room={room}
                  index={index}
                  total={rooms.length}
                  active={index === activeIndex}
                  apiBase={apiBase}
                  onRoomEnded={removeEndedRoom}
                />
              ))}
            </div>

            <div className="absolute bottom-5 right-5 z-10 hidden flex-col gap-2 lg:flex">
              <Button
                variant="outline"
                size="icon"
                disabled={activeIndex === 0}
                onClick={() => scrollToIndex(activeIndex - 1)}
                aria-label="上一个房间"
                className="rounded-full border-white/15 bg-[#11151d]/85"
              >
                <ArrowUp />
              </Button>

              <Button
                variant="outline"
                size="icon"
                disabled={
                  activeIndex >= rooms.length - 1 && !hasMore
                }
                onClick={() => {
                  if (activeIndex >= rooms.length - 1) {
                    void loadMore();
                    return;
                  }
                  scrollToIndex(activeIndex + 1);
                }}
                aria-label="下一个房间"
                className="rounded-full border-white/15 bg-[#11151d]/85"
              >
                <ArrowDown />
              </Button>
            </div>

            {loadingMore && (
              <div className="absolute bottom-5 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2 rounded-full border border-white/10 bg-[#11151d]/90 px-4 py-2 text-xs text-white/60">
                <RefreshCw className="size-3.5 animate-spin" />
                加载更多直播
              </div>
            )}

            {error && (
              <div className="absolute left-1/2 top-4 z-20 max-w-[90vw] -translate-x-1/2 rounded-lg border border-amber-500/25 bg-[#241a16] px-4 py-2 text-xs text-amber-200">
                {error}
              </div>
            )}
          </>
        ) : (
          <div className="grid h-full place-items-center px-6 text-center">
            <div className="max-w-sm">
              <div className="mx-auto mb-5 grid size-20 place-items-center rounded-2xl border border-white/10 bg-white/[.035] text-[#ff6255]">
                <Radio className="size-9" />
              </div>

              <h1 className="text-2xl font-bold">
                {loading
                  ? '正在寻找互动房间…'
                  : error
                    ? '直播 Feed 加载失败'
                    : '暂时没有正在互动的房间'}
              </h1>

              <p className="mt-3 text-sm leading-6 text-white/45">
                {error ||
                  (loading
                    ? '请稍候'
                    : '有人开启房间后，就可以在这里上下滑动浏览。')}
              </p>

              {!loading && (
                <div className="mt-6 flex justify-center gap-3">
                  <Button
                    onClick={() => void refreshFeed(true)}
                    className="bg-[#ff4d3d] hover:bg-[#ff6356]"
                  >
                    <RefreshCw />
                    刷新
                  </Button>

                  <Link
                    href="/"
                    className="inline-flex h-9 items-center rounded-md border border-white/15 px-4 text-sm text-white/75 hover:bg-white/5"
                  >
                    返回首页
                  </Link>
                </div>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
