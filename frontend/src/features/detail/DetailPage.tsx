import { useParams, useNavigate, Link } from 'react-router-dom'
import { Spinner } from '../../components/ui'
import { AppDetailContent } from '../database/drawer'

// Full-page detail route (§4.2): shareable deep link. Uses the same detail
// content component as the side-panel drawer, embedded in the page.
export function DetailPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const appId = Number(id)
  if (Number.isNaN(appId)) return <p>无效 ID</p>
  return (
    <div>
      <div className="row mb8" style={{ justifyContent: 'space-between' }}>
        <button className="btn btn-ghost btn-small" onClick={() => nav(-1)}>← 返回</button>
        <Link className="btn btn-ghost btn-small" to="/database">去数据库</Link>
      </div>
      <AppDetailContent appId={appId} onClose={() => nav('/database')} embedded />
    </div>
  )
}
