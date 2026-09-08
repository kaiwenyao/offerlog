import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'
import type { SavedView } from '../../lib/types'
import { Dialog } from '../../ds/Dialog'
import { Icon, type IconName } from '../../components/Icon'

export interface SearchItem {
  kind: 'application' | 'company' | 'file'
  id: number | string
  label: string
  hint: string
}

const KIND_ICON: Record<SearchItem['kind'], IconName> = {
  application: 'database',
  company: 'folder',
  file: 'file',
}

const KIND_LABEL: Record<SearchItem['kind'], string> = {
  application: '岗位',
  company: '公司',
  file: '文件',
}

interface Cmd {
  key: string
  label: string
  sub: string
  icon: IconName
  run: () => void
}

const ROW_STYLE: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 10,
  padding: '7px 8px',
  borderRadius: 4,
  border: 0,
  background: 'transparent',
  cursor: 'pointer',
  textAlign: 'left',
  font: 'var(--type-ui)',
  color: 'var(--text)',
}

/**
 * ⌘K command palette: mixed 岗位 / 公司 / 文件 results from the cross-entity
 * search endpoint plus local commands (跳转 / 记一个岗位 / 已保存视图). 空输入
 * 时后端返回「最近打开」记录。Esc / backdrop 关闭。
 */
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nav = useNavigate()
  const [q, setQ] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const searchQ = useQuery({
    queryKey: ['search', 'palette', q],
    queryFn: () => api.get<{ items: SearchItem[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&limit=20`),
    enabled: open,
  })
  const viewsQ = useQuery({
    queryKey: ['views'],
    queryFn: () => api.get<{ items: SavedView[] }>('/api/v1/views'),
    enabled: open,
  })

  const items = searchQ.data?.items ?? []

  const navCmds: Cmd[] = useMemo(() => {
    const base: Cmd[] = [
      { key: 'today', label: '今日待办', sub: '首页 · 行动清单', icon: 'today', run: () => nav('/') },
      { key: 'db', label: '求职数据库', sub: '表格 / 看板 / 列表', icon: 'database', run: () => nav('/database') },
      { key: 'cal', label: '面试日历', sub: '周 / 月 / 议程', icon: 'calendar', run: () => nav('/calendar') },
      { key: 'stats', label: '统计分析', sub: '漏斗 · 桑基', icon: 'analytics', run: () => nav('/analytics') },
      { key: 'files', label: '文件库', sub: '私有附件', icon: 'files', run: () => nav('/files') },
      { key: 'settings', label: '设置', sub: '账号 · 字段', icon: 'settings', run: () => nav('/settings') },
      { key: 'new', label: '记一个岗位', sub: '新建申请', icon: 'plus', run: () => nav('/database?new=1') },
    ]
    const saved = (viewsQ.data?.items ?? [])
      .filter((v) => v.id >= 0)
      .slice(0, 6)
      .map<Cmd>((v) => ({
        key: `view-${v.id}`,
        label: `视图：${v.name}`,
        sub: `求职数据库 · ${v.layout === 'table' ? '表格' : v.layout === 'board' ? '看板' : '列表'}`,
        icon: 'rows',
        run: () => nav(`/database?view=${v.id}&layout=${v.layout}`),
      }))
    return [...base, ...saved]
  }, [nav, viewsQ.data])

  useEffect(() => {
    if (open) {
      setQ('')
      // Let the dialog mount before focusing its input.
      const t = setTimeout(() => inputRef.current?.focus(), 10)
      return () => clearTimeout(t)
    }
  }, [open])

  const jump = (it: SearchItem) => {
    if (it.kind === 'application') nav(`/database/${it.id}`)
    else if (it.kind === 'company') nav(`/database?q=${encodeURIComponent(it.label)}`)
    else nav('/files')
    onClose()
  }

  // 中文命令关键字：输入「跳转 / 推进 / 记 / 新建 / 去 …」时只展示命令。
  const trimmed = q.trim().toLowerCase()
  const isCommandQuery = /^(跳转|推进|记|新建|打开|去|命令)/.test(trimmed)
  const filteredCmds = (trimmed === '' || isCommandQuery)
    ? navCmds.filter((c) => trimmed === '' || c.label.toLowerCase().includes(trimmed) || c.sub.toLowerCase().includes(trimmed))
    : []

  return (
    <Dialog open={open} onClose={onClose} title="快速搜索" width={560} description="搜岗位、公司、备注与文件，或直接跳转">
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, borderBottom: '1px solid var(--border)', paddingBottom: 10 }}>
        <Icon name="search" size={16} color="var(--neutral-500)" />
        <input
          ref={inputRef}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="搜岗位、公司、跳转、推进阶段…"
          aria-label="命令面板搜索"
          style={{
            flex: 1,
            border: 0,
            outline: 'none',
            background: 'transparent',
            font: 'var(--type-ui)',
            fontSize: 'var(--text-15)',
            color: 'var(--text)',
          }}
        />
        <span className="kbd" aria-hidden>
          ⌘K
        </span>
      </div>

      <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 2, maxHeight: 380, overflowY: 'auto' }}>
        {items.map((it) => (
          <button key={`${it.kind}-${it.id}`} type="button" className="cmd-row" onClick={() => jump(it)} style={ROW_STYLE}>
            <span
              aria-hidden
              style={{
                width: 22,
                height: 22,
                flex: '0 0 auto',
                display: 'grid',
                placeItems: 'center',
                background: 'var(--surface-thin)',
                border: '1px solid var(--border)',
                color: 'var(--neutral-600)',
              }}
            >
              <Icon name={KIND_ICON[it.kind]} size={13} />
            </span>
            <span className="grow" style={{ minWidth: 0 }}>
              <span className="ellipsis" style={{ display: 'block', fontSize: 14 }}>
                {it.label}
              </span>
              <span className="ellipsis" style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)' }}>
                {KIND_LABEL[it.kind]}
                {it.hint ? ` · ${it.hint}` : ''}
              </span>
            </span>
            <span style={{ fontSize: 10, color: 'var(--neutral-500)', flex: '0 0 auto' }}>↵</span>
          </button>
        ))}

        {items.length > 0 && filteredCmds.length > 0 && (
          <div style={{ margin: '6px 8px', borderTop: '1px dashed var(--border)' }} />
        )}

        {filteredCmds.map((c) => (
          <button
            key={c.key}
            type="button"
            className="cmd-row"
            onClick={() => {
              c.run()
              onClose()
            }}
            style={ROW_STYLE}
          >
            <span
              aria-hidden
              style={{ width: 22, flex: '0 0 auto', display: 'grid', placeItems: 'center', color: 'var(--neutral-600)' }}
            >
              <Icon name={c.icon} size={14} />
            </span>
            <span className="grow" style={{ minWidth: 0 }}>
              <span className="ellipsis" style={{ display: 'block', fontSize: 14 }}>
                {c.label}
              </span>
              <span className="ellipsis" style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)' }}>
                {c.sub}
              </span>
            </span>
            <span style={{ fontSize: 10, color: 'var(--neutral-500)', flex: '0 0 auto' }}>↵</span>
          </button>
        ))}

        {items.length === 0 && filteredCmds.length === 0 && (
          <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: 13, color: 'var(--text-muted)' }}>
            {q ? `没有匹配「${q}」的结果` : '输入关键字搜索，或试试「跳转 / 记一个岗位」'}
          </div>
        )}
      </div>
    </Dialog>
  )
}
