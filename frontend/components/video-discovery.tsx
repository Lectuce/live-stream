'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { Film, Radio, Search, Play, Sparkles } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { ApiError } from '@/lib/api';
import { watchTime } from '@/lib/watch-history';
import {
  videoCategories,
  videoDiscovery,
  type VideoCardData,
  type VideoFeedMode,
} from '@/lib/video-discovery';

export function DiscoveryNavigation({
  mode,
  category = '',
}: {
  mode: VideoFeedMode;
  category?: string;
}) {
  const tab = (sort: VideoFeedMode) =>
    category
      ? `/category/${category}?sort=${sort}`
      : sort === 'recommended'
        ? '/'
        : sort === 'hot'
          ? '/popular'
          : '/video';
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-4">
        <nav className="flex gap-2">
          {(
            [
              ['recommended', '推荐'],
              ['hot', '热门'],
              ['latest', '最新'],
            ] as const
          ).map(([sort, name]) => (
            <Link
              key={sort}
              href={tab(sort)}
              className={`rounded-full px-5 py-2 text-sm ${mode === sort ? 'bg-[#ff6255] text-white' : 'bg-white/5 text-white/55 hover:text-white'}`}
            >
              {name}
            </Link>
          ))}
          <Link
            href="/live"
            className="flex items-center gap-2 px-3 text-sm text-white/55"
          >
            <Radio className="size-4" />
            直播
          </Link>
        </nav>
        <form action="/search" className="flex w-full gap-2 sm:w-72">
          <input
            name="q"
            aria-label="搜索视频、用户或直播"
            required
            maxLength={50}
            placeholder="搜索视频、用户或直播"
            className="creator-input min-w-0"
          />
          <button aria-label="搜索" className="creator-secondary">
            <Search className="size-4" />
          </button>
        </form>
      </div>
      <nav aria-label="视频分区" className="mt-5 flex flex-wrap gap-2">
        <Link
          href="/"
          className={`rounded-lg px-3 py-1.5 text-xs ${!category ? 'bg-white/10 text-white' : 'text-white/45'}`}
        >
          全部
        </Link>
        {videoCategories.map(([id, name]) => (
          <Link
            key={id}
            href={`/category/${id}`}
            className={`rounded-lg px-3 py-1.5 text-xs hover:bg-white/5 ${category === id ? 'bg-white/10 text-[#ff8a80]' : 'text-white/45'}`}
          >
            {name}
          </Link>
        ))}
      </nav>
    </>
  );
}

export function VideoDiscovery({
  mode = 'recommended',
  category = '',
}: {
  mode?: VideoFeedMode;
  category?: string;
}) {
  const { apiBase, user } = useAuth();
  useEffect(() => {
    if (
      window.location.pathname === '/' &&
      ['#studio', '#live-rooms'].includes(window.location.hash)
    )
      window.location.replace('/live' + window.location.hash);
  }, []);
  const title = category
    ? videoCategories.find(([id]) => id === category)?.[1] || '未知分区'
    : mode === 'hot'
      ? '大家正在看'
      : mode === 'latest'
        ? '最新投稿'
        : '发现值得一看的作品';
  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-[1480px] px-4 py-8 sm:px-6 lg:px-8">
        <section className="mb-8 rounded-2xl border border-white/8 bg-gradient-to-r from-[#261b20] via-[#14171f] to-[#10131a] p-6 sm:p-9">
          <p className="mb-3 flex items-center gap-2 text-xs text-[#ff8a80]">
            <Sparkles className="size-4" />
            分享创作，遇见同好
          </p>
          <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
          <p className="mt-3 text-sm text-white/45">
            看视频、聊感受，也可以去直播间一起度过此刻。
          </p>
        </section>
        <DiscoveryNavigation mode={mode} category={category} />
        <DiscoveryGrid
          key={`${apiBase}:${user?.user_id}:${mode}:${category}`}
          mode={mode}
          category={category}
        />
      </main>
    </>
  );
}

function DiscoveryGrid({
  mode,
  category,
}: {
  mode: VideoFeedMode;
  category: string;
}) {
  const { apiBase } = useAuth();
  const [items, setItems] = useState<Array<VideoCardData & { token: string }>>(
    [],
  );
  const [cursor, setCursor] = useState('');
  const [next, setNext] = useState('');
  const [more, setMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expired, setExpired] = useState(false);
  const [effectiveMode, setEffectiveMode] = useState(mode);
  const [degraded, setDegraded] = useState(false);
  const [revision, setRevision] = useState(0);
  const sentinel = useRef<HTMLDivElement>(null);
  const pending = useRef(true);
  useEffect(() => {
    const controller = new AbortController();
    void videoDiscovery
      .page(apiBase, effectiveMode, category, cursor, controller.signal)
      .then(({ data }) => {
        if (controller.signal.aborted) return;
        if (data.has_more && (!data.next_cursor || data.next_cursor === cursor))
          throw new Error('列表分页异常，请刷新');
        setItems((previous) => {
          const seen = new Set(previous.map((v) => v.video_id));
          return [
            ...previous,
            ...data.items
              .filter((v) => !seen.has(v.video_id))
              .map((v) => ({ ...v, token: data.delivery_token })),
          ];
        });
        setNext(data.next_cursor);
        setMore(data.has_more);
        // Keep the selected API mode until the next request, so a fallback does
        // not immediately refetch/append a different first page.
        if (data.degraded) setDegraded(true);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : '加载失败');
          setExpired(
            err instanceof ApiError && [400, 410].includes(err.status),
          );
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          pending.current = false;
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [apiBase, effectiveMode, category, cursor, revision]);
  const loadMore = useCallback(() => {
    if (pending.current || !more || !next || error) return;
    pending.current = true;
    setLoading(true);
    setError('');
    if (degraded) setEffectiveMode('latest');
    setCursor(next);
  }, [more, next, error, degraded]);
  useEffect(() => {
    if (!sentinel.current) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) loadMore();
      },
      { rootMargin: '400px' },
    );
    observer.observe(sentinel.current);
    return () => observer.disconnect();
  }, [loadMore, loading]);
  function refresh() {
    pending.current = true;
    setLoading(true);
    setError('');
    setExpired(false);
    setItems([]);
    setCursor('');
    setNext('');
    setMore(false);
    setEffectiveMode(mode);
    setDegraded(false);
    setRevision((v) => v + 1);
  }
  function retry() {
    if (expired) {
      refresh();
      return;
    }
    pending.current = true;
    setLoading(true);
    setError('');
    setRevision((v) => v + 1);
  }
  return (
    <>
      {degraded && (
        <p className="mt-5 text-sm text-amber-200/75">
          推荐暂时不可用，当前显示最新投稿。
          <button onClick={refresh} className="ml-2 underline">
            重新加载
          </button>
        </p>
      )}
      <div className="mt-6 grid grid-cols-1 gap-x-5 gap-y-7 min-[460px]:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {items.map((item) => (
          <DiscoveryCard key={item.video_id} item={item} token={item.token} />
        ))}
      </div>
      {error && (
        <div role="alert" className="mt-8 text-center text-red-300">
          <p>{error}</p>
          <button className="creator-secondary mx-auto mt-3" onClick={retry}>
            {expired ? '刷新列表' : '重试'}
          </button>
        </div>
      )}
      {loading && (
        <output className="block py-10 text-center text-sm text-white/45">
          正在寻找精彩内容…
        </output>
      )}
      {!loading && !error && !items.length && (
        <div className="creator-panel mt-8 p-12 text-center">
          <Film className="mx-auto mb-4 size-10 text-white/20" />
          <p className="text-white/55">这里还没有公开视频</p>
          <Link
            href="/upload"
            className="mt-4 inline-block text-sm text-[#ff8a80]"
          >
            发布你的第一条作品
          </Link>
        </div>
      )}
      <div ref={sentinel} className="h-1" />
      {more && !error && (
        <button
          className="creator-secondary mx-auto my-8"
          disabled={loading}
          onClick={loadMore}
        >
          加载更多
        </button>
      )}
      {!more && items.length > 0 && !loading && (
        <p className="py-10 text-center text-xs text-white/30">
          这批内容已经看完了 ·{' '}
          <button onClick={refresh} className="hover:text-white">
            刷新看看新投稿
          </button>
        </p>
      )}
    </>
  );
}

export function DiscoveryCard({
  item,
  token = '',
}: {
  item: VideoCardData;
  token?: string;
}) {
  const { apiBase, user } = useAuth();
  const node = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!node.current || !token || !user) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let sent = false;
    let inView = false;
    const schedule = () => {
      clearTimeout(timer);
      if (!sent && inView && document.visibilityState === 'visible') {
        timer = setTimeout(() => {
          if (document.visibilityState !== 'visible') return;
          sent = true;
          void videoDiscovery
            .behavior(apiBase, item.video_id, token, 'impression')
            .catch(() => {});
        }, 1000);
      }
    };
    const observer = new IntersectionObserver(
      ([entry]) => {
        inView = entry.intersectionRatio >= 0.5;
        schedule();
      },
      { threshold: [0, 0.5] },
    );
    observer.observe(node.current);
    document.addEventListener('visibilitychange', schedule);
    return () => {
      observer.disconnect();
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', schedule);
    };
  }, [apiBase, item.video_id, token, user]);
  const click = () => {
    if (user && token)
      void videoDiscovery
        .behavior(apiBase, item.video_id, token, 'click')
        .catch(() => {});
  };
  return (
    <article ref={node} className="group min-w-0">
      <Link
        href={`/video/${item.video_id}`}
        onClick={click}
        className="relative block aspect-video overflow-hidden rounded-xl border border-white/8 bg-[#151923]"
      >
        {item.cover_url ? (
          <Image
            src={item.cover_url}
            alt=""
            fill
            sizes="(max-width: 640px) 100vw, 33vw"
            unoptimized
            className="object-cover transition duration-300 group-hover:scale-105"
          />
        ) : (
          <div className="grid size-full place-items-center bg-gradient-to-br from-[#30222b] to-[#101923]">
            <Play className="size-10 text-white/20" />
          </div>
        )}
        <span className="absolute bottom-2 right-2 rounded bg-black/65 px-1.5 py-0.5 text-[11px] text-white/85">
          {watchTime(item.duration_ms)}
        </span>
      </Link>
      <Link
        href={`/video/${item.video_id}`}
        onClick={click}
        className="mt-3 line-clamp-2 text-sm font-medium leading-6 text-white/85 hover:text-[#ff8a80]"
      >
        {item.title}
      </Link>
      <div className="mt-2 flex items-center justify-between gap-3 text-xs text-white/35">
        <Link
          href={`/space/${item.owner_id}`}
          className="truncate hover:text-white/70"
        >
          {item.owner_name}
        </Link>
        <time className="shrink-0" dateTime={item.published_at}>
          {new Date(item.published_at).toLocaleDateString('zh-CN')}
        </time>
      </div>
    </article>
  );
}
