'use client';

const API_STORAGE_KEY = 'live-stream-api-base';

export function defaultApiBase(): string {
  if (typeof window === 'undefined') return 'http://127.0.0.1:9000';
  const pageOrigin = window.location.origin;
  const configured = window.localStorage.getItem(API_STORAGE_KEY);
  if (configured) {
    try {
      const configuredOrigin = new URL(configured).origin;
      if (configuredOrigin === pageOrigin) return configuredOrigin;
    } catch {
      // 无效的旧版配置值会在下方被丢弃。
    }
    // 旧版本允许在浏览器中保存远程后端 URL，这会绕过同源代理并破坏
    // Session Cookie 与 WebSocket Origin 的约定，因此自动迁移回页面来源。
    window.localStorage.removeItem(API_STORAGE_KEY);
  }
  // 开发时默认走 Vinext/Vite 的同源反向代理，这样 Session Cookie
  // 不会因为 localhost 与局域网 IP 跨站而被浏览器拦截。
  return pageOrigin;
}

export function normalizeApiBase(value: string): string {
  const trimmed = value.trim().replace(/\/$/, '');
  if (!/^https?:\/\//i.test(trimmed)) throw new Error('地址必须以 http:// 或 https:// 开头');
  const normalized = new URL(trimmed).origin;
  if (typeof window !== 'undefined' && normalized !== window.location.origin) {
    throw new Error('浏览器端必须使用当前页面地址；远端后端请通过 LIVE_STREAM_BACKEND_URL 配置代理');
  }
  return normalized;
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
