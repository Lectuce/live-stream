'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { Bookmark, FolderPlus, LockKeyhole, Film } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import {
  interactions,
  interactionError,
  type FavoriteFolder,
  type FavoriteItem,
} from '@/lib/interactions';
import type { Page } from '@/lib/types';

export function MemberGate({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const [open, setOpen] = useState(false);
  if (user) return <>{children}</>;
  return (
    <div className="creator-panel mt-8 p-12 text-center">
      <p>{loading ? '正在确认登录状态…' : '登录后查看自己的收藏和消息'}</p>
      {!loading && (
        <button
          className="creator-primary mx-auto mt-5"
          onClick={() => setOpen(true)}
        >
          登录
        </button>
      )}
      <AuthDialog open={open} onOpenChange={setOpen} />
    </div>
  );
}
export function Favorites() {
  const { user, apiBase } = useAuth();
  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-6xl px-4 py-8">
        <div className="flex items-center gap-3">
          <Bookmark className="size-7 text-[#ff786c]" />
          <h1 className="text-2xl font-semibold">我的收藏</h1>
        </div>
        <p className="mt-3 flex items-center gap-2 text-sm text-white/45">
          <LockKeyhole className="size-3.5" />
          收藏夹仅自己可见，留住想再看一遍的内容。
        </p>
        <MemberGate>
          <Folders key={`${apiBase}:${user?.user_id}`} />
        </MemberGate>
      </main>
    </>
  );
}
function Folders() {
  const { apiBase } = useAuth();
  const [folders, setFolders] = useState<FavoriteFolder[]>([]),
    [selected, setSelected] = useState(''),
    [error, setError] = useState(''),
    [revision, setRevision] = useState(0),
    [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<FavoriteFolder | 'new' | null>(null),
    [name, setName] = useState(''),
    [removing, setRemoving] = useState<FavoriteFolder | null>(null),
    [busy, setBusy] = useState(false),
    [formError, setFormError] = useState('');
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .folders(apiBase, c.signal)
      .then(({ data }) => {
        if (!c.signal.aborted) {
          setFolders(data);
          setSelected((v) =>
            data.some((f) => f.folder_id === v) ? v : data[0]?.folder_id || '',
          );
          setError('');
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!c.signal.aborted) {
          setError(interactionError(err));
          setLoading(false);
        }
      });
    return () => c.abort();
  }, [apiBase, revision]);
  async function save() {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setFormError('');
    try {
      if (removing)
        await interactions.changeFolder(apiBase, removing.folder_id);
      else if (editing === 'new') {
        const { data } = await interactions.createFolder(apiBase, name.trim());
        setSelected(data.folder_id);
      } else if (editing)
        await interactions.changeFolder(
          apiBase,
          editing.folder_id,
          name.trim(),
        );
      setEditing(null);
      setRemoving(null);
      setRevision((v) => v + 1);
    } catch (err) {
      setFormError(interactionError(err));
    } finally {
      setBusy(false);
      pending.current = false;
    }
  }
  const current = folders.find((f) => f.folder_id === selected);
  return (
    <>
      <div className="mt-8 grid gap-6 md:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="creator-panel h-fit p-3">
          <button
            className="creator-secondary mb-4 w-full"
            onClick={() => {
              setEditing('new');
              setName('');
              setFormError('');
            }}
          >
            <FolderPlus className="size-4" />
            新建收藏夹
          </button>
          <nav aria-label="收藏夹">
            {folders.map((f) => (
              <button
                key={f.folder_id}
                className={`creator-nav mb-1 w-full text-left ${selected === f.folder_id ? 'creator-nav-active' : ''}`}
                onClick={() => setSelected(f.folder_id)}
              >
                <Bookmark className="size-4 shrink-0" />
                <span className="truncate">{f.name}</span>
              </button>
            ))}
          </nav>
          {loading && <p className="p-3 text-sm text-white/40">加载中…</p>}
          {error && (
            <p role="alert" className="p-3 text-sm text-red-300">
              {error}
              <button
                className="ml-2 underline"
                onClick={() => setRevision((v) => v + 1)}
              >
                重试
              </button>
            </p>
          )}
        </aside>
        <section className="min-w-0">
          {current && (
            <>
              <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                <h2 className="break-all text-xl font-semibold">
                  {current.name}
                </h2>
                <div className="flex gap-3 text-sm text-white/50">
                  <button
                    onClick={() => {
                      setEditing(current);
                      setName(current.name);
                      setFormError('');
                    }}
                  >
                    重命名
                  </button>
                  {!current.is_default && (
                    <button
                      onClick={() => {
                        setRemoving(current);
                        setFormError('');
                      }}
                    >
                      删除收藏夹
                    </button>
                  )}
                </div>
              </div>
              <FavoriteList
                key={current.folder_id}
                folder={current.folder_id}
              />
            </>
          )}
        </section>
      </div>
      <Dialog
        open={!!editing || !!removing}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setEditing(null);
            setRemoving(null);
          }
        }}
      >
        <DialogContent>
          <DialogTitle>
            {removing
              ? '删除收藏夹'
              : editing === 'new'
                ? '新建收藏夹'
                : '重命名收藏夹'}
          </DialogTitle>
          <DialogDescription>
            {removing
              ? '将移除这个收藏夹及其中的收藏记录，视频本身不会删除。'
              : '名称最多60字，收藏夹仅你自己可见。'}
          </DialogDescription>
          {!removing && (
            <input
              aria-label="收藏夹名称"
              className="creator-input"
              maxLength={60}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          )}
          <p role="alert" className="text-red-300">
            {formError}
          </p>
          <button
            className="creator-primary"
            disabled={busy || (!removing && !name.trim())}
            onClick={() => void save()}
          >
            {busy ? '正在保存…' : removing ? '确认删除' : '保存'}
          </button>
        </DialogContent>
      </Dialog>
    </>
  );
}
function FavoriteList({ folder }: { folder: string }) {
  const { apiBase } = useAuth();
  const [page, setPage] = useState<Page<FavoriteItem> | null>(null),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [reload, setReload] = useState(0);
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .favorites(apiBase, folder, '', c.signal)
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
  }, [apiBase, folder, reload]);
  async function more() {
    if (!page?.has_more || pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const { data } = await interactions.favorites(
        apiBase,
        folder,
        page.next_cursor,
      );
      setPage((p) => ({
        ...data,
        items: [
          ...(p?.items || []),
          ...data.items.filter(
            (i) => !p?.items.some((v) => v.item_id === i.item_id),
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
  async function remove(video: string) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      await interactions.favorite(apiBase, folder, video, false);
      setPage((p) =>
        p ? { ...p, items: p.items.filter((i) => i.video_id !== video) } : p,
      );
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
      {error && (
        <p role="alert" className="mb-4 text-sm text-red-300">
          {error}
          <button
            className="ml-2 underline"
            onClick={() => setReload((v) => v + 1)}
          >
            重新加载
          </button>
        </p>
      )}
      {!page && !error && <p>正在加载收藏…</p>}
      {page?.items.length === 0 && (
        <div className="creator-panel p-14 text-center text-white/45">
          <Bookmark className="mx-auto mb-4 size-8" />
          这里还没有收藏
          <Link href="/video" className="mt-4 block text-[#ff8a80]">
            去视频广场看看 →
          </Link>
        </div>
      )}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {page?.items.map((item) => (
          <article key={item.item_id} className="creator-panel overflow-hidden">
            <div className="grid aspect-video place-items-center bg-white/5">
              {item.available && item.cover_url ? (
                <Image
                  src={item.cover_url}
                  alt=""
                  width={480}
                  height={270}
                  unoptimized
                  className="size-full object-cover"
                />
              ) : (
                <Film className="size-8 text-white/20" />
              )}
            </div>
            <div className="p-4">
              {item.available ? (
                <Link
                  className="line-clamp-2 text-sm font-medium hover:text-[#ff8a80]"
                  href={`/video/${item.video_id}`}
                >
                  {item.title}
                </Link>
              ) : (
                <p className="text-sm text-white/40">视频已下架或不可用</p>
              )}
              <button
                disabled={busy}
                className="mt-4 text-xs text-white/40 hover:text-white"
                onClick={() => void remove(item.video_id)}
              >
                移出收藏
              </button>
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
