'use client';

import { useEffect, useState } from 'react';
import { useAuth } from '@/components/auth-provider';
import { RoomCard } from '@/components/room-card';
import { request } from '@/lib/api';
import type { ApiEnvelope, LiveFeedPage, Room } from '@/lib/types';

export function FollowingLive() {
  const { user, apiBase } = useAuth();
  if (!user)
    return (
      <section className="mt-10 text-sm text-white/50">
        登录后可查看关注创作者的直播。
      </section>
    );
  return (
    <FollowingLiveList key={`${user.user_id}:${apiBase}`} base={apiBase} />
  );
}

function FollowingLiveList({ base }: { base: string }) {
  const [rooms, setRooms] = useState<Room[]>([]);
  const [cursor, setCursor] = useState('');
  const [next, setNext] = useState('');
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void request<ApiEnvelope<LiveFeedPage>>(
      base,
      `/api/v1/feeds/live/following?${new URLSearchParams({ cursor })}`,
      { signal: controller.signal, cache: 'no-store' },
    )
      .then(({ data }) => {
        if (controller.signal.aborted) return;
        setRooms((old) =>
          cursor
            ? [
                ...old,
                ...data.items.filter(
                  (item) => !old.some((room) => room.room_id === item.room_id),
                ),
              ]
            : data.items,
        );
        setNext(data.has_more ? data.next_cursor : '');
        setError('');
      })
      .catch((err) => {
        if (!controller.signal.aborted)
          setError(err instanceof Error ? err.message : '读取失败');
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [base, cursor, revision]);
  return (
    <section className="mt-12">
      <div className="mb-5 flex items-center justify-between">
        <h2 className="text-2xl font-bold">关注的直播</h2>
        <button
          disabled={busy}
          className="creator-secondary"
          onClick={() => {
            setCursor('');
            setBusy(true);
            setRevision((value) => value + 1);
          }}
        >
          刷新
        </button>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {rooms.map((room, index) => (
          <RoomCard key={room.room_id} room={room} index={index} />
        ))}
      </div>
      {error && (
        <p role="alert" className="mt-4 text-red-300">
          {error}
        </p>
      )}
      {!busy && !error && !rooms.length && (
        <p className="text-sm text-white/40">关注的创作者暂未开播。</p>
      )}
      {busy ? (
        <p className="mt-4 text-sm text-white/40">正在读取直播…</p>
      ) : (
        next && (
          <button
            className="creator-secondary mt-4"
            onClick={() => {
              setBusy(true);
              setCursor(next);
            }}
          >
            加载更多
          </button>
        )
      )}
    </section>
  );
}
