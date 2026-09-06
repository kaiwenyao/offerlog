import React, { useEffect, useState } from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
// Self-hosted fonts: serif display for headings (dossier voice) and mono for
// dates/counts — bundled so the Docker deployment works fully offline.
import '@fontsource/noto-serif-sc/600.css'
import '@fontsource/noto-serif-sc/700.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import './styles/global.css'
import { fetchMe, setCsrf } from './lib/api'
import type { Me } from './lib/types'
import { AppLayout } from './app/layout'
import { TodayPage } from './features/today/TodayPage'
import { DatabasePage } from './features/database/DatabasePage'
import { DetailPage } from './features/detail/DetailPage'
const AnalyticsPage = React.lazy(() => import('./features/analytics/AnalyticsPage'))
import { FilesPage } from './features/files/FilesPage'
import { SettingsPage } from './features/settings/SettingsPage'
import { LoginPage } from './features/auth/LoginPage'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 10_000 },
  },
})

function SessionGate({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<'loading' | 'anon' | 'authed'>('loading')
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
    const h = () => setState('anon')
    window.addEventListener('offerlogs:unauthorized', h)
    return () => window.removeEventListener('offerlogs:unauthorized', h)
  }, [])
  if (state === 'loading') {
    return (
      <div style={{ height: '100vh', display: 'grid', placeItems: 'center' }}>
        <span className="spinner" />
      </div>
    )
  }
  if (state === 'anon') return <LoginPage onLoggedIn={(m) => { setMe(m); setState('authed') }} />
  return children
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <SessionGate>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/" element={<TodayPage />} />
              <Route path="/database" element={<DatabasePage />} />
              <Route path="/database/:id" element={<DatabasePage />} />
              <Route path="/apps/:id" element={<DetailPage />} />
              <Route path="/analytics" element={
                <React.Suspense fallback={<div style={{ padding: 40 }}><span className="spinner" /></div>}>
                  <AnalyticsPage />
                </React.Suspense>
              } />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/settings" element={<SettingsPage />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </SessionGate>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(<App />)
