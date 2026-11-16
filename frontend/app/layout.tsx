import type { Metadata } from 'next';
import './globals.css';
import { AuthProvider } from '@/components/auth-provider';

export const metadata: Metadata = {
  title: 'LIVE NOW · 视频与直播社区',
  description: '发现视频与直播，关注创作者，分享弹幕与评论',
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
