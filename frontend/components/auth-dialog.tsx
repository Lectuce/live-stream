'use client';

import { useState } from 'react';
import { LoaderCircle } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useAuth } from '@/components/auth-provider';

export function AuthDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { login, register } = useAuth();
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      if (mode === 'register') await register(username, email, password);
      else await login(email, password);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败');
    } finally { setSubmitting(false); }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="border-white/10 bg-[#11151d] p-0 sm:max-w-[430px]">
        <div className="border-b border-white/8 px-6 pb-5 pt-6">
          <DialogHeader>
            <div className="mb-2 flex items-center gap-2 text-xs font-semibold tracking-[.16em] text-[#ff6b5e]">
              <span className="live-dot size-2 rounded-full bg-[#ff4d3d]" /> LIVE NOW
            </div>
            <DialogTitle className="text-2xl font-bold tracking-tight">{mode === 'login' ? '欢迎回来' : '创建你的账号'}</DialogTitle>
            <DialogDescription>{mode === 'login' ? '登录后即可创建直播间并发送弹幕。' : '注册完成后会自动登录。'}</DialogDescription>
          </DialogHeader>
        </div>
        <form onSubmit={submit} className="space-y-4 px-6 pb-6">
          <div className="grid grid-cols-2 rounded-lg bg-white/5 p-1">
            {(['login', 'register'] as const).map((item) => (
              <button key={item} type="button" onClick={() => { setMode(item); setError(''); }} className={`rounded-md px-3 py-2 text-sm transition ${mode === item ? 'bg-white/10 text-white shadow-sm' : 'text-white/45 hover:text-white/75'}`}>
                {item === 'login' ? '登录' : '注册'}
              </button>
            ))}
          </div>
          {mode === 'register' && <label htmlFor="auth-username" className="block space-y-2 text-sm text-white/65"><span>用户名</span><Input id="auth-username" value={username} onChange={(e) => setUsername(e.target.value)} minLength={2} maxLength={32} required placeholder="2–32 个字符" className="h-11 bg-white/4" /></label>}
          <label htmlFor="auth-email" className="block space-y-2 text-sm text-white/65"><span>邮箱</span><Input id="auth-email" value={email} onChange={(e) => setEmail(e.target.value)} type="email" required placeholder="name@example.com" className="h-11 bg-white/4" /></label>
          <label htmlFor="auth-password" className="block space-y-2 text-sm text-white/65"><span>密码</span><Input id="auth-password" value={password} onChange={(e) => setPassword(e.target.value)} type="password" minLength={8} maxLength={128} required placeholder="至少 8 位" className="h-11 bg-white/4" /></label>
          {error && <p role="alert" className="rounded-lg border border-red-400/15 bg-red-400/8 px-3 py-2 text-sm text-red-300">{error}</p>}
          <Button type="submit" disabled={submitting} className="h-11 w-full bg-[#ff4d3d] hover:bg-[#ff6356]">
            {submitting && <LoaderCircle className="animate-spin" />}{mode === 'login' ? '登录' : '注册并登录'}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
