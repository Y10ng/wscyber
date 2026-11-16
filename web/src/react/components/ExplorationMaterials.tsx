import { useEffect, useRef, useState } from 'react'
import { Button, Spin } from 'antd'
import { MaterialSelection, MaterialText } from '../../ui/MaterialLibrary.tsx'
import { materialBytes, materialSelectionError, type MaterialDetail, type MaterialItem } from '../../ui/materials.ts'
import { ApiError, StaleSessionError } from '../api.ts'
import { fetchLibraryItem, fetchLibraryItems } from '../libraryApi.ts'
export function ExplorationMaterials({ selected, disabled, onChange }: { selected: MaterialItem[]; disabled: boolean; onChange: (items: MaterialItem[]) => void }) {
  const [items, setItems] = useState<MaterialItem[]>([]), [query, setQuery] = useState(''), [search, setSearch] = useState(''), [cursors, setCursors] = useState(['']), [next, setNext] = useState(''), [refresh, setRefresh] = useState(0), [loading, setLoading] = useState(false), [refreshing, setRefreshing] = useState(false), [error, setError] = useState('')
  const [detail, setDetail] = useState<MaterialDetail | null>(null), [detailLoading, setDetailLoading] = useState(false)
  const onChangeRef = useRef(onChange); onChangeRef.current = onChange
  const readTicket = useRef(0), refreshController = useRef<AbortController | null>(null), after = cursors.at(-1) || ''
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(''); setItems([]); setNext('')
    void fetchLibraryItems({ all: true, kind: 'file', q: search, after }, controller.signal).then(page => { if (!controller.signal.aborted) { setItems(page.items); setNext(page.next_cursor) } }).catch(reason => { if (!controller.signal.aborted && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setError('读取可选资料失败，请刷新。') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [search, after, refresh])
  useEffect(() => () => { readTicket.current++; refreshController.current?.abort() }, [])
  useEffect(() => { refreshController.current?.abort(); setRefreshing(false) }, [disabled])
  const view = async (item: MaterialItem) => {
    const ticket = ++readTicket.current; setDetail(null); setDetailLoading(true)
    try { const value = await fetchLibraryItem(item.id); if (ticket === readTicket.current && value.id === item.id) setDetail(value) }
    catch (reason) { if (ticket === readTicket.current && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setError('所选资料无法读取，可能已删除；请刷新选择。') }
    finally { if (ticket === readTicket.current) setDetailLoading(false) }
  }
  const refreshSelection = async () => {
    refreshController.current?.abort()
    const controller = new AbortController(); refreshController.current = controller; setRefreshing(true); setError('')
    try {
      const wanted = new Set(selected.map(item => item.id)), matched: MaterialItem[] = []; let cursor = ''
      // 明确刷新选择时只读有界元数据；不会借刷新读取正文。
      for (let pages = 0; wanted.size && pages < 80; pages++) {
        const page = await fetchLibraryItems({ all: true, kind: 'file', after: cursor }, controller.signal)
        for (const item of page.items) if (wanted.delete(item.id)) matched.push(item)
        if (!page.next_cursor) break
        cursor = page.next_cursor
      }
      if (controller.signal.aborted) return
      const failure = materialSelectionError(matched)
      if (failure) { setError(failure); return }
      onChangeRef.current(matched); setRefresh(value => value + 1); setCursors([''])
    } catch (reason) { if (!controller.signal.aborted && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setError('未能刷新选择，已保留原选项。') }
    finally { if (!controller.signal.aborted) setRefreshing(false) }
  }
  return <><MaterialSelection items={items} selected={selected} query={query} loading={loading || refreshing} error={error} disabled={disabled || refreshing} onQuery={setQuery} onSearch={() => { if (materialBytes(query) > 128) { setError('搜索最多 128 字节。'); return }; setSearch(query); setCursors(['']) }} nextCursor={next} hasPrevious={cursors.length > 1} onNext={() => setCursors(value => [...value, next])} onPrevious={() => setCursors(value => value.slice(0, -1))} onRefresh={() => void refreshSelection()} onView={item => void view(item)} onToggle={item => { const values = selected.some(value => value.id === item.id) ? selected.filter(value => value.id !== item.id) : [...selected, item]; const failure = materialSelectionError(values); setError(failure); if (!failure) onChange(values) }} />
    {detailLoading && <Spin aria-label="读取选用资料正文" />}{detail && <MaterialText detail={detail} actions={<Button onClick={() => { readTicket.current++; setDetail(null) }}>关闭正文</Button>} />}
  </>
}
