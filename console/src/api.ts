import { request as httpRequest } from '@devpablocristo/core-http/fetch'
import { registerTokenProvider } from '@devpablocristo/core-authn/http/fetch'
import { clerkEnabled, localDevBrowserAccessEnabled, localDevUserID } from './auth'

// Token provider: se registra desde AuthTokenBridge (Clerk) o queda null (API key)
let getClerkToken: (() => Promise<string | null>) | null = null
let currentIdentity: { userId: string | null; orgId: string | null } = {
  userId: localDevBrowserAccessEnabled ? localDevUserID : null,
  orgId: null,
}

// Registrar en core-authn para que httpRequest lo use automáticamente
if (clerkEnabled) {
  registerTokenProvider(async () => {
    if (getClerkToken) {
      return getClerkToken()
    }
    return null
  })
}

// Llamado desde AuthTokenBridge cuando Clerk está listo
export function setClerkTokenGetter(getter: () => Promise<string | null>) {
  getClerkToken = getter
}

export function setCurrentIdentity(identity: { userId?: string | null; orgId?: string | null }) {
  currentIdentity = {
    userId: identity.userId?.trim() || null,
    orgId: identity.orgId?.trim() || null,
  }
}

type RequestOptions = Omit<RequestInit, 'headers'> & {
  headers?: Record<string, string>
}

async function companionRequest(path: string, options: RequestOptions = {}): Promise<any> {
  const headers: Record<string, string> = { ...options.headers }
  if (currentIdentity.userId) {
    headers['X-User-ID'] = currentIdentity.userId
  }
  if (currentIdentity.orgId) {
    headers['X-Org-ID'] = currentIdentity.orgId
  }

  return httpRequest(path, { ...options, headers })
}

// Companion — Tasks
export const fetchCompanionTasks = () => companionRequest('/companion/v1/tasks')
export const fetchCompanionTask = (id: string) => companionRequest(`/companion/v1/tasks/${id}`)
export const createCompanionTask = (data: unknown) =>
  companionRequest('/companion/v1/tasks', { method: 'POST', body: JSON.stringify(data) })
export const proposeCompanionTask = (id: string, data: Record<string, unknown> = {}) =>
  companionRequest(`/companion/v1/tasks/${id}/propose`, { method: 'POST', body: JSON.stringify(data) })
export const investigateCompanionTask = (id: string, note = '') =>
  companionRequest(`/companion/v1/tasks/${id}/investigate`, {
    method: 'POST',
    body: JSON.stringify({ note }),
  })
export const syncCompanionTaskFromGovernance = (id: string) =>
  companionRequest(`/companion/v1/tasks/${id}/sync`, { method: 'POST' })
export const saveCompanionTaskExecutionPlan = (id: string, data: Record<string, unknown>) =>
  companionRequest(`/companion/v1/tasks/${id}/execution-plan`, { method: 'PUT', body: JSON.stringify(data) })
export const executeCompanionTask = (id: string) =>
  companionRequest(`/companion/v1/tasks/${id}/execute`, { method: 'POST' })
export const retryCompanionTask = (id: string) =>
  companionRequest(`/companion/v1/tasks/${id}/retry`, { method: 'POST' })

// Companion — Connectors
export const fetchCompanionConnectors = () => companionRequest('/companion/v1/connectors')
export const fetchCompanionConnectorCapabilities = () =>
  companionRequest('/companion/v1/connectors/capabilities')
export const fetchCompanionConnectorExecutions = (connectorId: string) =>
  companionRequest(`/companion/v1/connectors/${connectorId}/executions`)

// Companion — Memory
export const fetchCompanionMemory = (scopeType: string, scopeId: string, kind?: string) => {
  const params = new URLSearchParams({ scope_type: scopeType, scope_id: scopeId })
  if (kind) {
    params.set('kind', kind)
  }
  return companionRequest(`/companion/v1/memory?${params.toString()}`)
}
export const saveCompanionMemory = (data: Record<string, unknown>) =>
  companionRequest('/companion/v1/memory', { method: 'PUT', body: JSON.stringify(data) })
export const deleteCompanionMemory = (id: string) =>
  companionRequest(`/companion/v1/memory/${id}`, { method: 'DELETE' })

// Companion — Chat (interfaz conversacional del suscriptor)
export const sendChatMessage = (
  message: string,
  taskId?: string,
  channel = 'console',
  productSurface?: string,
) =>
  companionRequest('/companion/v1/chat', {
    method: 'POST',
    body: JSON.stringify({
      message,
      task_id: taskId || undefined,
      channel,
      product_surface: productSurface || undefined,
    }),
  })

// Companion — Run Traces (replay del runtime)
export const fetchCompanionRunTrace = (runId: string) =>
  companionRequest(`/companion/v1/run-traces/${runId}`)
export const fetchCompanionRunTracesByOrg = (limit = 50) =>
  companionRequest(`/companion/v1/run-traces?limit=${limit}`)
export const fetchCompanionRunTracesByTask = (taskId: string) =>
  companionRequest(`/companion/v1/run-traces?task_id=${encodeURIComponent(taskId)}`)
