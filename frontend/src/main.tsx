import React, { useEffect, useState } from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
// Self-hosted fonts: Inter for body/UI and Noto Sans SC as the Chinese
// fallback — bundled so Docker deployments work fully offline.
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/noto-sans-sc/400.css'
import '@fontsource/noto-sans-sc/500.css'
import '@fontsource/noto-sans-sc/700.css'
import './styles/app.css'
import { fetchMe, setCsrf } from './lib/api'
import type { Me } from './lib/types'
import { AppLayout } from './app/layout'
import { PageSpinner } from './components/ui'
import { TodayPage } from './features/today/TodayPage'
import { DatabasePage } from './features/database/DatabasePage'
import { DetailPage } from './features/detail/DetailPage'
import { FilesPage } from './features/files/FilesPage'
import { SettingsPage } from './features/settings/SettingsPage'
import { LoginPage } from './features/auth/LoginPage'

const AnalyticsPage = React.lazy(() => import('./features/analytics/AnalyticsPage'))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 10_000 },
  },
})

type SessionState = 'loading' | 'anon' | 'authed'

export default function App() {
  const [state, setState] = useState<SessionState>('loading')
  const [me, setMe] = useState<Me | null>(null)

  useEffect(() => {
    fetchMe()
      .then((m) => {
        if (m) {
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
    return () => window.removeEventListener('offerlog:unauthorized', onUnauthorized)
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
            <Route
              path="/analytics"
              element={
                <React.Suspense fallback={<PageSpinner />}>
                  <AnalyticsPage />
                </React.Suspense>
              }
            />
            <Route path="/files" element={<FilesPage />} />
            <Route path="/settings" element={<SettingsPage me={me} />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(<App />)
