import type { ReactNode } from 'react'
import './workspace.css'

/** 对象导航和阅读区共用布局；不读取业务、不推断状态，不制造导航入口。 */
export function WorkspaceSplit({ navigation, navigationLabel, children, detailOpen = true, mobileNavigation }: {
  navigation: ReactNode
  navigationLabel: string
  children?: ReactNode
  detailOpen?: boolean
  mobileNavigation?: ReactNode
}) {
  return <div className="workspace-split" data-detail-open={detailOpen} data-mobile-selector={!!mobileNavigation}>
    <aside className="workspace-navigation" aria-label={navigationLabel}>{navigation}</aside>
    {mobileNavigation && <div className="workspace-mobile-navigation">{mobileNavigation}</div>}
    {detailOpen && <div className="workspace-detail">{children}</div>}
  </div>
}
