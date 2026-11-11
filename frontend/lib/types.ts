export interface ApiEnvelope<T> { data: T; message: string; }

export interface User {
  user_id: number;
  user_name: string;
  user_email: string;
  avatar_url?: string;
  created_at: string;
  updated_at: string;
}

export interface Room {
  room_id: number;
  room_title: string;
  status: number;
  owner_id: number;
  owner_username?: string;
  created_at?: string;
  updated_at?: string;
  start_at?: string | null;
  like_count?: number;
  hot_score?: number;
  praise_count?: number;
  play_url?: string;
}

export interface DanmakuMessage {
  id?: number;
  message_id: string;
  room_id: number;
  room_seq?: number;
  ack_seq?: number;
  user_id: number;
  user_name: string;
  content: string;
  type: string;
  data?: Record<string, unknown>;
  created_at?: string;
}

export interface HistoryPage { items: DanmakuMessage[]; next_cursor: number; has_more: boolean; }
export interface ReplayPage { items: DanmakuMessage[]; next_seq: number; has_more: boolean; source: string; }
export type ConnectionState = 'connecting' | 'open' | 'recovering' | 'closed' | 'error';


export interface LiveFeedPage {
  items: Room[];
  next_cursor: string;
  has_more: boolean;
}