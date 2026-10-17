import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtDate } from '../../lib/api'
import { queryListState } from '../../lib/queryState'
import type { Snippet } from '../../lib/types'
import { Button, Card, Dialog, IconButton, Input, Tag, Textarea } from '../../ds'
import { Icon } from '../../components/Icon'
import { ConfirmDialog, EmptyHint, ErrorText, Num, PageSpinner, Spinner } from '../../components/ui'
import { ALL_CATEGORIES, copyText, filterSnippets, snippetCategories } from './filter'

export const SNIPPETS_KEY = ['snippets']

/** Shared list query — the ⌘K palette reads the same cache. */
export function useSnippets(enabled = true) {
  return useQuery({
    queryKey: SNIPPETS_KEY,
    queryFn: () => api.get<{ items: Snippet[] }>('/api/v1/snippets'),
    staleTime: 30_000,
    enabled,
  })
}

type Draft = { id?: number; title: string; content: string; category: string }

export function SnippetsPage() {
  const qc = useQueryClient()
  const q = useSnippets()
  const [category, setCategory] = useState(ALL_CATEGORIES)
  const [search, setSearch] = useState('')
  const [editing, setEditing] = useState<Draft | null>(null)
  const [pendingDel, setPendingDel] = useState<Snippet | null>(null)
  const [delErr, setDelErr] = useState('')
  // 复制反馈挂在卡片上：「已复制」或「浏览器拒绝了剪贴板」，两秒后消失。
  const [copied, setCopied] = useState<{ id: number; ok: boolean } | null>(null)

  const items = useMemo(() => q.data?.items ?? [], [q.data])
  const categories = useMemo(() => snippetCategories(items), [items])
  // 选中的分类被删空（最后一条移走或删除）后回到「全部」，否则页面停在一个
  // 看不见的筛选上，显示「这个分类下没有片段」。
  const activeCategory = category === ALL_CATEGORIES || categories.some((c) => c.category === category) ? category : ALL_CATEGORIES
  const shown = filterSnippets(items, activeCategory, search)
  const state = queryListState(q)

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/snippets/${id}`),
    onSuccess: () => {
      setDelErr('')
      setPendingDel(null)
      qc.invalidateQueries({ queryKey: SNIPPETS_KEY })
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除失败'),
  })

  const copy = async (s: Snippet) => {
    const ok = await copyText(s.content)
    setCopied({ id: s.id, ok })
    setTimeout(() => setCopied((cur) => (cur?.id === s.id ? null : cur)), 2000)
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <Tag selected={activeCategory === ALL_CATEGORIES} onClick={() => setCategory(ALL_CATEGORIES)}>
          全部 {items.length}
        </Tag>
        {categories.map((c) => (
          <Tag key={c.category || '__none__'} selected={activeCategory === c.category} onClick={() => setCategory(c.category)}>
            {c.category || '未分类'} {c.count}
          </Tag>
        ))}
        <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          <Input
            size="sm"
            fullWidth={false}
            placeholder="搜标题、分类、内容"
            aria-label="搜索片段"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            iconLeft={<Icon name="search" size={14} color="var(--neutral-500)" />}
          />
          <Button
            variant="primary"
            size="sm"
            onClick={() =>
              setEditing({ title: '', content: '', category: activeCategory === ALL_CATEGORIES ? '' : activeCategory })
            }
          >
            <Icon name="plus" size={14} />新建片段
          </Button>
        </span>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
        把投递时反复要填的文字存在这里：实习经历的 role description、自我介绍、Why us……点「复制」即可粘贴；也可以在 ⌘K
        里直接搜片段、回车复制。
      </p>

      {state === 'loading' ? (
        <PageSpinner />
      ) : state === 'error' ? (
        <EmptyHint>
          <ErrorText>片段加载失败，请重试</ErrorText>
          <Button variant="secondary" size="sm" onClick={() => q.refetch()} style={{ marginTop: 10 }}>
            重试
          </Button>
        </EmptyHint>
      ) : shown.length === 0 ? (
        <EmptyHint>
          <p style={{ margin: 0 }}>{items.length === 0 ? '还没有片段' : '没有匹配的片段'}</p>
          {items.length === 0 && (
            <p style={{ margin: 0, fontSize: 13 }}>比如把每段实习经历的描述各存一条，分类填「实习经历」。</p>
          )}
        </EmptyHint>
      ) : (
        <div className="snippet-grid">
          {shown.map((s) => (
            <Card key={s.id} padding="var(--space-4)" style={{ display: 'flex', flexDirection: 'column', gap: 8, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="ellipsis" style={{ fontSize: 14, fontWeight: 600 }} title={s.title}>
                    {s.title}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                    {s.category || '未分类'} · <Num color="var(--text-muted)">{fmtDate(s.updated_at)}</Num>
                  </div>
                </div>
                <IconButton
                  size="sm"
                  variant="ghost"
                  label="编辑"
                  onClick={() => setEditing({ id: s.id, title: s.title, content: s.content, category: s.category })}
                >
                  <Icon name="edit" size={14} />
                </IconButton>
                <IconButton size="sm" variant="ghost" label="删除" onClick={() => setPendingDel(s)}>
                  <Icon name="trash" size={14} />
                </IconButton>
              </div>
              <div className="snippet-body">{s.content || <span style={{ color: 'var(--text-muted)' }}>（空）</span>}</div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 'auto' }}>
                <Button size="sm" variant="secondary" onClick={() => copy(s)} disabled={!s.content}>
                  <Icon name={copied?.id === s.id && copied.ok ? 'check' : 'copy'} size={14} />
                  {copied?.id === s.id && copied.ok ? '已复制' : '复制'}
                </Button>
                {copied?.id === s.id && !copied.ok && <ErrorText>浏览器拒绝了剪贴板，请手动选中复制</ErrorText>}
                <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
                  <Num color="var(--text-muted)">{s.content.length}</Num> 字
                </span>
              </div>
            </Card>
          ))}
        </div>
      )}

      {editing && (
        <SnippetDialog
          draft={editing}
          categories={categories.map((c) => c.category).filter(Boolean)}
          onClose={() => setEditing(null)}
        />
      )}

      {pendingDel && (
        <ConfirmDialog
          title="删除片段"
          pending={del.isPending}
          error={delErr}
          onConfirm={() => del.mutate(pendingDel.id)}
          onClose={() => {
            setPendingDel(null)
            setDelErr('')
          }}
        >
          删除「{pendingDel.title}」后无法恢复。
        </ConfirmDialog>
      )}
    </section>
  )
}

function SnippetDialog({ draft, categories, onClose }: { draft: Draft; categories: string[]; onClose: () => void }) {
  const qc = useQueryClient()
  const [form, setForm] = useState(draft)
  const [err, setErr] = useState('')

  const save = useMutation({
    mutationFn: () => {
      const body = { title: form.title, content: form.content, category: form.category }
      return form.id
        ? api.patch<Snippet>(`/api/v1/snippets/${form.id}`, body)
        : api.post<Snippet>('/api/v1/snippets', body)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: SNIPPETS_KEY })
      onClose()
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.title.trim()) {
      setErr('标题不能为空')
      return
    }
    setErr('')
    save.mutate()
  }

  return (
    <Dialog
      title={form.id ? '编辑片段' : '新建片段'}
      onClose={onClose}
      width={560}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={save.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" type="submit" form="snippet-form" disabled={save.isPending}>
            {save.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      <form id="snippet-form" onSubmit={submit} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Input
          label="标题"
          placeholder="例如：字节跳动 · 推荐算法实习"
          maxLength={100}
          value={form.title}
          onChange={(e) => setForm({ ...form, title: e.target.value })}
        />
        <Input
          label="分类"
          placeholder="例如：实习经历 / 自我介绍 / Why us（可留空）"
          maxLength={40}
          list="snippet-categories"
          value={form.category}
          onChange={(e) => setForm({ ...form, category: e.target.value })}
        />
        <datalist id="snippet-categories">
          {categories.map((c) => (
            <option key={c} value={c} />
          ))}
        </datalist>
        <Textarea
          label="内容"
          rows={10}
          maxLength={10000}
          placeholder="粘贴或撰写要复用的文字，换行和缩进会原样保存"
          value={form.content}
          onChange={(e) => setForm({ ...form, content: e.target.value })}
          hint={`${form.content.length} / 10000`}
        />
        {err && <ErrorText>{err}</ErrorText>}
      </form>
    </Dialog>
  )
}
