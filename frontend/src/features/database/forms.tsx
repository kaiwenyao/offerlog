import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { toInstantInUserZone, toLocalDateTimeInput } from '../../lib/tz'
import type { ActionItem, AppRow, AssessmentRound, Interview, Milestone, Note } from '../../lib/types'
import { ENDED, NEXT_STEP_SUGGESTION, PRIORITIES, REMOTE_OPTIONS, CHANNEL_OPTIONS, SALARY_CURRENCIES, statusMeta } from '../../lib/status'
import {
  isDefaultLabel,
  MILESTONE_GROUP_LABEL,
  MILESTONE_KIND_GROUPS,
  milestoneDefaultLabel,
  milestoneKindsByGroup,
  statusEffectForKind,
} from '../../lib/milestones'
import { Button, Checkbox, Input, Select, Textarea } from '../../ds'
import { ErrorText, Modal, Spinner } from '../../components/ui'
import { buildApplicationPatch, editFieldsFromApp, type ApplicationEditFields } from './edit'
import {
  actionEditFields,
  assessmentEditFields,
  buildActionPatch,
  buildAssessmentPatch,
  buildInterviewPatch,
  buildNotePatch,
  interviewEditFields,
  type ActionEditFields,
  type AssessmentEditFields,
  type InterviewEditFields,
} from './activityEdit'

/** Shared with the progress dialog's inline scheduler — keep one list. */
export const ROUNDS = ['一面', '二面', '三面', '终面', '技术面', 'HR 面', '其他']
export const FORMATS = [
  { value: 'phone', label: '电话' },
  { value: 'video', label: '视频' },
  { value: 'onsite', label: '到面' },
  { value: 'takehome', label: '作业' },
]

export function InterviewForm({
  appId,
  onClose,
  onDone,
}: {
  appId: number
  onClose: () => void
  onDone: () => void
}) {
  const [roundName, setRoundName] = useState('一面')
  const [format, setFormat] = useState('video')
  const [scheduled, setScheduled] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () => {
      // The datetime-local value is naive wall-clock; toInstantInUserZone reads
      // it in the user's configured zone and hands back the zone label to stamp
      // on the round, so the stored interview keeps a truthful timezone tag.
      let scheduledAt: string | null = null
      let zoneLabel = ''
      if (scheduled) {
        const { iso, zone } = toInstantInUserZone(scheduled)
        if (iso == null) {
          setErr('时间格式不正确')
          return Promise.reject(new ApiError('bad_scheduled_at', '时间格式不正确', 400))
        }
        scheduledAt = iso
        zoneLabel = zone
      }
      return api.post(`/api/v1/applications/${appId}/interviews`, {
        round_name: roundName,
        format,
        scheduled_at: scheduledAt,
        timezone: zoneLabel,
      })
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="安排面试"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mut.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select label="轮次" options={ROUNDS} value={roundName} onChange={(e) => setRoundName(e.target.value)} />
        <Select label="形式" options={FORMATS} value={format} onChange={(e) => setFormat(e.target.value)} />
        <Input
          label="时间"
          type="datetime-local"
          value={scheduled}
          onChange={(e) => setScheduled(e.target.value)}
        />
      </div>
    </Modal>
  )
}

export function ActionForm({ app, onClose, onDone }: { app: AppRow; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = useState(app.next_action || NEXT_STEP_SUGGESTION[app.status] || '')
  // next_action_due_at is a date-only YYYY-MM-DD string (never a timestamp).
  const [due, setDue] = useState(app.next_action_due_at ?? '')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: async () => {
      // The standalone action is the source of truth for the unified todo
      // list (§5.3). Creating one also mirrors it onto the application's
      // legacy next_action fields so older surfaces (table column, list
      // view) stay in sync; completing/undoing happens on the action row.
      //
      // due_date is a calendar day: send it as the plain YYYY-MM-DD string
      // (never a browser-local-midnight instant — that shifts the stored day
      // for non-UTC users). The backend stores it in a DATE column.
      const created = await api.post<{ id: number }>(`/api/v1/applications/${app.id}/actions`, {
        title: title.trim(),
        due_date: due || null,
        priority: app.priority,
      })
      // Mirror onto the row (best-effort; the action is authoritative).
      try {
        const fresh = await api.get<AppRow>(`/api/v1/applications/${app.id}`)
        await api.patch(`/api/v1/applications/${app.id}`, {
          version: fresh.version,
          next_action: title.trim() || null,
          // 空串才是「清空」：JSON null 在后端等于「这个字段没传」，会把上一条
          // 待办留下的旧截止日原样留在岗位行上。
          next_action_due_at: due,
        })
      } catch {
        /* the standalone action still exists — surface stays consistent via it */
      }
      return created
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="设置下一步行动"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mut.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={!title.trim() || mut.isPending}>
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="行动内容" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="例如：准备二面" />
        <Input label="截止日期" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
      </div>
    </Modal>
  )
}

export function NoteForm({ appId, onClose, onDone }: { appId: number; onClose: () => void; onDone: () => void }) {
  const [content, setContent] = useState('')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: () => api.post(`/api/v1/applications/${appId}/notes`, { content_md: content }),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal
      title="记录备注"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mut.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" disabled={!content.trim() || mut.isPending} onClick={() => mut.mutate()}>
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <Textarea
        rows={6}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="沟通要点、联系人、后续计划…"
      />
    </Modal>
  )
}

/** 测评类型（方案 §3.2）：作业显示「作业」，不强迫都叫 OA。 */
export const ASSESSMENT_KIND_OPTIONS = [
  { value: 'online_test', label: '在线测试 (OA)' },
  { value: 'take_home', label: 'Take-home 作业' },
  { value: 'other', label: '其他测评' },
]

export const ASSESSMENT_PROGRESS_OPTIONS = [
  { value: 'preparing', label: '准备中' },
  { value: 'completed', label: '已完成 · 等结果' },
]

/**
 * 新增一轮 OA / 作业（方案 §3.2）。收到、计划、截止、完成四种时间各自保存，
 * 互不覆盖；完成时间不详时只标「已完成」，不伪造精确时间。
 */
export function AssessmentForm({
  appId,
  suggestedName,
  onClose,
  onDone,
}: {
  appId: number
  /** 已有轮次数决定默认名称：第一轮 OA，之后 OA 2、OA 3… */
  suggestedName?: string
  onClose: () => void
  onDone: () => void
}) {
  const [kind, setKind] = useState('online_test')
  const [name, setName] = useState(suggestedName || 'OA')
  const [progress, setProgress] = useState('preparing')
  const [invited, setInvited] = useState('')
  const [planned, setPlanned] = useState('')
  const [due, setDue] = useState('')
  const [completed, setCompleted] = useState('')
  const [completedUnknown, setCompletedUnknown] = useState(false)
  const [link, setLink] = useState('')
  const [notes, setNotes] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () => {
      const at = (v: string) => {
        if (!v) return null
        const { iso } = toInstantInUserZone(v)
        if (iso == null) throw new ApiError('bad_time', '时间格式不正确', 400)
        return iso
      }
      return api.post(`/api/v1/applications/${appId}/assessments`, {
        kind,
        name,
        progress,
        result: 'unknown',
        invited_at: at(invited),
        planned_at: at(planned),
        due_at: at(due),
        completed_at: progress === 'completed' ? at(completed) : null,
        completed_unknown: progress === 'completed' ? completedUnknown || !completed : false,
        link,
        notes,
      })
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="新增一轮测评"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mut.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select
          label="类型"
          options={ASSESSMENT_KIND_OPTIONS}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value)
            setName((n) => (n === 'OA' || n.startsWith('OA ') ? (e.target.value === 'take_home' ? '作业' : 'OA') : n))
          }}
        />
        <Input label="名称" value={name} onChange={(e) => setName(e.target.value)} />
        <Select
          label="当前进度"
          options={ASSESSMENT_PROGRESS_OPTIONS}
          value={progress}
          onChange={(e) => setProgress(e.target.value)}
        />
        <Input label="收到邀请" type="datetime-local" value={invited} onChange={(e) => setInvited(e.target.value)} />
        <Input label="计划开做" type="datetime-local" value={planned} onChange={(e) => setPlanned(e.target.value)} />
        <Input
          label="截止时间"
          type="datetime-local"
          value={due}
          onChange={(e) => setDue(e.target.value)}
          hint="完成后不再提醒这个截止时间"
        />
        {progress === 'completed' && (
          <>
            <Input
              label="完成时间"
              type="datetime-local"
              value={completed}
              onChange={(e) => setCompleted(e.target.value)}
              hint="留空则标为「完成时间不详」"
            />
            <Checkbox
              label="完成时间不详（只标「已完成」，不伪造精确时间）"
              checked={completedUnknown}
              onChange={setCompletedUnknown}
            />
          </>
        )}
        <Input label="测试链接" value={link} onChange={(e) => setLink(e.target.value)} />
        <Textarea label="备注" value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} />
      </div>
    </Modal>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * 添加 / 编辑一个时间线事件（迁移 00005 / 00006）。
 *
 * 这是记录进度的**唯一入口**：不是每个岗位都有 OA、初筛或面试——发生了什么由
 * 用户自己选择，时间也可以留空（时间未定）。岗位阶段由后端按事件类型推导，
 * 所以这个表单里没有「目标状态」「子状态」，也没有任何必填的原因。
 *
 * milestone 传入时为编辑模式（PATCH），否则新建（POST）。
 */
export function MilestoneForm({
  appId,
  milestone,
  initialKind,
  onClose,
  onDone,
}: {
  appId: number
  milestone?: Milestone
  /** 从参考流程图点进来时预填的事件类型。 */
  initialKind?: string
  onClose: () => void
  onDone: () => void
}) {
  const [kind, setKind] = useState(milestone?.kind ?? initialKind ?? 'apply')
  const [label, setLabel] = useState(milestone?.label ?? milestoneDefaultLabel(milestone?.kind ?? initialKind ?? 'apply'))
  const [occurred, setOccurred] = useState(
    milestone?.occurred_at ? toLocalDateTimeInput(milestone.occurred_at) : '',
  )
  const [note, setNote] = useState(milestone?.note ?? '')
  const [err, setErr] = useState('')

  const effect = statusEffectForKind(kind)
  const isEnding = effect !== '' && ENDED.has(effect)

  const mut = useMutation({
    mutationFn: () => {
      let occurredAt: string | null = null
      if (occurred) {
        const { iso } = toInstantInUserZone(occurred)
        if (iso == null) throw new ApiError('bad_occurred_at', '时间格式不正确', 400)
        occurredAt = iso
      }
      const body = {
        kind,
        label: label.trim() || milestoneDefaultLabel(kind),
        occurred_at: occurredAt,
        note,
      }
      return milestone
        ? api.patch(`/api/v1/applications/${appId}/milestones/${milestone.id}`, {
            ...body,
            // 新时间留空 = 回到「时间未定」，而不是悄悄保留旧时间。
            clear_occurred_at: occurredAt == null,
          })
        : api.post(`/api/v1/applications/${appId}/milestones`, body)
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title={milestone ? '编辑事件' : '添加事件'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mut.isPending}>
            取消
          </Button>
          <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
          <Select
            label="事件类型"
            options={[]}
            value={kind}
            onChange={(e) => {
              const next = e.target.value
              setKind(next)
              // 名称没改过（还是某个类型的默认名）时跟随类型走；改过则保留用户写的。
              setLabel((l) => (l === '' || isDefaultLabel(l) ? milestoneDefaultLabel(next) : l))
            }}
          >
            {MILESTONE_KIND_GROUPS.map((g) => (
              <optgroup key={g} label={MILESTONE_GROUP_LABEL[g]}>
                {milestoneKindsByGroup(g).map((k) => (
                  <option key={k.key} value={k.key}>
                    {k.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </Select>
          <span style={{ font: 'var(--type-caption)', fontWeight: 400, color: 'var(--text-muted)' }}>
            {effect
              ? `记下这一步，岗位状态会更新为「${statusMeta(effect).label}」`
              : '只记在时间线上，不改变岗位状态'}
          </span>
        </div>
        <Input
          label="名称"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          hint="可自由修改，例如「一面」「背调」「谈薪」"
        />
        <Input
          label="发生时间"
          type="datetime-local"
          value={occurred}
          onChange={(e) => setOccurred(e.target.value)}
          hint="留空则记为「时间未定」；时间线按这个时间自动排序"
        />
        <Textarea
          label="备注"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          rows={2}
          placeholder={isEnding ? '为什么结束？会显示在岗位详情的「原因」里' : '细节、结果、要点…'}
        />
      </div>
    </Modal>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * 编辑岗位的基础信息（创建时只要求公司和岗位，其余这里补齐）。
 *
 * 这是详情页「编辑」入口的表单：公司/岗位/城市/JD 链接填错要能改，薪资、渠道、
 * 截止日期要能补。走 PATCH /applications/:id，携带 version 走乐观锁；薪资传 null
 * 表示清空（后端用 nullable 语义区分「没传」与「清空」）。
 */
export function ApplicationEditForm({
  app,
  onClose,
  onDone,
}: {
  app: AppRow
  onClose: () => void
  onDone: () => void
}) {
  const [fields, setFields] = useState<ApplicationEditFields>(() => editFieldsFromApp(app))
  const [err, setErr] = useState('')

  const set = <K extends keyof ApplicationEditFields>(key: K, value: ApplicationEditFields[K]) =>
    setFields((f) => ({ ...f, [key]: value }))

  const canSave = fields.company_name.trim() !== '' && fields.position.trim() !== ''

  const mut = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.patch(`/api/v1/applications/${app.id}`, body),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="编辑岗位信息"
      onClose={onClose}
      width={560}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={!canSave || mut.isPending}
            onClick={() => {
              setErr('')
              try {
                mut.mutate(buildApplicationPatch(app, fields))
              } catch (e) {
                setErr(e instanceof Error ? e.message : '保存失败')
              }
            }}
          >
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div className="field-grid">
        <Input label="公司 *" value={fields.company_name} onChange={(e) => set('company_name', e.target.value)} />
        <Input label="岗位 *" value={fields.position} onChange={(e) => set('position', e.target.value)} />
        <Input
          label="城市"
          value={fields.location}
          onChange={(e) => set('location', e.target.value)}
          placeholder="例如：上海 / Berlin"
        />
        <div>
          <Select
            label="工作方式"
            value={fields.remote_policy}
            onChange={(e) => set('remote_policy', e.target.value)}
            options={REMOTE_OPTIONS.map((v) => ({ value: v, label: v || '未填' }))}
          />
        </div>
        <div className="full">
          <Input
            label="JD 链接"
            value={fields.job_url}
            onChange={(e) => set('job_url', e.target.value)}
            placeholder="https://…"
          />
        </div>
        <div>
          <Select
            label="渠道"
            value={fields.channel}
            onChange={(e) => set('channel', e.target.value)}
            options={[{ value: '', label: '未填' }, ...CHANNEL_OPTIONS.map((v) => ({ value: v, label: v }))]}
          />
        </div>
        <Input
          label="截止日期"
          type="date"
          value={fields.deadline}
          onChange={(e) => set('deadline', e.target.value)}
          hint="留空表示没有截止日期"
        />
        <div>
          <Select
            label="优先级"
            value={fields.priority}
            onChange={(e) => set('priority', e.target.value)}
            options={Object.entries(PRIORITIES).map(([value, p]) => ({ value, label: p.label }))}
          />
        </div>
        <div>
          <Select
            label="币种"
            value={fields.salary_currency}
            onChange={(e) => set('salary_currency', e.target.value)}
            options={[{ value: '', label: '未填' }, ...SALARY_CURRENCIES.map((v) => ({ value: v, label: v }))]}
          />
        </div>
        <Input
          label="薪资下限"
          inputMode="numeric"
          value={fields.salary_min}
          onChange={(e) => set('salary_min', e.target.value)}
          placeholder="月薪，如 25000"
        />
        <Input
          label="薪资上限"
          inputMode="numeric"
          value={fields.salary_max}
          onChange={(e) => set('salary_max', e.target.value)}
          placeholder="月薪，如 35000"
        />
        <div className="full">
          <Textarea
            label="备注"
            rows={3}
            value={fields.notes}
            onChange={(e) => set('notes', e.target.value)}
            placeholder="岗位细节、条件、联系人…"
          />
        </div>
      </div>
    </Modal>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * 编辑一轮面试（轮次 / 形式 / 时间）。
 *
 * 「面试时间填错一位」以前无解——只能再排一轮，错的那轮永远留在日历、首页
 * 「即将到来的面试」和每天的提醒里。这里接 PATCH /interviews/:id：时间留空即
 * 回到「时间未定」（后端对 scheduled_at 不做合并，空值就是清空）。
 */
export function InterviewEditForm({
  appId,
  interview,
  onClose,
  onDone,
}: {
  appId: number
  interview: Interview
  onClose: () => void
  onDone: () => void
}) {
  const [fields, setFields] = useState<InterviewEditFields>(() => interviewEditFields(interview))
  const [err, setErr] = useState('')

  const set = <K extends keyof InterviewEditFields>(key: K, value: InterviewEditFields[K]) =>
    setFields((f) => ({ ...f, [key]: value }))

  const mut = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.patch(`/api/v1/applications/${appId}/interviews/${interview.id}`, body),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="编辑面试"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={mut.isPending}
            onClick={() => {
              setErr('')
              try {
                mut.mutate(buildInterviewPatch(interview, fields))
              } catch (e) {
                setErr(e instanceof Error ? e.message : '保存失败')
              }
            }}
          >
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select label="轮次" options={ROUNDS} value={fields.round_name} onChange={(e) => set('round_name', e.target.value)} />
        <Select label="形式" options={FORMATS} value={fields.format} onChange={(e) => set('format', e.target.value)} />
        <Input
          label="时间"
          type="datetime-local"
          value={fields.scheduled}
          onChange={(e) => set('scheduled', e.target.value)}
          hint="留空表示时间未定；改期后原来那天的提醒会一起撤销"
        />
      </div>
    </Modal>
  )
}

/**
 * 编辑一轮 OA / 测评（名称 / 收到邀请 / 计划开做 / 截止时间 / 测试链接）。
 *
 * 四种时间里有两格是「看着像装饰、其实是数据源」的：
 *   - planned_at（计划开做）是首页「即将到来的面试 / OA」的时间源与过滤条件、
 *     日历上 kind='assessment' 事件的时刻、本周工序条 OA chip 落在哪一天；
 *   - invited_at（收到邀请）只是事实记录，但它和 planned 一样是「填错一位就
 *     永远改不了」的字段。
 * 截止时间填错同理：改不了的话，日历和「OA 截止」提醒会一直按错的日子响。
 * 三格留空都是**清空**（请求显式带对应的 clear_* 旗标，后端合并语义不会吃掉）。
 */
export function AssessmentEditForm({
  appId,
  assessment,
  onClose,
  onDone,
}: {
  appId: number
  assessment: AssessmentRound
  onClose: () => void
  onDone: () => void
}) {
  const [fields, setFields] = useState<AssessmentEditFields>(() => assessmentEditFields(assessment))
  const [err, setErr] = useState('')

  const set = <K extends keyof AssessmentEditFields>(key: K, value: AssessmentEditFields[K]) =>
    setFields((f) => ({ ...f, [key]: value }))

  const mut = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.patch(`/api/v1/applications/${appId}/assessments/${assessment.id}`, body),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="编辑 OA / 测评"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={mut.isPending}
            onClick={() => {
              setErr('')
              try {
                mut.mutate(buildAssessmentPatch(assessment, fields))
              } catch (e) {
                setErr(e instanceof Error ? e.message : '保存失败')
              }
            }}
          >
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="名称" value={fields.name} onChange={(e) => set('name', e.target.value)} />
        <Input
          label="收到邀请"
          type="datetime-local"
          value={fields.invited}
          onChange={(e) => set('invited', e.target.value)}
          hint="留空表示没有记录（会清掉原来的）"
        />
        <Input
          label="计划开做"
          type="datetime-local"
          value={fields.planned}
          onChange={(e) => set('planned', e.target.value)}
          hint="首页「即将到来的 OA」与日历按这个时间排（留空会清掉）"
        />
        <Input
          label="截止时间"
          type="datetime-local"
          value={fields.due}
          onChange={(e) => set('due', e.target.value)}
          hint="留空表示没有截止时间（会清掉原来的）"
        />
        <Input label="测试链接" value={fields.link} onChange={(e) => set('link', e.target.value)} placeholder="https://…" />
      </div>
    </Modal>
  )
}

/**
 * 编辑一条待办（标题 / 截止日）。
 *
 * 待办写错字以前只能「完成」掉（污染统计）或一天天点「延期」。due_date 是
 * 日历日 YYYY-MM-DD（DATE 列），不做任何时区换算；留空表示没有截止日期。
 */
export function ActionEditForm({
  appId,
  action,
  onClose,
  onDone,
}: {
  appId: number
  action: ActionItem
  onClose: () => void
  onDone: () => void
}) {
  const [fields, setFields] = useState<ActionEditFields>(() => actionEditFields(action))
  const [err, setErr] = useState('')

  const set = <K extends keyof ActionEditFields>(key: K, value: ActionEditFields[K]) =>
    setFields((f) => ({ ...f, [key]: value }))

  const mut = useMutation({
    // 岗位行上的 next_action 只是「最早的未完成待办」的镜像（§5.3），由 PATCH
    // /actions/:id 在同一个事务里重写。这里曾经有一段前端补偿：先 GET 岗位、镜像
    // 恰好等于旧标题才写回新标题。它比后端少知道两件事——哪条待办才是最早的、
    // 以及这次编辑有没有改掉排序——所以只会在后端刚写对之后再把镜像改错。
    mutationFn: (body: Record<string, unknown>) =>
      api.patch<unknown>(`/api/v1/actions/${action.id}`, body),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="编辑待办"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={mut.isPending || !fields.title.trim()}
            onClick={() => {
              setErr('')
              try {
                mut.mutate(buildActionPatch(action, fields))
              } catch (e) {
                setErr(e instanceof Error ? e.message : '保存失败')
              }
            }}
          >
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="待办内容" value={fields.title} onChange={(e) => set('title', e.target.value)} placeholder="例如：准备二面" />
        <Input
          label="截止日期"
          type="date"
          value={fields.due_date}
          onChange={(e) => set('due_date', e.target.value)}
          hint="留空表示没有截止日期"
        />
      </div>
    </Modal>
  )
}

/** 编辑一条备注（只改正文）——以前只能删了重写。 */
export function NoteEditForm({
  appId,
  note,
  onClose,
  onDone,
}: {
  appId: number
  note: Note
  onClose: () => void
  onDone: () => void
}) {
  const [content, setContent] = useState(note.content_md ?? '')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: (body: { content_md: string }) => api.patch(`/api/v1/applications/${appId}/notes/${note.id}`, body),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal
      title="编辑备注"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={mut.isPending}
            onClick={() => {
              setErr('')
              try {
                mut.mutate(buildNotePatch(content))
              } catch (e) {
                setErr(e instanceof Error ? e.message : '保存失败')
              }
            }}
          >
            {mut.isPending ? <Spinner size={14} /> : '保存'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <Textarea rows={6} value={content} onChange={(e) => setContent(e.target.value)} placeholder="沟通要点、联系人、后续计划…" />
    </Modal>
  )
}
