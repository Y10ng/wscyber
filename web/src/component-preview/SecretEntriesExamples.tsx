import { useState } from 'react'
import { Button, Space, Switch } from 'antd'
import { SecretEntriesEditor } from '../ui/SecretEntriesEditor.tsx'
import { validateSecretEntries, type SecretEntry } from '../ui/secretEntries.ts'
import { Example } from './examples.tsx'

export function SecretEntriesExamples() {
  const [entries, setEntries] = useState<SecretEntry[]>([{ id: 'sample-saved', name: 'X-Saved' }])
  const [disabled, setDisabled] = useState(false), [checked, setChecked] = useState(false)
  const [parameters, setParameters] = useState<SecretEntry[]>([{ id: 'sample-json', name: 'metadata', value: '{"note":"本地长值样例，可使用键盘编辑与删除"}' }])
  return <Example title="秘密键值条目编辑器">
    <Space wrap><Switch aria-label="禁用条目编辑器" checked={disabled} onChange={setDisabled} /><Button onClick={() => setChecked(true)}>校验条目</Button><Button onClick={() => { setChecked(false); setEntries([{ id: 'sample-long', name: `X-${'Long-'.repeat(30)}`, value: '仅本地长值'.repeat(100) }]) }}>长值样例</Button><Button onClick={() => { setChecked(true); setEntries([{ id: 'sample-error', name: '' }]) }}>错误样例</Button></Space>
    <SecretEntriesEditor label="Header" value={entries} onChange={setEntries} savedNames={['X-Saved']} caseInsensitive disabled={disabled} errors={checked ? validateSecretEntries(entries, { savedNames: ['X-Saved'], caseInsensitive: true }) : {}} />
    <SecretEntriesEditor label="参数" value={parameters} onChange={setParameters} json disabled={disabled} errors={checked ? validateSecretEntries(parameters, { json: true }) : {}} />
  </Example>
}
