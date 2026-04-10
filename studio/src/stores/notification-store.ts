import { create } from 'zustand'

export type NotificationType = 'task_completed' | 'task_failed' | 'plan_executed' | 'plan_paused'

export interface Notification {
  id: string
  type: NotificationType
  title: string
  message: string
  timestamp: string
  read: boolean
  link?: string
}

interface NotificationState {
  notifications: Notification[]
  addNotification: (notification: Omit<Notification, 'id' | 'timestamp' | 'read'>) => void
  markAsRead: (id: string) => void
  markAllAsRead: () => void
  unreadCount: () => number
}

const STORAGE_KEY = 'anbanwriter_notifications'
const MAX_NOTIFICATIONS = 50

function loadNotifications(): Notification[] {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    return stored ? JSON.parse(stored) : []
  } catch {
    return []
  }
}

function saveNotifications(notifications: Notification[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(notifications.slice(0, MAX_NOTIFICATIONS)))
  } catch {
    // Ignore storage errors
  }
}

export const useNotificationStore = create<NotificationState>((set, get) => ({
  notifications: loadNotifications(),

  addNotification: (input) => {
    const notification: Notification = {
      ...input,
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      timestamp: new Date().toISOString(),
      read: false,
    }
    set((state) => {
      const updated = [notification, ...state.notifications].slice(0, MAX_NOTIFICATIONS)
      saveNotifications(updated)
      return { notifications: updated }
    })
  },

  markAsRead: (id) => {
    set((state) => {
      const updated = state.notifications.map((n) =>
        n.id === id ? { ...n, read: true } : n
      )
      saveNotifications(updated)
      return { notifications: updated }
    })
  },

  markAllAsRead: () => {
    set((state) => {
      const updated = state.notifications.map((n) => ({ ...n, read: true }))
      saveNotifications(updated)
      return { notifications: updated }
    })
  },

  unreadCount: () => {
    return get().notifications.filter((n) => !n.read).length
  },
}))
