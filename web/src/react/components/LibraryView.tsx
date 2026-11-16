import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Alert, Button, Card, Empty, Space, Spin } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { MaterialBrowser, MaterialEditor, MaterialText } from '../../ui/MaterialLibrary.tsx'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { materialBytes, type MaterialDetail, type MaterialItem } from '../../ui/materials.ts'
import { ApiError, StaleSessionError } from '../api.ts'
import { fetchLibraryFolders, fetchLibraryItem, fetchLibraryItems } from '../libraryApi.ts'
import type { LibrarySession } from '../library.ts'
export function LibraryView({ session }: { session: LibrarySession }) {
  const state = useSyncExternalStore(session.subscribe, session.snapshot)
  const [parentID, setParentID] = useState(''), [query, setQuery] = useState(''), [search, setSearch] = useState(''), [cursors, setCursors] = useState(['']), [refresh, setRefresh] = useState(0)
  const [folders, setFolders] = useState<MaterialItem[]>([]), [items, setItems] = useState<MaterialItem[]>([]), [nextCursor, setNextCursor] = useState(''), [loading, setLoading] = useState(true), [error, setError] = useState('')
  const [detail, setDetail] = useState<MaterialDetail | null>(null), [detailLoading, setDetailLoading] = useState(false), [detailError, setDetailError] = useState(''), [editorOpen, setEditorOpen] = useState(false), [deleteOpen, setDeleteOpen] = useState(false)
  const detailGeneration = useRef(0)
  const after = cursors.at(-1) || '', busy = state.phase === 'writing' || state.phase === 'checking'
  const reload = () => { setRefresh(value => value + 1); setCursors(['']) }
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError(''); setItems([]); setNextCursor('')
    void fetchLibraryFolders(controller.signal).then(async tree => {
      if (controller.signal.aborted) return
      setFolders(tree.items)
      // 使用实时目录而非历史回执恢复位置，避免删除后继续向失效父目录新建。
      if (parentID && !tree.items.some(folder => folder.id === parentID)) { setParentID(''); setCursors(['']); return }
      const page = await fetchLibraryItems({ parentID, q: search, after }, controller.signal)
      if (controller.signal.aborted) return
      setItems(page.items); setNextCursor(page.next_cursor)
    }).catch(reason => { if (!controller.signal.aborted && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setError(reason instanceof Error ? reason.message : '读取资料库失败。') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [parentID, search, after, refresh, state.revision])
  useEffect(() => { detailGeneration.current++; setDetail(null); setDetailError(''); setDetailLoading(false) }, [parentID, search, state.revision])
  useEffect(() => () => { detailGeneration.current++ }, [])
  const open = async (item: MaterialItem) => {
    if (item.kind === 'folder') { setParentID(item.id); setCursors(['']); return }
    await readDetail(item.id)
  }
  const readDetail = async (id: string) => {
    const generation = ++detailGeneration.current
    setDetail(null); setDetailLoading(true); setDetailError('')
    try { const result = await fetchLibraryItem(id); if (generation === detailGeneration.current && result.id === id) setDetail(result) }
    catch (reason) { if (generation === detailGeneration.current && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setDetailError(reason instanceof Error ? reason.message : '读取正文失败。') }
    finally { if (generation === detailGeneration.current) setDetailLoading(false) }
  }
  const newDraft = (kind: 'folder' | 'file') => { session.newDraft(kind, parentID); setEditorOpen(true) }
  return <Card className="ws-panel">
    <div className="material-toolbar" style={{ marginBottom: 12 }}><h2>资料库</h2><Button type="primary" icon={<PlusOutlined />} disabled={Boolean(state.pending || state.draft)} onClick={() => newDraft('file')}>新建资料</Button><Button disabled={Boolean(state.pending || state.draft)} onClick={() => newDraft('folder')}>新建文件夹</Button>{state.draft && <Button onClick={() => setEditorOpen(true)}>继续编辑草稿</Button>}{parentID && <Button onClick={() => void readDetail(parentID)}>当前文件夹</Button>}</div>
    {state.receipt && <Alert style={{ marginBottom: 12 }} type="success" title={`写入回执已确认 · ${state.receipt.deleted ? '删除' : '保存'} · ${new Date(state.receipt.committed_at).toLocaleString('zh-CN')}`} />}
    {!editorOpen && state.error && <Alert style={{ marginBottom: 12 }} type={state.pending ? 'warning' : 'error'} title={state.error} />}
    {state.pending && <div className="material-toolbar" style={{ marginBottom: 12 }} role="status"><span>{busy ? '正在处理原写入' : '写入结果待核对'}</span><Button type="primary" loading={state.phase === 'checking'} disabled={busy} onClick={() => void session.check()}>核对写入回执</Button></div>}
    <MaterialBrowser folders={folders} parentID={parentID} items={items} selectedID={detail?.id} loading={loading} error={error} query={query} onQueryChange={setQuery} onSearch={() => { if (materialBytes(query) > 128) { setError('搜索最多 128 字节。'); return }; setSearch(query); setCursors(['']) }} onFolder={id => { setParentID(id); setCursors(['']) }} onOpen={item => void open(item)} nextCursor={nextCursor} hasPrevious={cursors.length > 1} onNext={() => setCursors(value => [...value, nextCursor])} onPrevious={() => setCursors(value => value.slice(0, -1))} onRefresh={reload}
      detail={<>{detailLoading && <Spin aria-label="读取资料正文" />}{detailError && <Alert type="error" title={detailError} />}{detail && <>
        <div className="material-toolbar"><Button disabled={Boolean(state.pending || state.draft)} onClick={() => { if (session.edit(detail)) setEditorOpen(true) }}>编辑{detail.kind === 'folder' ? '文件夹' : '资料'}</Button><Button danger disabled={Boolean(state.pending || state.draft)} onClick={() => setDeleteOpen(true)}>删除{detail.kind === 'folder' ? '文件夹' : '资料'}</Button><Button onClick={() => { detailGeneration.current++; setDetail(null) }}>关闭查看</Button></div>
        {detail.kind === 'file' ? <MaterialText detail={detail} /> : <section className="material-detail"><h2>{detail.name}</h2>{detail.description && <p>{detail.description}</p>}</section>}
      </>}</>} />
    <TaskModal title={state.draft?.expectedToken ? '编辑资料' : state.draft?.value.kind === 'folder' ? '新建文件夹' : '新建资料'} open={editorOpen} onCancel={() => setEditorOpen(false)} destroyOnHidden footer={<Space wrap><Button disabled={Boolean(state.pending)} onClick={() => { session.discard(); setEditorOpen(false) }}>丢弃草稿</Button><Button onClick={() => setEditorOpen(false)}>暂时关闭</Button><Button type="primary" loading={state.phase === 'writing'} disabled={Boolean(state.pending)} onClick={async () => { await session.save(); if (!session.snapshot().draft) setEditorOpen(false) }}>保存</Button></Space>}>
      {state.error && <Alert style={{ marginBottom: 12 }} type={state.pending ? 'warning' : 'error'} title={state.error} />}
      {state.draft ? <MaterialEditor key={state.draft.id} value={state.draft.value} folders={folders.filter(folder => folder.id !== state.draft?.id)} disabled={Boolean(state.pending)} onChange={value => session.change(value)} onImportError={message => session.error(message)} /> : <Empty description="暂无编辑草稿" />}
    </TaskModal>
    <TaskModal title="确认删除" width={520} open={deleteOpen && Boolean(detail)} onCancel={() => setDeleteOpen(false)} destroyOnHidden footer={<Space><Button onClick={() => setDeleteOpen(false)}>取消</Button><Button danger type="primary" onClick={() => { if (detail) void session.remove(detail); setDeleteOpen(false) }}>确认删除</Button></Space>}>
      <p>删除“{detail?.name}”？已固定到旧任务的内容仍会保留。{detail?.kind === 'folder' ? '仅空文件夹可以删除。' : ''}</p>
    </TaskModal>
  </Card>
}
