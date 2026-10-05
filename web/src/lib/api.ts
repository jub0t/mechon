// Typed client for /api/v1. Every error the server sends has the shape
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
  const isForm = body instanceof FormData
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      credentials: 'same-origin',
      headers: body === undefined || isForm ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
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

const get = <T>(p: string) => request<T>('GET', p)
const post = <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {})
const patch = <T>(p: string, b: unknown) => request<T>('PATCH', p, b)
const put = <T>(p: string, b: unknown) => request<T>('PUT', p, b)
const del = (p: string) => request<void>('DELETE', p)

// ---------- Types ----------

export type Role = 'admin' | 'user'

export type User = {
  id: string
  email: string
  name: string
  role: Role
  externalId: string | null
  suspendedAt: string | null
  hasPassword: boolean
  createdAt: string
}

export type AdminUser = User & { subscriptionCount: number; botCount: number }

export type Capacity = { memoryMb: number; cpuMillicores: number; diskMb: number }

export type Plan = Capacity & {
  id: string
  slug: string
  name: string
  maxBots: number
  pidsMax: number
  hardened: boolean
  templates: string[]
  subscriptionCount: number
  createdAt: string
}

export type PlanInput = Omit<Plan, 'id' | 'subscriptionCount' | 'createdAt'>

export type SubscriptionStatus = 'active' | 'suspended' | 'terminated'

export type Limits = Capacity & { maxBots: number; pidsMax: number }
export type Overrides = { [K in keyof Limits]: number | null }

export type Subscription = {
  id: string
  status: SubscriptionStatus
  externalId: string | null
  plan: Plan
  /** The plan with this customer's overrides applied. */
  limits: Limits
  overrides: Overrides
  note: string
  used: Capacity & { bots: number }
  createdAt: string
}

export type ApiKey = {
  id: string
  name: string
  prefix: string
  scopes: string[]
  lastUsedAt: string | null
  expiresAt: string | null
  createdAt: string
}

export type Template = {
  id: string
  name: string
  language: string
  description: string
  image: string
  env: { key: string; label: string; secret: boolean; required: boolean; default?: string }[]
}

export type HostInfo = {
  agentVersion: string
  hostname: string
  kernel: string
  dockerVersion: string
  cgroupV2: boolean
  runsc: boolean
  cpus: number
  memoryBytes: number
  diskBytes: number
}

export type NodeUsage = { cpuPercent: number; memoryBytes: number; diskBytes: number }

export type Node = {
  id: string
  name: string
  region: string
  capacity: Capacity
  allocated: Capacity
  overcommit: number
  maintenance: boolean
  botCount: number
  agentVersion: string
  online: boolean
  lastSeenAt: string | null
  createdAt: string
  host?: HostInfo
  usage?: NodeUsage
}

export type NodeInput = Capacity & { name: string; region: string; overcommit: number; maintenance: boolean }

export type AgentSetup = { token: string; panelUrl: string; command: string }

export type BotState = 'pending' | 'installing' | 'running' | 'stopped' | 'crashed' | 'unknown'

export type BotUsage = { botId: string; cpuPercent: number; memoryBytes: number; diskBytes: number; netRxBytes: number; netTxBytes: number }

export type Bot = {
  id: string
  name: string
  template: string
  state: BotState
  desired: 'running' | 'stopped'
  error: string
  exitCode: number | null
  restarts: number
  limits: Capacity
  uid: number
  node: { id: string; name: string }
  owner: { id: string; name: string; email: string }
  plan: string
  subscriptionId: string
  subscriptionStatus?: SubscriptionStatus
  currentDeployId: string | null
  stateChangedAt: string
  createdAt: string
  usage?: BotUsage
}

export type DeployStatus = 'queued' | 'fetching' | 'installing' | 'live' | 'failed' | 'superseded'

export type Deploy = {
  id: string
  number: number
  source: 'upload' | 'git' | 'api' | 'rollback'
  status: DeployStatus
  error: string
  sha256: string
  bytes: number
  rollbackOf: string | null
  createdBy: string | null
  createdAt: string
  finishedAt: string | null
  current: boolean
  gitUrl?: string
  gitRef?: string
  gitCommit?: string
  log?: string
}

export type EnvVar = { key: string; value: string; secret: boolean }

export type LogLine = { t: number; stream: 'stdout' | 'stderr' | 'system'; text: string }

export type MetricPoint = { t: number; cpu: number; memory: number; disk: number }

export type Overview = {
  bots: number
  botsByState: Partial<Record<BotState, number>>
  nodes: number
  nodesOnline: number
  users: number
  plans: number
  capacity: Capacity
  allocated: Capacity
}

// ---------- Endpoints ----------

export const api = {
  health: () => get<{ status: string }>('/health'),
  login: (email: string, password: string) => post<User>('/auth/login', { email, password }),
  logout: () => post<void>('/auth/logout'),

  me: () => get<User>('/me'),
  updateMe: (b: { name: string; email: string }) => patch<User>('/me', b),
  changePassword: (current: string, next: string) => post<void>('/me/password', { current, new: next }),
  mySubscriptions: () => get<Subscription[]>('/me/subscriptions'),
  keys: () => get<ApiKey[]>('/me/keys'),
  createKey: (b: { name: string; scopes: string[]; expiresInDays?: number }) => post<ApiKey & { secret: string }>('/me/keys', b),
  deleteKey: (id: string) => del(`/me/keys/${id}`),

  templates: () => get<Template[]>('/templates'),
  overview: () => get<Overview>('/overview'),

  plans: () => get<Plan[]>('/plans'),
  createPlan: (b: PlanInput) => post<Plan>('/plans', b),
  updatePlan: (id: string, b: PlanInput) => patch<Plan>(`/plans/${id}`, b),
  archivePlan: (id: string) => del(`/plans/${id}`),

  users: () => get<AdminUser[]>('/users'),
  createUser: (b: { email: string; name: string; password?: string; role: Role; planId?: string }) => post<User>('/users', b),
  updateUser: (id: string, b: { name: string; email: string; role: Role }) => patch<User>(`/users/${id}`, b),
  deleteUser: (id: string) => del(`/users/${id}`),
  resetPassword: (id: string, password: string) => post<void>(`/users/${id}/password`, { password }),
  suspendUser: (id: string, suspended: boolean) => post<User>(`/users/${id}/suspend`, { suspended }),
  userSubscriptions: (id: string) => get<Subscription[]>(`/users/${id}/subscriptions`),
  subscribe: (userId: string, planId: string) => post<{ id: string }>(`/users/${userId}/subscriptions`, { planId }),
  updateSubscription: (id: string, b: { status?: SubscriptionStatus; planId?: string; overrides?: Partial<Overrides>; note?: string }) => patch<unknown>(`/subscriptions/${id}`, b),

  nodes: () => get<Node[]>('/nodes'),
  createNode: (b: NodeInput) => post<{ node: Node; setup: AgentSetup }>('/nodes', b),
  updateNode: (id: string, b: NodeInput) => patch<Node>(`/nodes/${id}`, b),
  deleteNode: (id: string) => del(`/nodes/${id}`),
  rotateNodeToken: (id: string) => post<AgentSetup>(`/nodes/${id}/token`),

  bots: (mine = false) => get<Bot[]>(mine ? '/bots?mine=1' : '/bots'),
  bot: (id: string) => get<Bot>(`/bots/${id}`),
  createBot: (b: { subscriptionId: string; name: string; template: string; env: Record<string, string> } & Capacity) => post<Bot>('/bots', b),
  updateBot: (id: string, b: { name: string } & Capacity) => patch<Bot>(`/bots/${id}`, b),
  deleteBot: (id: string) => del(`/bots/${id}`),
  botAction: (id: string, action: 'start' | 'stop' | 'restart') => post<Bot>(`/bots/${id}/actions`, { action }),
  env: (id: string) => get<EnvVar[]>(`/bots/${id}/env`),
  putEnv: (id: string, vars: EnvVar[]) => put<void>(`/bots/${id}/env`, { vars }),
  metrics: (id: string, range: '1h' | '24h' | '7d') => get<MetricPoint[]>(`/bots/${id}/metrics?range=${range}`),
  deploys: (id: string) => get<Deploy[]>(`/bots/${id}/deploys`),
  deploy: (id: string, deployId: string) => get<Deploy>(`/bots/${id}/deploys/${deployId}`),
  upload: (id: string, file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return request<Deploy>('POST', `/bots/${id}/deploys`, fd)
  },
  gitDeploy: (id: string, b: { gitUrl: string; gitRef: string; token?: string }) => post<Deploy>(`/bots/${id}/deploys`, b),
  rollback: (id: string, deployId: string) => post<Deploy>(`/bots/${id}/deploys/${deployId}/rollback`),
}
