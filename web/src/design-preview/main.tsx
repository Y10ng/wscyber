import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.tsx'
import './styles.css'

const container = document.getElementById('root')
if (!container) {
  throw new Error('未找到设计预览根节点 #root')
}

createRoot(container).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
