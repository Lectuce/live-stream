'use client';

import { useState } from 'react';
import { CheckCircle2, LoaderCircle, Server } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/lib/api';
import { defaultApiBase, saveApiBase } from '@/lib/runtime-config';

export function ConnectionDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [value, setValue] = useState(() => defaultApiBase());
  const [status, setStatus] = useState<'idle' | 'checking' | 'ok' | 'error'>('idle');
  const [message, setMessage] = useState('');

  async function testAndSave() {
    setStatus('checking'); setMessage('');
    try {
      const base = saveApiBase(value);
      await api.listRooms(base);
      setStatus('ok'); setMessage('连接成功，配置已保存');
      window.setTimeout(() => onOpenChange(false), 700);
    } catch (error) {
      setStatus('error'); setMessage(error instanceof Error ? error.message : '连接失败');
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="border-white/10 bg-[#11151d] sm:max-w-[460px]">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><Server className="size-4 text-[#ff6255]" />后端连接</DialogTitle><DialogDescription>开发环境建议保留当前页面地址并通过 LIVE_STREAM_BACKEND_URL 代理后端，登录 Cookie 才能保持同源。</DialogDescription></DialogHeader>
        <div className="space-y-3"><Input value={value} onChange={(e) => setValue(e.target.value)} className="h-11 font-mono text-sm" placeholder="http://172.21.15.57:9000" />
          {message && <p className={`text-sm ${status === 'ok' ? 'text-emerald-400' : 'text-red-300'}`}>{message}</p>}
          <Button onClick={testAndSave} disabled={status === 'checking'} className="h-10 w-full">{status === 'checking' ? <LoaderCircle className="animate-spin" /> : status === 'ok' ? <CheckCircle2 /> : <Server />}测试并保存</Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
