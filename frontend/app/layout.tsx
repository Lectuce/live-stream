import type { Metadata } from 'next';
import './globals.css';
import { AuthProvider } from '@/components/auth-provider';

export const metadata: Metadata = {
  title: 'LIVE NOW · 实时直播间',
  description: '发现精彩直播，与主播和观众实时互动',
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
