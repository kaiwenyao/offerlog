import React, { useEffect, useState } from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
// Self-hosted fonts (Industry design system): Barlow for body/UI, Barlow
// Condensed for headings and controls, Noto Sans SC as the Chinese fallback
// — bundled so Docker deployments work fully offline.
import '@fontsource/barlow/400.css'
import '@fontsource/barlow/500.css'
import '@fontsource/barlow/700.css'
import '@fontsource/barlow-condensed/400.css'
import '@fontsource/barlow-condensed/600.css'
import '@fontsource/noto-sans-sc/400.css'
import '@fontsource/noto-sans-sc/500.css'
import '@fontsource/noto-sans-sc/700.css'
import './styles/app.css'
import { fetchMe, setCsrf, api } from './lib/api'
import { setUserZone } from './lib/tz'
import type { Me } from './lib/types'
import { hydrateStatusModel, type ServerStatusModel } from './lib/status'
import { hydrateMilestoneKinds } from './lib/milestones'
import { AppLayout } from './app/layout'
import { PageSpinner } from './components/ui'
import { TodayPage } from './features/today/TodayPage'
import { DatabasePage } from './features/database/DatabasePage'
import { DetailPage } from './features/detail/DetailPage'
import { FilesPage } from './features/files/FilesPage'
import { SettingsPage } from './features/settings/SettingsPage'
import { NotificationsPage } from './features/notifications/Page'
import { CalendarPage } from './features/calendar/CalendarPage'
import { LoginPage } from './features/auth/LoginPage'

const AnalyticsPage = React.lazy(() => import('./features/analytics/AnalyticsPage'))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 10_000 },
  },
})

type SessionState = 'loading' | 'anon' | 'authed'

// 方案 §6.5: pull the server-owned status model BEFORE the app tree renders,
// so no component ever caches the offline fallback tables. A failure (network
// hiccup, older backend) silently keeps the bundled mirror — the server still
// re-validates every mutation.
async function fetchStatusModel(): Promise<ServerStatusModel | null> {
  try {
    return await api.get<ServerStatusModel>('/api/v1/meta/status-model')
  } catch {
    return null
  }
}

function applyStatusModel(m: ServerStatusModel | null): void {
  if (!m) return
  hydrateStatusModel(m)
  // 事件类型清单（迁移 00006）：旧后端不返回时保留 lib/milestones.ts 的兜底表。
  hydrateMilestoneKinds(m.milestone_kinds)
}

export default function App() {
  const [state, setState] = useState<SessionState>('loading')
  const [me, setMe] = useState<Me | null>(null)

  useEffect(() => {
    Promise.all([fetchMe(), fetchStatusModel()])
      .then(([m, model]) => {
        applyStatusModel(model)
        if (m) {
          setUserZone(m.timezone)
          setMe(m)
          setState('authed')
        } else {
          setCsrf(null)
          setState('anon')
        }
      })
      .catch(() => setState('anon'))
  }, [])

  useEffect(() => {
    const onUnauthorized = () => setState('anon')
    window.addEventListener('offerlog:unauthorized', onUnauthorized)
    // After a profile/preferences save, re-read /auth/me so the sidebar and
    // session name reflect the new display name/timezone immediately.
    const onProfile = () =>
      fetchMe().then((m) => {
        if (m) {
          setUserZone(m.timezone)
          setMe(m)
        }
      })
    window.addEventListener('offerlog:profile-changed', onProfile)
    return () => {
      window.removeEventListener('offerlog:unauthorized', onUnauthorized)
      window.removeEventListener('offerlog:profile-changed', onProfile)
    }
  }, [])

  if (state === 'loading') {
    return (
      <div className="app-shell">
        <div style={{ flex: 1, display: 'grid', placeItems: 'center' }}>
          <PageSpinner />
        </div>
      </div>
    )
  }

  if (state === 'anon') {
    return (
      <LoginPage
        onLoggedIn={(m) => {
          // Same hydration guarantee as the initial session: the model may
          // have failed to load before login (anon bootstrap races it).
          void fetchStatusModel().then(applyStatusModel)
          setUserZone(m.timezone)
          setMe(m)
          setState('authed')
        }}
      />
    )
  }

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<AppLayout me={me} />}>
            <Route path="/" element={<TodayPage />} />
            <Route path="/database" element={<DatabasePage />} />
            <Route path="/database/:id" element={<DatabasePage />} />
            <Route path="/apps/:id" element={<DetailPage />} />
            <Route path="/calendar" element={<CalendarPage />} />
            <Route
              path="/analytics"
              element={
                <React.Suspense fallback={<PageSpinner />}>
                  <AnalyticsPage />
                </React.Suspense>
              }
            />
            <Route path="/files" element={<FilesPage />} />
            <Route path="/notifications" element={<NotificationsPage />} />
            <Route path="/settings" element={<SettingsPage me={me} />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(<App />)
