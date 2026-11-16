import { Tabs } from 'antd'
import type { PriceEditSession } from '../modelUsage.ts'
import { ModelUsageWorkspace } from './ModelUsageWorkspace.tsx'
import { ModelPricesView } from './ModelPricesView.tsx'
export function UsageView({ active, tab, callID, session, navigate }: { active: boolean; tab: 'calls' | 'prices'; callID: string | null; session: PriceEditSession; navigate: (path: string) => void }) {
  return <div hidden={!active} className="ws-usage-view">
    <Tabs aria-label="用量工作面" activeKey={tab} onChange={value => navigate(value === 'prices' ? '/usage/prices' : '/usage')} items={[{ key: 'calls', label: '调用与用量' }, { key: 'prices', label: '价格' }]} />
    <div hidden={tab !== 'calls'}><ModelUsageWorkspace active={active && tab === 'calls'} selectedCallID={callID} onSelectCall={id => navigate(id ? `/usage/calls/${id}` : '/usage')} /></div>
    <ModelPricesView active={active && tab === 'prices'} session={session} />
  </div>
}
