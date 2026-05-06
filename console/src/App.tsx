import { useState } from 'react'
import Home from './views/Home'
import RunTraces from './views/RunTraces'
import Tasks from './views/Tasks'
import Memory from './views/Memory'
import Connectors from './views/Connectors'
import Chat from './views/Chat'
import { getSavedLang, saveLang, t } from './i18n'
import { getSavedView, saveView } from './storage'
import { AuthTokenBridge, ProtectedRoute } from './AuthTokenBridge'

const tabs = ['home', 'chat', 'tasks', 'memory', 'connectors', 'runTraces']

const validViews = new Set(tabs)

function normalizeView(v: string): string {
  return validViews.has(v) ? v : 'home'
}

export default function App() {
  const [view, setView] = useState(() => normalizeView(getSavedView()))
  const [lang, setLang] = useState(getSavedLang)
  const [taskFocusId, setTaskFocusId] = useState<string | null>(null)

  const changeView = (v: string) => {
    const next = normalizeView(v)
    setView(next)
    saveView(next)
    if (next !== 'tasks') {
      setTaskFocusId(null)
    }
  }

  const changeLang = (l: string) => {
    setLang(l)
    saveLang(l)
  }

  const viewTask = (taskId: string) => {
    setTaskFocusId(taskId)
    setView('tasks')
    saveView('tasks')
  }

  return (
    <div className="min-h-screen">
      <nav className="bg-gray-900 border-b border-gray-800 px-6 py-3">
        <div className="flex items-center gap-8 mb-2">
          <h1 className="text-lg font-bold text-white tracking-tight">Companion Console</h1>
          <div className="ml-auto flex items-center gap-3">
            <AuthTokenBridge />
            {['en', 'es'].map((l) => (
              <button
                key={l}
                onClick={() => changeLang(l)}
                className={`px-2 py-1 rounded text-xs font-medium uppercase transition-colors ${
                  lang === l
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-500 hover:text-white hover:bg-gray-800'
                }`}
              >
                {l}
              </button>
            ))}
          </div>
        </div>
        <div className="flex gap-1">
          {tabs.map((id) => (
            <button
              key={id}
              onClick={() => changeView(id)}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                view === id
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800'
              }`}
            >
              {t(lang, id)}
            </button>
          ))}
        </div>
      </nav>
      <ProtectedRoute>
        <main className="max-w-7xl mx-auto px-6 py-6">
          {view === 'home' && <Home lang={lang} onViewTask={viewTask} />}
          {view === 'chat' && <Chat lang={lang} />}
          {view === 'tasks' && <Tasks lang={lang} focusTaskId={taskFocusId} />}
          {view === 'runTraces' && <RunTraces />}
          {view === 'memory' && <Memory lang={lang} />}
          {view === 'connectors' && <Connectors lang={lang} />}
        </main>
      </ProtectedRoute>
    </div>
  )
}
