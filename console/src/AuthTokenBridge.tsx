import { useEffect } from 'react'
import { useAuth, useUser, UserButton } from '@clerk/react'
import { createClerkTokenProvider } from '@devpablocristo/core-authn/providers/clerk'
import { registerTokenProvider } from '@devpablocristo/core-authn/http/fetch'
import { setClerkTokenGetter, setCurrentIdentity } from './api'
import { clerkEnabled, localDevBrowserAccessEnabled } from './auth'
import { getSavedLang, t } from './i18n'

// Registra el token de Clerk en core-authn y en api.ts para que las requests usen Bearer automáticamente.
export function AuthTokenBridge() {
  if (!clerkEnabled) return null
  return <ClerkBridge />
}

function ClerkBridge() {
  const { getToken, isLoaded, isSignedIn } = useAuth()
  const { user } = useUser()

  useEffect(() => {
    if (!isLoaded) return
    const provider = createClerkTokenProvider(getToken)
    registerTokenProvider(provider)
    setClerkTokenGetter(getToken)
    if (!isSignedIn || !user) {
      setCurrentIdentity({ userId: null, orgId: null })
      return
    }
    const email = user.primaryEmailAddress?.emailAddress || user.emailAddresses[0]?.emailAddress || ''
    setCurrentIdentity({
      userId: email || user.id,
      orgId: null,
    })
  }, [getToken, isLoaded, isSignedIn, user])

  if (!isLoaded) return null
  if (!isSignedIn) {
    return (
      <div className="flex items-center gap-2 text-sm text-yellow-400">
        <span>Sign in required</span>
      </div>
    )
  }

  return <UserButton />
}

// Componente para proteger rutas — redirige al sign-in si no hay sesión.
export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  if (!clerkEnabled) {
    if (localDevBrowserAccessEnabled) {
      return <>{children}</>
    }
    const lang = getSavedLang()
    return (
      <div className="flex items-center justify-center min-h-screen bg-gray-900">
        <div className="max-w-md rounded-2xl border border-amber-800 bg-amber-950/40 p-6 text-center">
          <h2 className="text-xl font-bold text-white mb-4">{t(lang, 'authRequiredTitle')}</h2>
          <p className="text-sm leading-6 text-amber-100">{t(lang, 'authRequiredBody')}</p>
        </div>
      </div>
    )
  }
  return <ClerkProtected>{children}</ClerkProtected>
}

function ClerkProtected({ children }: { children: React.ReactNode }) {
  const { isLoaded, isSignedIn } = useAuth()

  if (!isLoaded) {
    return <div className="text-gray-400 p-6">Loading...</div>
  }
  if (!isSignedIn) {
    return (
      <div className="flex items-center justify-center min-h-screen bg-gray-900">
        <div className="text-center">
          <h2 className="text-xl font-bold text-white mb-4">Nexus Workspace</h2>
          <p className="text-gray-400 mb-6">Sign in to continue</p>
          <a href="/" className="bg-blue-600 hover:bg-blue-700 text-white px-6 py-2 rounded">
            Sign In
          </a>
        </div>
      </div>
    )
  }

  return <>{children}</>
}
