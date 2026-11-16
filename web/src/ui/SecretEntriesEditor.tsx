import { useId, useRef } from 'react'
import { Button, Input } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { entryName, type SecretEntry, type SecretEntryErrors } from './secretEntries.ts'
import './secretEntries.css'

/** 无API/业务状态；只编辑内存中的条目，不接受已保存值、不解析或重写JSON。 */
export function SecretEntriesEditor({ label, value, onChange, savedNames = [], caseInsensitive = false, json = false, disabled = false, errors = {} }: {
  label: string
  value: SecretEntry[]
  onChange: (value: SecretEntry[]) => void
  savedNames?: string[]
  caseInsensitive?: boolean
  json?: boolean
  disabled?: boolean
  errors?: SecretEntryErrors
}) {
  const prefix = useId(), sequence = useRef(0), root = useRef<HTMLDivElement>(null)
  const saved = new Set(savedNames.map(name => entryName(name, caseInsensitive)))
  const update = (id: string, patch: Partial<SecretEntry>) => onChange(value.map(entry => entry.id === id ? { ...entry, ...patch } : entry))
  const focus = (id: string) => requestAnimationFrame(() => document.getElementById(id)?.focus())
  return <div ref={root} className="secret-entries" role="group" aria-label={label}>
    <div className="secret-entries-heading"><strong>{label}</strong><Button disabled={disabled} icon={<PlusOutlined />} onClick={() => {
      const id = `${prefix}-${++sequence.current}`
      onChange([...value, { id, name: '' }]); focus(`${id}-name`)
    }}>添加条目</Button></div>
    {value.map((entry, index) => {
      const error = errors[entry.id]
      const nameID = `${entry.id}-name`, valueID = `${entry.id}-value`
      return <div className="secret-entry" key={entry.id}>
        <div className="secret-entry-field"><label htmlFor={nameID}>{label}名称 {index + 1}</label>
          <Input id={nameID} value={entry.name} disabled={disabled} autoComplete="off" spellCheck={false} status={error?.name ? 'error' : undefined} aria-invalid={Boolean(error?.name)} aria-describedby={error?.name ? `${nameID}-error` : undefined} onChange={e => update(entry.id, { name: e.target.value })} />
          {error?.name && <span id={`${nameID}-error`} className="secret-entry-error" role="alert">{error.name}</span>}
        </div>
        <div className="secret-entry-field"><label htmlFor={valueID}>{label}{json ? ' JSON 值' : '值'} {index + 1}</label>
          <Input.Password id={valueID} value={entry.value ?? ''} disabled={disabled} autoComplete="new-password" spellCheck={false} placeholder={entry.value === undefined && saved.has(entryName(entry.name, caseInsensitive)) ? '已保存' : json ? 'JSON 值' : '请输入值'} status={error?.value ? 'error' : undefined} aria-invalid={Boolean(error?.value)} aria-describedby={error?.value ? `${valueID}-error` : undefined} onChange={e => update(entry.id, { value: e.target.value })} />
          {error?.value && <span id={`${valueID}-error`} className="secret-entry-error" role="alert">{error.value}</span>}
        </div>
        <Button className="secret-entry-remove" disabled={disabled} aria-label={`删除${label}条目 ${index + 1}`} icon={<DeleteOutlined />} onClick={() => {
          onChange(value.filter(item => item.id !== entry.id))
          const next = value[index + 1] ?? value[index - 1]
          if (next) focus(`${next.id}-name`)
          else requestAnimationFrame(() => root.current?.querySelector<HTMLButtonElement>('button')?.focus())
        }} />
      </div>
    })}
  </div>
}
