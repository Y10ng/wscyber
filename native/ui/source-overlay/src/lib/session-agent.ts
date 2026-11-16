import type { AgentView, SessionRecord } from '../api'

// The hub-side mode flag the local controlled model node announces via its
// hello.capabilities. It marks a restricted execution surface (no console/PTY/
// tmux, no subagent, no eval/continue, no model probe); it is NOT a per-node
// authorization and never widens what the node can do.
export const LOCAL_MODEL_CAPABILITY = 'wscyber.local-model.v1'

// 正式模型节点能力：原生供应商/HTTPS/代理/模型参数与正常 Open/Run/预算。它不是
// 业务模式开关，也不等于旧受控分支；旧受控分支仍是 LOCAL_MODEL_CAPABILITY，两者不能混同。
export const MODEL_CAPABILITY = 'wscyber.model.v1'

// 节点已安装 webapi_http 工具（ToolExtension）时公告的能力标志。根 webapi 只证明
// Hub 有业务 API，不证明所有外接节点都能发 HTTP；前端只选明确提供该工具的节点。
export const WEBAPI_HTTP_CAPABILITY = 'wscyber.webapi-http.v1'

// Chat sessions and live agents use the same Web-scoped node_id.
export function agentMatchesSession(agent: AgentView, session: SessionRecord): boolean {
  return agent.hello?.nodeId === session.session?.nodeId
}

// Whether some connected agent currently backs this session (i.e. a live turn
// can be dispatched to it). False means the bound node is offline.
export function isSessionAgentOnline(session: SessionRecord, agents: AgentView[]): boolean {
  return agents.some((a) => agentMatchesSession(a, session))
}

// Mode detection is always per-node via hello.capabilities — never the merged
// capability union — so another node is not mistaken for the local model node.
export function isLocalModelAgent(agent: AgentView | null | undefined): boolean {
  return !!agent?.hello?.capabilities?.includes(LOCAL_MODEL_CAPABILITY)
}

export function findAgentByNode(agents: AgentView[], nodeID: string | null | undefined): AgentView | undefined {
  if (!nodeID) return undefined
  return agents.find((a) => a.hello?.nodeId === nodeID)
}

// 选「就绪」且明确提供 webapi_http 工具的节点：就绪 = 在 listAgents 返回的在线名册
// agents 中（与 isSessionAgentOnline 一致），在此基础上再按 capability 过滤，不新增
// 探测/配置校验步骤。优先当前选中，单节点自动用；返回 undefined 表示没有可用 HTTP
// 研究节点（普通远端节点不视为可用）。
export function findWebApiHttpNode(agents: AgentView[], selectedNodeID: string | null | undefined): AgentView | undefined {
  const capable = agents.filter((agent) => agent.hello?.capabilities?.includes(WEBAPI_HTTP_CAPABILITY))
  return capable.find((agent) => agent.hello?.nodeId === selectedNodeID) ?? capable[0]
}
