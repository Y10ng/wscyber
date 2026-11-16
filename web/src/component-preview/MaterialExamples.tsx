import { useState } from 'react'
import { Button, Select } from 'antd'
import { MaterialBrowser, MaterialEditor, MaterialSelection, MaterialText } from '../ui/MaterialLibrary.tsx'
import { materialSelectionError, type MaterialDraft, type MaterialItem } from '../ui/materials.ts'
import { Example } from './examples.tsx'
const folders: MaterialItem[] = [{ id: 'folder', parent_id: '', kind: 'folder', name: '授权测试方法', description: '', format: '', content_sha256: '', byte_size: 0, token: 'sample', created_at: '', updated_at: '' }]
const items: MaterialItem[] = [{ id: 'file', parent_id: '', kind: 'file', classification: 'skill', name: '双身份对照.md', description: '明确授权后对照业务权限', format: 'markdown', byte_size: 48, content_sha256: '本地组件示例', token: 'sample', created_at: '', updated_at: '' }, { id: 'long', parent_id: 'folder', kind: 'file', name: '较长的资料名称用于检查窄屏选择与阅读时的自然换行'.repeat(3) + '.txt', description: '纯文本样例，不会加载外链或执行脚本。', format: 'text', byte_size: 2048, content_sha256: '本地组件示例', token: 'sample', created_at: '', updated_at: '' }]
export function MaterialExamples() {
  const [mode, setMode] = useState('default'), [parent, setParent] = useState(''), [query, setQuery] = useState(''), [selected, setSelected] = useState<MaterialItem[]>([]), [opened, setOpened] = useState<MaterialItem | null>(null), [error, setError] = useState('')
  const [draft, setDraft] = useState<MaterialDraft>({ parent_id: '', kind: 'file', name: '草稿.md', description: '', content: '# 本地组件样例\n\n<script>此处只是文字</script>\n![外链不加载](https://invalid.example/pixel)\n' })
  const list = mode === 'empty' ? [] : items.filter(item => item.name.includes(query))
  const detail = opened && <MaterialText detail={{ ...opened, content: draft.content.repeat(opened.id === 'long' ? 100 : 1) }} />
  return <Example title="资料浏览、选用与本地导入"><Select aria-label="资料组件状态" value={mode} onChange={setMode} options={['default', 'disabled', 'loading', 'empty', 'error'].map(value => ({ value, label: { default: '默认', disabled: '禁用', loading: '加载', empty: '空', error: '错误' }[value] }))} />
    <MaterialBrowser folders={folders} parentID={parent} onFolder={setParent} query={query} onQueryChange={setQuery} onSearch={() => {}} items={list} selectedID={opened?.id} onOpen={setOpened} loading={mode === 'loading'} error={mode === 'error' ? '读取失败，保留当前列表。' : ''} onNext={() => {}} onPrevious={() => {}} onRefresh={() => setMode('default')} detail={detail} actions={<Button onClick={() => setOpened(null)}>关闭正文</Button>} />
    <MaterialSelection items={list} selected={selected} query={query} onQuery={setQuery} onSearch={() => {}} onView={setOpened} onNext={() => {}} onPrevious={() => {}} disabled={mode === 'disabled'} loading={mode === 'loading'} error={error || (mode === 'error' ? '无法读取可选资料。' : '')} onRefresh={() => setMode('default')} onToggle={item => { const next = selected.some(value => value.id === item.id) ? selected.filter(value => value.id !== item.id) : [...selected, item]; const failure = materialSelectionError(next); setError(failure); if (!failure) setSelected(next) }} />
    <MaterialEditor value={draft} folders={folders} disabled={mode === 'disabled'} onChange={setDraft} onImportError={setError} />
  </Example>
}
