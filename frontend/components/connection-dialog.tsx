'use client';

import { useState } from 'react';
import { CheckCircle2, LoaderCircle, Server } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/lib/api';
import { defaultApiBase, normalizeApiBase, saveApiBase } from '@/lib/runtime-config';

export function ConnectionDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [value, setValue] = useState(() => defaultApiBase());
  const [status, setStatus] = useState<'idle' | 'checking' | 'ok' | 'error'>('idle');
  const [message, setMessage] = useState('');

  async function testAndSave() {
    setStatus('checking'); setMessage('');
    try {
      const base = normalizeApiBase(value);
      await api.listRooms(base);
      saveApiBase(base);
      setStatus('ok'); setMessage('连接成功，配置已保存');
      window.setTimeout(() => onOpenChange(false), 700);
    } catch (error) {
      setStatus('error'); setMessage(error instanceof Error ? error.message : '连接失败');
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="border-white/10 bg-[#11151d] sm:max-w-[460px]">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><Server className="size-4 text-[#ff6255]" />后端连接</DialogTitle><DialogDescription>浏览器固定走当前页面的同源代理。后端位于其他机器时，请在启动前端前设置 LIVE_STREAM_BACKEND_URL，不能在这里直填远端 9000 端口。</DialogDescription></DialogHeader>
        <div className="space-y-3"><Input value={value} onChange={(e) => setValue(e.target.value)} className="h-11 font-mono text-sm" placeholder="http://localhost:3000" />
          {message && <p className={`text-sm ${status === 'ok' ? 'text-emerald-400' : 'text-red-300'}`}>{message}</p>}
          <Button onClick={testAndSave} disabled={status === 'checking'} className="h-10 w-full">{status === 'checking' ? <LoaderCircle className="animate-spin" /> : status === 'ok' ? <CheckCircle2 /> : <Server />}测试并保存</Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
