// 文本导出下载：Blob + 临时 anchor + revoke（同 ReportsPage 既有写法）。
// 只负责落文件，不参与文件名规则（见 api.ts webApiVulnExportFilename）。

export function downloadText(text: string, filename: string, format: 'markdown' | 'json'): void {
  const url = URL.createObjectURL(new Blob([text], {
    type: format === 'markdown' ? 'text/markdown;charset=utf-8' : 'application/json',
  }))
  let anchor: HTMLAnchorElement | undefined
  try {
    anchor = document.createElement('a')
    anchor.href = url
    anchor.download = filename
    document.body.appendChild(anchor)
    anchor.click()
  } finally {
    anchor?.remove()
    URL.revokeObjectURL(url)
  }
}
