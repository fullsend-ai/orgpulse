import { createContext, useContext, useState, useCallback, useRef, type ReactNode } from 'react'
import { Portal } from '../Portal'

interface ToastState {
  message: string
  type: 'success' | 'error'
  id: number
}

interface AdminContextValue {
  toast: (message: string, type: 'success' | 'error') => void
  openModal: (content: ReactNode) => void
  closeModal: () => void
  confirm: (message: string, onConfirm: () => void) => void
  navigateTo: (tab: string, params?: Record<string, string>) => void
  consumeNavParams: () => Record<string, string>
}

const AdminContext = createContext<AdminContextValue | null>(null)

export function AdminProvider({
  children,
  onNavigate,
}: {
  children: ReactNode
  onNavigate?: (tab: string, params: Record<string, string>) => void
}) {
  const [toasts, setToasts] = useState<ToastState[]>([])
  const [modal, setModal] = useState<{ content: ReactNode | null }>({ content: null })
  const toastId = useRef(0)

  const toast = useCallback((message: string, type: 'success' | 'error') => {
    const id = ++toastId.current
    setToasts(prev => [...prev, { message, type, id }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }, [])

  const openModal = useCallback((content: ReactNode) => {
    setModal({ content })
  }, [])

  const closeModal = useCallback(() => {
    setModal({ content: null })
  }, [])

  const confirm = useCallback(
    (message: string, onConfirm: () => void) => {
      openModal(
        <div>
          <h3 className="adm-modal-title">Confirm</h3>
          <p className="adm-modal-text">{message}</p>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={() => setModal({ content: null })}>Cancel</button>
            <button className="adm-btn adm-btn--danger" onClick={() => { setModal({ content: null }); onConfirm() }}>Confirm</button>
          </div>
        </div>,
      )
    },
    [openModal],
  )

  const navParamsRef = useRef<Record<string, string>>({})

  const navigateTo = useCallback(
    (tab: string, params?: Record<string, string>) => {
      navParamsRef.current = params || {}
      onNavigate?.(tab, params || {})
    },
    [onNavigate],
  )

  const consumeNavParams = useCallback(() => {
    const p = navParamsRef.current
    navParamsRef.current = {}
    return p
  }, [])

  return (
    <AdminContext.Provider value={{ toast, openModal, closeModal, confirm, navigateTo, consumeNavParams }}>
      {children}
      {modal.content && (
        <Portal>
        <div className="adm-modal-overlay" onMouseDown={e => { if (e.target === e.currentTarget) closeModal() }}>
          <div className="adm-modal">{modal.content}</div>
        </div>
        </Portal>
      )}
      <div className="adm-toast-container">
        {toasts.map(t => (
          <div key={t.id} className={`adm-toast adm-toast--${t.type}`}>{t.message}</div>
        ))}
      </div>
    </AdminContext.Provider>
  )
}

export function useAdmin(): AdminContextValue {
  const ctx = useContext(AdminContext)
  if (!ctx) throw new Error('useAdmin must be used within AdminProvider')
  return ctx
}
