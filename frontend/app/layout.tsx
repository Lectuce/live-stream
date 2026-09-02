import type { Metadata } from 'next';
import './globals.css';
import { AuthProvider } from '@/components/auth-provider';

export const metadata: Metadata = {
  title: 'LIVE NOW · 实时直播间',
  description: '基于 Gin、WebSocket、Kafka 与 Redis 的实时直播弹幕前端',
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className="dark">
      <body><AuthProvider>{children}</AuthProvider></body>
    </html>
  );
}
