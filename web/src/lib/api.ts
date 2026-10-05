// Thin fetch wrapper for /api/v1. Every error the server sends has the shape
// {"error": {"code", "message"}}; it surfaces here as an ApiError.

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', 'Cannot reach the panel. Check your connection.')
  }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const e = data?.error
    throw new ApiError(res.status, e?.code ?? 'unknown', e?.message ?? `Request failed (${res.status}).`)
  }
  return data as T
}

export type Role = 'admin' | 'user'

export type User = {
  id: string
  email: string
  name: string
  role: Role
  createdAt: string
}

export const api = {
  me: () => request<User>('GET', '/me'),
  login: (email: string, password: string) => request<User>('POST', '/auth/login', { email, password }),
  logout: () => request<void>('POST', '/auth/logout'),
  health: () => request<{ status: string }>('GET', '/health'),
}
