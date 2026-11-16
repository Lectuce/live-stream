'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { request } from '@/lib/api';
import type { ApiEnvelope, Page } from '@/lib/types';

type SearchItem = { id: string; title: string; image: string; kind: string };
export function DiscoverySearch({
  term,
  kind,
}: {
  term: string;
  kind: string;
}) {
  const { apiBase } = useAuth();
  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-5xl px-4 py-8">
        <h1 className="text-2xl font-semibold">搜索</h1>
        <form action="/search" className="my-6 flex gap-3">
          <input
            name="q"
            aria-label="搜索内容"
            required
            maxLength={50}
            defaultValue={term}
            className="creator-input"
            placeholder="输入关键词"
          />
          <input type="hidden" name="type" value={kind} />
          <button className="creator-primary">搜索</button>
        </form>
        <nav className="flex gap-3">
          {[
            ['video', '视频'],
            ['user', '用户'],
            ['live', '直播'],
          ].map(([type, name]) => (
            <Link
              key={type}
              href={`/search?${new URLSearchParams({ q: term, type })}`}
              className={`rounded-full px-5 py-2 text-sm ${kind === type ? 'bg-[#ff6255]' : 'bg-white/5 text-white/50'}`}
            >
              {name}
            </Link>
          ))}
        </nav>
        <p className="mt-4 text-xs text-white/35">
          当前搜索最近收录的内容，较早的作品可前往作者主页查找。
        </p>
        {term.trim() ? (
          <SearchResults
            key={`${apiBase}:${kind}:${term}`}
            kind={kind}
            term={term.trim()}
          />
        ) : (
          <p className="py-16 text-center text-white/40">输入关键词开始搜索</p>
        )}
      </main>
    </>
  );
}
function SearchResults({ term, kind }: { term: string; kind: string }) {
  const { apiBase } = useAuth();
  const [cursor, setCursor] = useState('');
  const [result, setResult] = useState<{
    cursor: string;
    page?: Page<SearchItem>;
    error?: string;
  } | null>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    void request<ApiEnvelope<Page<SearchItem>>>(
      apiBase,
      `/api/v1/search?${new URLSearchParams({ q: term, type: kind, cursor, limit: '20' })}`,
      { signal: c.signal, cache: 'no-store' },
    )
      .then(({ data }) => {
        if (!c.signal.aborted) setResult({ cursor, page: data });
      })
      .catch((e: unknown) => {
        if (!c.signal.aborted)
          setResult({
            cursor,
            error: e instanceof Error ? e.message : '搜索失败',
          });
      });
    return () => c.abort();
  }, [apiBase, kind, term, cursor, retry]);
  const current = result?.cursor === cursor ? result : null;
  return (
    <section className="mt-6 space-y-3">
      {!current && (
        <output className="block py-8 text-white/45">正在搜索…</output>
      )}
      {current?.error && (
        <p role="alert" className="text-red-300">
          {current.error}{' '}
          <button
            className="underline"
            onClick={() => {
              setResult(null);
              setRetry((v) => v + 1);
            }}
          >
            重试
          </button>
        </p>
      )}
      {current?.page?.items.map((item) => (
        <Link
          key={item.id}
          href={
            kind === 'video'
              ? `/video/${item.id}`
              : kind === 'user'
                ? `/space/${item.id}`
                : `/rooms/${item.id}`
          }
          className="creator-panel block p-5 hover:border-white/20"
        >
          <p className="font-medium">{item.title}</p>
          <p className="mt-2 text-xs text-white/35">
            {kind === 'video'
              ? '观看视频'
              : kind === 'user'
                ? '查看作者主页'
                : '进入直播间'}
          </p>
        </Link>
      ))}
      {current?.page?.items.length === 0 && (
        <p className="py-12 text-center text-white/40">
          没有找到相关结果，试试其他关键词
        </p>
      )}
      <div className="flex gap-4 py-5">
        {cursor && (
          <button className="creator-secondary" onClick={() => setCursor('')}>
            返回第一页
          </button>
        )}
        {current?.page?.has_more && (
          <button
            className="creator-secondary"
            onClick={() => setCursor(current.page!.next_cursor)}
          >
            下一页
          </button>
        )}
      </div>
    </section>
  );
}
