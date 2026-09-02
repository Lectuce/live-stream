'use client';

const API_STORAGE_KEY = 'live-stream-api-base';

export function defaultApiBase(): string {
  if (typeof window === 'undefined') return 'http://127.0.0.1:9000';
  const configured = window.localStorage.getItem(API_STORAGE_KEY);
  if (configured) return configured;
  // 开发时默认走 Vinext/Vite 的同源反向代理，这样 Session Cookie
  // 不会因为 localhost 与局域网 IP 跨站而被浏览器拦截。
  return window.location.origin;
}

export function normalizeApiBase(value: string): string {
  const trimmed = value.trim().replace(/\/$/, '');
  if (!/^https?:\/\//i.test(trimmed)) throw new Error('地址必须以 http:// 或 https:// 开头');
  return trimmed;
}

export function saveApiBase(value: string): string {
  const normalized = normalizeApiBase(value);
  window.localStorage.setItem(API_STORAGE_KEY, normalized);
  window.dispatchEvent(new CustomEvent('live-stream-api-change', { detail: normalized }));
  return normalized;
}

export function toWebSocketBase(apiBase: string): string {
  return apiBase.replace(/^http:/i, 'ws:').replace(/^https:/i, 'wss:');
}
