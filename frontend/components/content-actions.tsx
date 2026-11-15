'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { Heart, Bookmark } from 'lucide-react';
import { AuthDialog } from '@/components/auth-dialog';
import { useAuth } from '@/components/auth-provider';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { ReportButton } from '@/components/report-button';
import {
  interactions,
  interactionError,
  type FavoriteFolder,
  type LikeState,
} from '@/lib/interactions';

export function ContentActions({
  kind,
  id,
}: {
  kind: 'video' | 'post';
  id: string;
}) {
  const { user, apiBase } = useAuth();
  return (
    <Actions
      key={`${apiBase}:${user?.user_id}:${kind}:${id}`}
      kind={kind}
      id={id}
    />
  );
}
function Actions({ kind, id }: { kind: 'video' | 'post'; id: string }) {
  const { user, apiBase } = useAuth();
  const [state, setState] = useState<LikeState | null>(null),
    [error, setError] = useState(''),
    [retry, setRetry] = useState(0);
  const [login, setLogin] = useState(false),
    [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false);
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .like(apiBase, kind, id, undefined, c.signal)
      .then(({ data }) => {
        if (!c.signal.aborted) {
          setState(data);
          setError('');
        }
      })
      .catch((err) => {
        if (!c.signal.aborted) setError(interactionError(err));
      });
    return () => c.abort();
  }, [apiBase, kind, id, retry]);
  async function like() {
    if (!user) {
      setLogin(true);
      return;
    }
    if (!state || pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const { data } = await interactions.like(apiBase, kind, id, !state.liked);
      setState(data);
      setError('');
    } catch (err) {
      setError(interactionError(err));
      setState(null);
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <section
      className="mt-5 border-y border-white/8 py-4"
      aria-label="内容互动"
    >
      <div className="flex flex-wrap items-center gap-4">
        <button
          className={state?.liked ? 'creator-primary' : 'creator-secondary'}
          aria-pressed={state?.liked ?? false}
          disabled={busy || (!state && !error)}
          onClick={() => (state ? void like() : setRetry((v) => v + 1))}
        >
          <Heart className={`size-4 ${state?.liked ? 'fill-current' : ''}`} />
          {!state
            ? error
              ? '重试点赞状态'
              : '加载中…'
            : `${state.liked ? '已点赞' : '点赞'} ${state.like_count}`}
        </button>
        {kind === 'video' && (
          <button
            className="creator-secondary"
            onClick={() => (user ? setOpen(true) : setLogin(true))}
          >
            <Bookmark className="size-4" />
            收藏
          </button>
        )}
        <ReportButton kind={kind} id={id} />
      </div>
      {error && (
        <p role="alert" className="mt-2 text-xs text-red-300">
          {error}
        </p>
      )}
      <AuthDialog open={login} onOpenChange={setLogin} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogTitle>收藏到</DialogTitle>
          <DialogDescription>
            收藏夹仅自己可见。勾选后立即保存。
          </DialogDescription>
          {open && <FavoritePicker id={id} />}
        </DialogContent>
      </Dialog>
    </section>
  );
}
function FavoritePicker({ id }: { id: string }) {
  const { apiBase } = useAuth();
  const [folders, setFolders] = useState<FavoriteFolder[] | null>(null),
    [selected, setSelected] = useState<string[]>([]),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [retry, setRetry] = useState(0);
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void Promise.all([
      interactions.folders(apiBase, c.signal),
      interactions.membership(apiBase, id, c.signal),
    ])
      .then(([f, s]) => {
        if (!c.signal.aborted) {
          setFolders(f.data);
          setSelected(s.data);
          setError('');
        }
      })
      .catch((err) => {
        if (!c.signal.aborted) setError(interactionError(err));
      });
    return () => c.abort();
  }, [apiBase, id, retry]);
  async function toggle(folder: string) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const save = !selected.includes(folder);
      await interactions.favorite(apiBase, folder, id, save);
      setSelected((v) =>
        save ? [...v, folder] : v.filter((x) => x !== folder),
      );
      setError('');
    } catch (err) {
      setError(interactionError(err));
      setFolders(null);
    } finally {
      setBusy(false);
      pending.current = false;
    }
  }
  return (
    <>
      <div className="max-h-64 space-y-2 overflow-y-auto">
        {folders?.map((f) => (
          <label
            key={f.folder_id}
            className="flex items-center gap-3 rounded-lg bg-white/5 p-3"
          >
            <input
              type="checkbox"
              checked={selected.includes(f.folder_id)}
              disabled={busy}
              onChange={() => void toggle(f.folder_id)}
            />
            <span className="break-all">{f.name}</span>
          </label>
        ))}
      </div>
      {!folders && !error && <p>正在读取收藏夹…</p>}
      {error && (
        <p role="alert" className="text-red-300">
          {error}
          <button
            className="ml-2 underline"
            onClick={() => setRetry((v) => v + 1)}
          >
            重新加载
          </button>
        </p>
      )}
      <Link href="/me/favorites" className="text-sm text-[#ff8a80]">
        管理收藏夹 →
      </Link>
    </>
  );
}
