import { resolveClerkBrowserConfig } from '@devpablocristo/core-authn/providers/clerk'

const clerkConfig = resolveClerkBrowserConfig()
const localHosts = new Set(['localhost', '127.0.0.1', '::1'])

export const clerkEnabled = clerkConfig.enabled
export const clerkPublishableKey = clerkConfig.publishableKey
export const localDevBrowserAccessEnabled =
  !clerkEnabled &&
  typeof window !== 'undefined' &&
  localHosts.has(window.location.hostname)
export const localDevUserID = 'local-dev-user'
