'use client';

import { useParams } from 'next/navigation';
import { LiveRoom } from '@/components/live-room';

export default function RoomPage() {
  const params = useParams<{ id: string }>();
  const roomId = Number(params.id);
  return <LiveRoom roomId={Number.isFinite(roomId) ? roomId : 0} />;
}
