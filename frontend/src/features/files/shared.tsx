import { useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { FILE_CATEGORIES } from '../../lib/files'
import type { FileItem } from '../../lib/types'
import { Button, LinkButton } from '../../ds'
import { Modal } from '../../components/ui'
import { FileTile } from '../database/tabs'

function dragHasFiles(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes('Files')
}

/**
 * 窗口级拖拽上传：文件拖进浏览器任意位置松开即回调 onFiles（多文件按顺序逐个
 * 上传）。默认的浏览器「打开文件」行为一律拦掉；拖拽悬停时返回置顶横幅提示。
 */
export function useDropUpload(onFiles: (files: File[]) => void, hint: string) {
  const [dragOver, setDragOver] = useState(false)
  const depth = useRef(0)
  const handler = useRef(onFiles)
  handler.current = onFiles

  useEffect(() => {
    const onDragEnter = (e: DragEvent) => {
      if (!dragHasFiles(e)) return
      e.preventDefault()
      depth.current++
      setDragOver(true)
    }
    const onDragOver = (e: DragEvent) => {
      if (dragHasFiles(e)) e.preventDefault()
    }
    const onDragLeave = (e: DragEvent) => {
      if (!dragHasFiles(e)) return
      depth.current = Math.max(0, depth.current - 1)
      if (depth.current === 0) setDragOver(false)
    }
    const onDrop = (e: DragEvent) => {
      if (!dragHasFiles(e)) return
      e.preventDefault()
      depth.current = 0
      setDragOver(false)
      const files = Array.from(e.dataTransfer?.files ?? [])
      if (files.length > 0) handler.current(files)
    }
    window.addEventListener('dragenter', onDragEnter)
    window.addEventListener('dragover', onDragOver)
    window.addEventListener('dragleave', onDragLeave)
    window.addEventListener('drop', onDrop)
    return () => {
      window.removeEventListener('dragenter', onDragEnter)
      window.removeEventListener('dragover', onDragOver)
      window.removeEventListener('dragleave', onDragLeave)
      window.removeEventListener('drop', onDrop)
    }
  }, [])

  const banner = dragOver ? (
    <div
      style={{
        position: 'fixed',
        top: 14,
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 70,
        background: 'var(--accent)',
        color: '#fff',
        padding: '6px 14px',
        borderRadius: 'var(--radius-control)',
        fontSize: 13,
        boxShadow: 'var(--shadow-pop)',
        pointerEvents: 'none',
      }}
    >
      {hint}
    </div>
  ) : null
  return { dragOver, banner }
}

/** 行内改类别的下拉框：上传选错（把求职信传成简历）时不用重传，直接改。 */
export function CategorySelect({
  value,
  onChange,
  disabled,
}: {
  value: string
  onChange: (c: string) => void
  disabled?: boolean
}) {
  return (
    <select
      aria-label="附件类别"
      title="点击修改类别"
      value={FILE_CATEGORIES.some((c) => c.key === value) ? value : 'other'}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      style={{
        height: 22,
        borderRadius: 'var(--radius-control)',
        border: '1px solid var(--border)',
        background: 'var(--surface)',
        fontSize: 12,
        padding: '0 4px',
        color: 'var(--text-muted)',
      }}
    >
      {FILE_CATEGORIES.map((c) => (
        <option key={c.key} value={c.key}>
          {c.label}
        </option>
      ))}
    </select>
  )
}

/** PATCH /files/:id —— 改类别并同步 application_files 的 purpose/is_resume。 */
export function useUpdateCategory(onError: (msg: string) => void) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, category }: { id: string; category: string }) => api.patch(`/api/v1/files/${id}`, { category }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => onError(e instanceof ApiError ? e.message : '更新类别失败'),
  })
}

/** 浏览器可内联渲染的类型（与后端 previewable 白名单一致）；DOCX 等退回下载提示。 */
function inlineKind(f: FileItem): 'image' | 'embed' | null {
  const ct = f.content_type || ''
  if (/^image\//.test(ct)) return 'image'
  if (ct === 'application/pdf' || ct === 'text/plain') return 'embed'
  return null
}

/** 在线预览弹窗：PDF/图片/TXT 内嵌渲染，DOCX 提示下载查看。 */
export function FileViewerModal({ file, onClose }: { file: FileItem; onClose: () => void }) {
  const kind = file.status === 'ready' ? inlineKind(file) : null
  const src = `/api/v1/files/${file.id}/download?disposition=inline`
  return (
    <Modal
      title={file.name}
      onClose={onClose}
      width={920}
      footer={
        <>
          {file.status === 'ready' && (
            <LinkButton size="sm" href={`/api/v1/files/${file.id}/download`} download>
              下载
            </LinkButton>
          )}
          <Button size="sm" variant="secondary" onClick={onClose}>
            关闭
          </Button>
        </>
      }
    >
      {kind === 'image' ? (
        <img
          src={src}
          alt={file.name}
          style={{ maxWidth: '100%', maxHeight: '68vh', display: 'block', margin: '0 auto', border: '1px solid var(--border)' }}
        />
      ) : kind === 'embed' ? (
        <iframe
          src={src}
          title={file.name}
          style={{ width: '100%', height: '68vh', border: '1px solid var(--border)', background: 'var(--surface)' }}
        />
      ) : (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 10,
            padding: '36px 0',
            color: 'var(--text-muted)',
            fontSize: 13,
          }}
        >
          <FileTile name={file.name} size={40} />
          {file.status !== 'ready' ? (
            <p style={{ margin: 0 }}>文件尚未就绪（{file.status}），稍后再试。</p>
          ) : (
            <p style={{ margin: 0 }}>该格式浏览器无法直接预览，请下载后查看。</p>
          )}
        </div>
      )}
    </Modal>
  )
}
