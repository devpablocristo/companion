import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider } from '@clerk/react'
import { esMX } from '@clerk/localizations'
import '@devpablocristo/modules-ui-notification-feed/styles.css'
import '@devpablocristo/modules-ui-conversation-inbox/styles.css'
import './index.css'
import App from './App'
import { clerkEnabled, clerkPublishableKey } from './auth'

const app = (
  <StrictMode>
    <App />
  </StrictMode>
)

createRoot(document.getElementById('root')!).render(
  clerkEnabled ? (
    <ClerkProvider publishableKey={clerkPublishableKey} localization={esMX}>
      {app}
    </ClerkProvider>
  ) : (
    app
  ),
)
