const ADMIN_KEY = 'adminKey'

export function getAdminKey(): string {
  return sessionStorage.getItem(ADMIN_KEY) || ''
}

export function setAdminKey(key: string): void {
  sessionStorage.setItem(ADMIN_KEY, key)
}

export function clearAdminKey(): void {
  sessionStorage.removeItem(ADMIN_KEY)
}

export class AuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthError'
  }
}

export async function adminApi<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const key = getAdminKey()
  if (key) headers['X-API-Key'] = key
  const opts: RequestInit = { method, headers }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const r = await fetch(path, opts)
  if (r.status === 401 || r.status === 403) {
    throw new AuthError('auth')
  }
  if (r.status === 204) return null as T
  const data = await r.json()
  if (!r.ok) throw new Error((data as Record<string, string>).error || r.statusText)
  return data as T
}
