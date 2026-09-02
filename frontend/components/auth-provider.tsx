'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { api, ApiError } from '@/lib/api';
import { defaultApiBase } from '@/lib/runtime-config';
import type { User } from '@/lib/types';

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  apiBase: string;
  refresh: () => Promise<void>;
  login: (email: string, password: string) => Promise<void>;
  register: (username: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [apiBase, setApiBase] = useState(() => defaultApiBase());

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.me(apiBase);
      setUser(result.data);
    } catch (error) {
      if (error instanceof ApiError && error.status !== 401 && error.status !== 0) console.warn(error.message);
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, [apiBase]);

  useEffect(() => {
    const listener = (event: Event) => setApiBase((event as CustomEvent<string>).detail);
    window.addEventListener('live-stream-api-change', listener);
    return () => window.removeEventListener('live-stream-api-change', listener);
  }, []);

  useEffect(() => { queueMicrotask(() => void refresh()); }, [refresh]);

  const value = useMemo<AuthContextValue>(() => ({
    user,
    loading,
    apiBase,
    refresh,
    login: async (email, password) => {
      const result = await api.login(apiBase, email, password);
      setUser(result.data);
    },
    register: async (username, email, password) => {
      await api.register(apiBase, username, email, password);
      const result = await api.login(apiBase, email, password);
      setUser(result.data);
    },
    logout: async () => {
      await api.logout(apiBase);
      setUser(null);
    },
  }), [apiBase, loading, refresh, user]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used inside AuthProvider');
  return context;
}
