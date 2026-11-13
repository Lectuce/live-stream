'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { Check, LoaderCircle, Plus } from 'lucide-react';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';
import { api } from '@/lib/api';
import type { FollowState } from '@/lib/types';

type Props = { userId: string; onChange?: (state: FollowState) => void };

export function FollowButton({ userId, onChange }: Props) {
  const { user, loading, apiBase } = useAuth();
  const [authOpen, setAuthOpen] = useState(false);
  if (loading) return <button disabled className="creator-secondary">正在加载…</button>;
  if (String(user?.user_id) === userId) return <Link href="/studio" className="creator-secondary">管理投稿</Link>;
  if (user) return <AuthenticatedFollow key={`${apiBase}:${user.user_id}:${userId}`} base={apiBase} userId={userId} onChange={onChange} />;
  return <>
    <button type="button" className="creator-primary" onClick={() => setAuthOpen(true)}><Plus className="size-4" />关注</button>
    <AuthDialog open={authOpen} onOpenChange={setAuthOpen} />
  </>;
}

function AuthenticatedFollow({ base, userId, onChange }: Props & { base: string }) {
  const [state, setState] = useState<FollowState | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [retry, setRetry] = useState(0);
  const pending = useRef(false);
  const mutation = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void api.followState(base, userId, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) { setState(data); setError(''); }
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '关注状态读取失败');
    });
    return () => { controller.abort(); mutation.current?.abort(); };
  }, [base, userId, retry]);

  async function toggle() {
    if (!state || pending.current) return;
    pending.current = true;
    setBusy(true);
    setError('');
    const controller = new AbortController();
    mutation.current = controller;
    try {
      const { data } = await api.setFollow(base, userId, !state.following, controller.signal);
      if (!controller.signal.aborted) { setState(data); onChange?.(data); }
    } catch (err) {
      if (!controller.signal.aborted) {
        // A lost response may follow a committed write. Re-read before another toggle.
        setState(null);
        setError(err instanceof Error ? err.message : '关注操作失败');
      }
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return <div className="flex flex-col items-start gap-2">
    <button type="button" aria-pressed={state?.following ?? false}
      disabled={busy || (!state && !error)}
      className={state?.following ? 'creator-secondary' : 'creator-primary'}
      onClick={() => { if (!state) { setError(''); setRetry((value) => value + 1); } else void toggle(); }}>
      {busy || (!state && !error) ? <LoaderCircle className="size-4 animate-spin" /> : state?.following ? <Check className="size-4" /> : <Plus className="size-4" />}
      {busy ? '处理中…' : !state ? error ? '重试关注状态' : '加载中…' : state.following ? '已关注 · 取消' : '关注'}
    </button>
    {error && <p role="alert" className="max-w-64 text-xs text-red-300">{error}</p>}
  </div>;
}
