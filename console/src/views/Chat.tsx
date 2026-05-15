import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import {
  ConversationInbox,
  type ConversationInboxItem,
} from '@devpablocristo/modules-ui-conversation-inbox'
import { sendChatMessage, fetchCompanionTasks } from '../api'
import { t, relativeTime } from '../i18n'

type ChatMessage = {
  id: string
  author_type: string
  author_id: string
  body: string
  created_at: string
}

type ChatTask = {
  id: string
  title: string
  status: string
  created_at: string
}

type ProductSurface = 'companion' | 'ponti'

const surfaceOptions: { id: ProductSurface; label: string; hint: string }[] = [
  { id: 'companion', label: 'Companion', hint: 'general assist' },
  { id: 'ponti', label: 'Ponti', hint: 'agro insights' },
]

export default function Chat({ lang }: { lang: string }) {
  const [taskId, setTaskId] = useState<string | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [conversations, setConversations] = useState<ChatTask[]>([])
  const [loadingConversations, setLoadingConversations] = useState(true)
  const [surface, setSurface] = useState<ProductSurface>('companion')
  const messagesEndRef = useRef<HTMLDivElement>(null)

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => { scrollToBottom() }, [messages])

  const loadConversations = useCallback(() => {
    setLoadingConversations(true)
    fetchCompanionTasks()
      .then((r: { data?: ChatTask[] }) => {
        const tasks = (r.data || []).filter(
          (task: ChatTask) => task.status !== 'done' && task.status !== 'failed'
        )
        setConversations(tasks)
      })
      .catch(() => {})
      .finally(() => setLoadingConversations(false))
  }, [])

  useEffect(() => { loadConversations() }, [loadConversations])

  const handleSend = async () => {
    const msg = input.trim()
    if (!msg || sending) return

    setSending(true)
    setInput('')

    const optimistic: ChatMessage = {
      id: `temp-${Date.now()}`,
      author_type: 'user',
      author_id: 'subscriber',
      body: msg,
      created_at: new Date().toISOString(),
    }
    setMessages((prev) => [...prev, optimistic])

    try {
      const result = await sendChatMessage(msg, taskId || undefined, 'console', surface)
      setTaskId(result.task.id)
      setMessages(result.messages || [])
      if (!taskId) {
        loadConversations()
      }
    } catch {
      setMessages((prev) => [
        ...prev,
        {
          id: `err-${Date.now()}`,
          author_type: 'system',
          author_id: 'system',
          body: 'Error al enviar el mensaje. Intentalo de nuevo.',
          created_at: new Date().toISOString(),
        },
      ])
    } finally {
      setSending(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const startNewConversation = () => {
    setTaskId(null)
    setMessages([])
  }

  const selectConversation = useCallback(async (id: string) => {
    setTaskId(id)
    try {
      const { fetchCompanionTask } = await import('../api')
      const detail = await fetchCompanionTask(id)
      setMessages(detail.messages || [])
    } catch {
      setMessages([])
    }
  }, [])

  // Mapea las tasks (Companion-side) al shape de ConversationInbox. Cada
  // entry expone una action "Abrir" porque el componente del módulo no
  // tiene afordancia click-en-la-fila completa; usamos el slot `actions`
  // como CTA primaria.
  const inboxItems: ConversationInboxItem[] = useMemo(() => {
    return conversations.map((c) => ({
      id: c.id,
      contactName: c.title || t(lang, 'chat'),
      timestamp: relativeTime(lang, c.created_at),
      unread: taskId !== c.id,
      tone: taskId === c.id ? 'attention' : 'default',
      actions: (
        <button
          type="button"
          onClick={() => selectConversation(c.id)}
          className={`px-3 py-1 text-xs rounded transition-colors ${
            taskId === c.id
              ? 'bg-blue-600 text-white'
              : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
          }`}
        >
          {t(lang, taskId === c.id ? 'chat' : 'newConversation')}
        </button>
      ),
    }))
  }, [conversations, taskId, lang, selectConversation])

  return (
    <div className="flex gap-4 h-[calc(100vh-140px)]">
      <div className="w-64 flex-shrink-0 bg-gray-800 rounded-lg overflow-hidden flex flex-col">
        <div className="p-3 border-b border-gray-700">
          <button
            onClick={startNewConversation}
            className="w-full px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors"
          >
            {t(lang, 'newConversation')}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto">
          {/* Inbox de conversaciones del módulo compartido
              @devpablocristo/modules-ui-conversation-inbox.
              Misma UX que pymes/frontend; antes acá había una lista
              custom (F-07 del audit modular-swinging-hummingbird). */}
          <ConversationInbox
            items={inboxItems}
            loading={loadingConversations}
            loadingMessage={t(lang, 'chat') + '…'}
            emptyMessage={t(lang, 'noConversations')}
          />
        </div>
      </div>

      <div className="flex-1 flex flex-col bg-gray-800 rounded-lg overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-700 flex items-center justify-between gap-3">
          <h2 className="text-white font-medium truncate">
            {taskId
              ? conversations.find((c) => c.id === taskId)?.title || t(lang, 'chat')
              : t(lang, 'newConversation')}
          </h2>
          <div className="flex items-center gap-1 text-xs">
            <span className="text-gray-500 mr-1">surface:</span>
            {surfaceOptions.map((opt) => (
              <button
                key={opt.id}
                onClick={() => setSurface(opt.id)}
                disabled={Boolean(taskId)}
                title={taskId ? 'Cannot change surface mid-conversation; start a new one.' : opt.hint}
                className={`px-2 py-1 rounded transition-colors ${
                  surface === opt.id
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                } ${taskId ? 'opacity-50 cursor-not-allowed' : ''}`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto px-4 py-4 space-y-3">
          {messages.length === 0 && (
            <div className="text-center text-gray-500 mt-12">
              <p className="text-lg mb-2">{t(lang, 'chatGreeting')}</p>
            </div>
          )}
          {messages.map((m) => (
            <div
              key={m.id}
              className={`flex ${m.author_type === 'user' ? 'justify-end' : 'justify-start'}`}
            >
              <div
                className={`max-w-[70%] px-4 py-2 rounded-lg text-sm ${
                  m.author_type === 'user'
                    ? 'bg-blue-600 text-white'
                    : m.author_type === 'system'
                    ? 'bg-red-900/50 text-red-300'
                    : 'bg-gray-700 text-gray-200'
                }`}
              >
                <p className="whitespace-pre-wrap">{m.body}</p>
                <p className="text-xs opacity-50 mt-1">{relativeTime(lang, m.created_at)}</p>
              </div>
            </div>
          ))}
          <div ref={messagesEndRef} />
        </div>

        <div className="px-4 py-3 border-t border-gray-700">
          <div className="flex gap-2">
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t(lang, 'chatGreeting')}
              rows={1}
              className="flex-1 bg-gray-700 text-white rounded-lg px-4 py-2 text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500"
              disabled={sending}
            />
            <button
              onClick={handleSend}
              disabled={sending || !input.trim()}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 disabled:cursor-not-allowed text-white text-sm font-medium rounded-lg transition-colors"
            >
              {sending ? '...' : t(lang, 'send')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
