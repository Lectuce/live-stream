'use client';

import { useParams } from 'next/navigation';
import { LiveRoom } from '@/components/live-room';

export default function RoomPage() {
  const params = useParams<{ id: string }>();
  const roomId = Number(params.id);
  const safeRoomId = Number.isFinite(roomId) ? roomId : 0;
  return <LiveRoom key={safeRoomId} roomId={safeRoomId} />;
}
