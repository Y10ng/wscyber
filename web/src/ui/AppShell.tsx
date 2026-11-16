import { useState, type ReactNode } from 'react'
import { Button, Tooltip } from 'antd'
import { ApartmentOutlined, MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons'
import './shell.css'

export interface NavigationItem { key: string; label: string; icon: ReactNode; group?: string }
/** 全局外壳：全高侧栏 + 内容区，无独立全局顶栏；标题/页面操作由各页 toolbar 自行承载。 */
export function AppShell({ items, activeKey, onNavigate, footer, children, navigationLabel = '主导航' }: {
  items: NavigationItem[]; activeKey: string; onNavigate: (key: string) => void;
  footer?: ReactNode; children: ReactNode; navigationLabel?: string;
}) {
  const [collapsed, setCollapsed] = useState(false)
  return <div className="ui-shell" data-collapsed={collapsed}>
    <aside className="ui-sidebar" aria-label="应用侧栏">
      <div className="ui-brand">
        <span className="ui-brand-symbol"><ApartmentOutlined /></span>
        {!collapsed && <strong>WSCYBER</strong>}
        <Button type="text" className="ui-collapse" aria-label={collapsed ? '展开左侧菜单' : '收起左侧菜单'} aria-expanded={!collapsed} icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} onClick={() => setCollapsed(!collapsed)} />
      </div>
      <nav aria-label={navigationLabel}>
        {items.map((item, index) => <div className="ui-nav-entry" key={item.key}>{!collapsed && item.group && (index === 0 || items[index - 1]?.group !== item.group) && <div className="ui-nav-group">{item.group}</div>}
          <Tooltip title={collapsed ? item.label : undefined} placement="right">
            <button className="ui-nav-item" aria-label={item.label} aria-current={activeKey === item.key ? 'page' : undefined} onClick={() => onNavigate(item.key)}>{item.icon}{!collapsed && <span>{item.label}</span>}</button>
          </Tooltip>
        </div>)}
      </nav>
      <div className="ui-sidebar-footer">{footer}</div>
    </aside>
    <main className="ui-main"><div className="ui-content">{children}</div></main>
  </div>
}
