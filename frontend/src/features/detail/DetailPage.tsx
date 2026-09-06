import { Link, useNavigate, useParams } from 'react-router-dom'
import { Button, Card } from '../../ds'
import { Icon } from '../../components/Icon'
import { AppDetailContent } from '../database/drawer'

/**
 * Full-page detail route (§4.2): a shareable deep link that reuses the same
 * detail component as the drawer, embedded in a flat panel.
 */
export function DetailPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const appId = Number(id)

  if (Number.isNaN(appId)) return <p>无效 ID</p>

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Button variant="ghost" size="sm" onClick={() => nav(-1)} iconLeft={<Icon name="back" size={15} />}>
          返回
        </Button>
        <Link to="/database" style={{ fontSize: 13 }}>
          去数据库 ↗
        </Link>
      </div>
      <Card padding="18px 20px">
        <AppDetailContent appId={appId} onClose={() => nav('/database')} embedded />
      </Card>
    </div>
  )
}
