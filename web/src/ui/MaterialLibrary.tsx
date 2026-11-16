import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { Alert, Button, Checkbox, Empty, Input, Select, Space, Spin, Tree, Typography } from 'antd'
import { FileTextOutlined, FolderOutlined, UploadOutlined } from '@ant-design/icons'
import { CopyButton } from './SurfaceControls.tsx'
import { importMaterialText, materialBytes, materialPath, type MaterialDetail, type MaterialDraft, type MaterialItem } from './materials.ts'
import './materials.css'

interface FolderNode { key: string; title: string; children: FolderNode[] }
function folderNodes(folders: MaterialItem[], parent = '', seen = new Set<string>()): FolderNode[] {
  return folders.filter(item => item.parent_id === parent && !seen.has(item.id)).map(item => ({ key: item.id, title: item.name, children: folderNodes(folders, item.id, new Set([...seen, item.id])) }))
}
export function MaterialFolders({ folders, value, onChange, disabled = false }: { folders: MaterialItem[]; value: string; onChange: (id: string) => void; disabled?: boolean }) {
  return <nav aria-label="资料文件夹" className="material-folders"><Tree blockNode defaultExpandAll disabled={disabled} selectedKeys={[value]} onSelect={keys => { if (keys.length) onChange(String(keys[0])) }} treeData={[{ key: '', title: '全部根目录', children: folderNodes(folders) }]} /></nav>
}
interface BrowserProps {
  folders: MaterialItem[]; parentID: string; items: MaterialItem[]; selectedID?: string; loading?: boolean; error?: string; nextCursor?: string; hasPrevious?: boolean
  query: string; onQueryChange: (value: string) => void; onSearch: () => void; onFolder: (id: string) => void; onOpen: (item: MaterialItem) => void; onNext: () => void; onPrevious: () => void; onRefresh: () => void
  actions?: ReactNode; detail?: ReactNode
}
export function MaterialBrowser(props: BrowserProps) {
  return <section className="material-browser" aria-label="资料库浏览">
    <MaterialFolders folders={props.folders} value={props.parentID} onChange={props.onFolder} />
    <div className="material-list-pane">
      <div className="material-toolbar"><Input.Search aria-label="搜索资料名称或说明" placeholder="搜索名称或说明" value={props.query} onChange={event => props.onQueryChange(event.target.value)} onSearch={props.onSearch} allowClear /><Button onClick={props.onRefresh} loading={props.loading}>刷新</Button>{props.actions}</div>
      {props.error && <Alert type="error" showIcon title={props.error} />}
      <Spin spinning={Boolean(props.loading)}><div className="material-list" aria-label="当前目录资料">
        {!props.loading && !props.items.length && !props.error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="此目录暂无资料" />}
        {props.items.map(item => <button type="button" className="material-row" aria-pressed={props.selectedID === item.id} key={item.id} onClick={() => props.onOpen(item)}>
          {item.kind === 'folder' ? <FolderOutlined /> : <FileTextOutlined />}<span className="material-row-text"><strong>{item.name}</strong>{item.description && <span>{item.description}</span>}</span><span>{item.kind === 'folder' ? '文件夹' : `${item.classification === 'skill' ? 'Skill' : '普通资料'} · ${item.byte_size.toLocaleString('zh-CN')} 字节`}</span>
        </button>)}
      </div></Spin>
      <div className="material-pagination"><Button disabled={!props.hasPrevious || props.loading} onClick={props.onPrevious}>上一页</Button><Button disabled={!props.nextCursor || props.loading} onClick={props.onNext}>下一页</Button></div>
      {props.detail}
    </div>
  </section>
}
export function MaterialText({ detail, actions }: { detail: Pick<MaterialDetail, 'name' | 'description' | 'content' | 'byte_size' | 'content_sha256'>; actions?: ReactNode }) {
  return <section className="material-detail" aria-label="资料正文"><div className="material-toolbar"><h2>{detail.name}</h2><span>{detail.byte_size.toLocaleString('zh-CN')} 字节</span>{actions}<CopyButton text={detail.content} label="复制资料正文" /></div>{detail.description && <p>{detail.description}</p>}<pre tabIndex={0} className="material-text">{detail.content}</pre><details><summary>内容校验</summary><code className="material-hash">SHA256 {detail.content_sha256}</code></details></section>
}
export function MaterialEditor({ value, folders, disabled, onChange, onImportError }: { value: MaterialDraft; folders: MaterialItem[]; disabled?: boolean; onChange: (draft: MaterialDraft) => void; onImportError: (error: string) => void }) {
  const id = useId(), fileRef = useRef<HTMLInputElement>(null), generation = useRef(0), latest = useRef(value)
  latest.current = value
  const [importing, setImporting] = useState(false)
  useEffect(() => () => { generation.current++ }, [])
  useEffect(() => { generation.current++; setImporting(false) }, [disabled])
  const change = (next: MaterialDraft) => { generation.current++; setImporting(false); onChange(next) }
  return <div className="material-editor">
    <label htmlFor={`${id}-name`}>名称</label><Input id={`${id}-name`} autoFocus value={value.name} disabled={disabled} onChange={event => change({ ...value, name: event.target.value })} />
    <label htmlFor={`${id}-parent`}>所属目录</label><Select id={`${id}-parent`} value={value.parent_id} disabled={disabled} showSearch optionFilterProp="label" onChange={parent_id => change({ ...value, parent_id })} options={[{ value: '', label: '/' }, ...folders.map(folder => ({ value: folder.id, label: materialPath(folder, folders) }))]} />
    {value.kind === 'file' && <><label htmlFor={`${id}-classification`}>分类</label><Select id={`${id}-classification`} value={value.classification || 'material'} disabled={disabled} onChange={classification => change({ ...value, classification })} options={[{ value: 'material', label: '普通资料（任务显式选用）' }, { value: 'skill', label: 'Skill（Agent按需发现与加载）' }]} /></>}
    <label htmlFor={`${id}-description`}>{value.classification === 'skill' ? '适用说明（必填）' : '说明'}</label><Input.TextArea id={`${id}-description`} rows={2} value={value.description} disabled={disabled} onChange={event => change({ ...value, description: event.target.value })} />
    {value.kind === 'file' && <><div className="material-toolbar"><label htmlFor={`${id}-content`}>正文</label><span>{materialBytes(value.content).toLocaleString('zh-CN')} / 32,768 字节</span><Button icon={<UploadOutlined />} disabled={disabled} loading={importing} onClick={() => fileRef.current?.click()}>导入 .md / .txt</Button></div>
      <input ref={fileRef} className="sr-only" type="file" tabIndex={-1} accept=".md,.txt" disabled={disabled} aria-label="导入本地文本资料" onChange={async event => {
        const file = event.target.files?.[0]; event.target.value = ''; if (!file) return
        const ticket = ++generation.current; setImporting(true)
        try { const result = await importMaterialText(file); if (ticket === generation.current) { onChange({ ...latest.current, ...result }); onImportError('') } }
        catch (error) { if (ticket === generation.current) onImportError(error instanceof Error ? error.message : '读取文件失败。') }
        finally { if (ticket === generation.current) setImporting(false) }
      }} />
      <Input.TextArea id={`${id}-content`} rows={12} value={value.content} disabled={disabled || importing} onChange={event => change({ ...value, content: event.target.value })} /></>}
  </div>
}
export function MaterialSelection({ items, selected, loading, error, query, nextCursor, hasPrevious, disabled, onQuery, onSearch, onNext, onPrevious, onToggle, onView, onRefresh }: {
  items: MaterialItem[]; selected: MaterialItem[]; loading?: boolean; error?: string; query: string; nextCursor?: string; hasPrevious?: boolean; disabled?: boolean
  onQuery: (value: string) => void; onSearch: () => void; onNext: () => void; onPrevious: () => void; onToggle: (item: MaterialItem) => void; onView: (item: MaterialItem) => void; onRefresh: () => void
}) {
  return <section className="material-selection" aria-label="任务资料选择"><div className="material-toolbar"><Typography.Text strong>选用资料</Typography.Text><span>{selected.length} / 16</span><Button disabled={disabled} onClick={onRefresh} loading={loading}>刷新选择</Button></div>
    {selected.length > 0 && <ul className="material-selected" aria-label="已选资料">{selected.map(item => <li key={item.id}><span>{item.name}</span><Button size="small" disabled={disabled} onClick={() => onToggle(item)} aria-label={`移除 ${item.name}`}>移除</Button></li>)}</ul>}
    <Input.Search aria-label="搜索可选资料" placeholder="搜索名称或说明" value={query} disabled={disabled} onChange={event => onQuery(event.target.value)} onSearch={onSearch} />
    {error && <Alert type="error" title={error} showIcon />}
    <Spin spinning={Boolean(loading)}><div className="material-picker-list">{!items.length && !loading && !error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无可选资料" />}{items.map(item => <div className="material-row" key={item.id}><Checkbox disabled={disabled} checked={selected.some(value => value.id === item.id)} onChange={() => onToggle(item)} aria-label={`选用 ${item.name}`} /><span className="material-row-text"><strong>{item.name}</strong>{item.description && <span>{item.description}</span>}</span><span>{item.byte_size.toLocaleString('zh-CN')} 字节</span><Button onClick={() => onView(item)}>查看</Button></div>)}</div></Spin>
    <Space wrap><Button disabled={disabled || loading || !hasPrevious} onClick={onPrevious}>上一页</Button><Button disabled={disabled || loading || !nextCursor} onClick={onNext}>下一页</Button></Space>
  </section>
}
