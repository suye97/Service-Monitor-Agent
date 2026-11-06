import './style.css'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { api, isDemo, type Alert, type AnalysisResult, type AnalysisTurn, type AppState, type Metric, type Project, type ServerInput, type Settings } from './backend'

type Page = 'overview' | 'alerts' | 'servers' | 'projects' | 'analysis' | 'settings'
type Modal = 'server' | 'project' | 'delete-server' | 'delete-project' | null
const app = document.querySelector<HTMLDivElement>('#app')!
const icons: Record<string, string> = {
  pulse: '<path d="M3 12h4l3-7 4 14 3-7h4"/>', grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9ZM10 21h4"/>', server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M11 6.5h7M11 17.5h7"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10H3Z"/>', sparkle: '<path d="m12 3 1.7 5.3L19 10l-5.3 1.7L12 17l-1.7-5.3L5 10l5.3-1.7ZM19 17l.7 2.3L22 20l-2.3.7L19 23l-.7-2.3L16 20l2.3-.7Z"/>',
  settings: '<path d="M12 3v2m0 14v2M3 12h2m14 0h2M5.6 5.6 7 7m10 10 1.4 1.4M18.4 5.6 17 7M7 17l-1.4 1.4"/><circle cx="12" cy="12" r="4"/>', arrow: '<path d="M5 12h14m-6-6 6 6-6 6"/>', plus: '<path d="M12 5v14M5 12h14"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.4 6.6M20 4v7h-7"/>', lock: '<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>', logout: '<path d="M10 17l5-5-5-5m5 5H3m9-8h6a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6"/>',
  chevron: '<path d="m9 18 6-6-6-6"/>', check: '<path d="m4 12 5 5L20 6"/>', warning: '<path d="M12 3 2 20h20L12 3Zm0 6v4m0 4h.01"/>', clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  cpu: '<rect x="5" y="5" width="14" height="14" rx="2"/><path d="M9 9h6v6H9zM9 2v3m6-3v3M9 19v3m6-3v3M2 9h3m-3 6h3m14-6h3m-3 6h3"/>', memory: '<rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 7V4m5 3V4m5 3V4M7 17v3m5-3v3m5-3v3"/>', disk: '<ellipse cx="12" cy="6" rx="9" ry="3"/><path d="M3 6v12c0 1.7 4 3 9 3s9-1.3 9-3V6M3 12c0 1.7 4 3 9 3s9-1.3 9-3"/>',
  send: '<path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/>', download: '<path d="M12 3v12m-4-4 4 4 4-4M4 17v4h16v-4"/>', trash: '<path d="M4 7h16M10 3h4m-8 4 1 14h10l1-14M10 11v6m4-6v6"/>', edit: '<path d="m4 17 13-13 3 3L7 20H4Zm11-11 3 3"/>',
  link: '<path d="M10 13a5 5 0 0 0 7 .5l3-3a5 5 0 0 0-7-7l-1.7 1.7M14 11a5 5 0 0 0-7-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7"/>', shield: '<path d="M12 2 4 5v6c0 5 3.5 8.5 8 11 4.5-2.5 8-6 8-11V5Z"/><path d="m9 12 2 2 4-4"/>', close: '<path d="M5 5l14 14M19 5 5 19"/>', menu: '<path d="M4 6h16M4 12h16M4 18h16"/>', dots: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
}
const icon = (name: string, size = 20) => `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.pulse}</svg>`
const esc = (text: unknown) => String(text ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!)
const uid = () => globalThis.crypto?.randomUUID?.() || `id-${Date.now()}-${Math.random().toString(36).slice(2)}`
const modelBaseURLs: Record<string, string> = { deepseek: 'https://api.deepseek.com', openai: 'https://api.openai.com/v1', custom: '' }
const fmt = (date: string) => { const time = new Date(date); return Number.isNaN(time.getTime()) ? '时间未知' : time.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
const percent = (n: number) => Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 0
const value = (n: number) => `${Math.round(percent(n))}%`
const labelMetric = (name: string) => ({ cpu: 'CPU', memory: '内存', disk: '磁盘' })[name.toLowerCase()] || name
const metricFor = (serverId: string) => state?.metrics.find(metric => metric.serverId === serverId)
const serverFor = (serverId: string) => state?.servers.find(server => server.id === serverId)
const activeAlerts = () => state?.alerts.filter(alert => alert.active) || []
const statusOf = (metric?: Metric) => !metric || metric.error ? 'offline' : [metric.cpu, metric.memory, ...metric.disks.map(d => d.percent)].some(n => n > (state?.settings.threshold || 80)) ? 'warning' : 'healthy'
const statusLabel = (status: string) => ({ healthy: '运行正常', warning: '需要关注', offline: '连接异常' })[status] || status
const meter = (n: number, compact = false) => `<div class="meter ${compact ? 'compact' : ''} ${n > (state?.settings.threshold || 80) ? 'hot' : ''}"><div style="width:${percent(n)}%"></div></div>`
const button = (text: string, action: string, cls = 'btn-ghost', other = '') => `<button type="button" class="btn ${cls}" data-action="${action}" ${other}>${text}</button>`
const field = (title: string, name: string, val: string | number = '', opts: { type?: string; placeholder?: string; required?: boolean; hint?: string; min?: number; max?: number } = {}) => `<label class="field"><span>${esc(title)}</span><input name="${esc(name)}" type="${opts.type || 'text'}" value="${esc(val)}" placeholder="${esc(opts.placeholder || '')}" ${opts.required ? 'required' : ''} ${opts.min === undefined ? '' : `min="${opts.min}"`} ${opts.max === undefined ? '' : `max="${opts.max}"`} autocomplete="off" /><small>${esc(opts.hint || '')}</small></label>`
const blank = (title: string, desc: string, action = '', actionText = '') => `<div class="empty">${icon('folder', 34)}<h3>${esc(title)}</h3><p>${esc(desc)}</p>${action ? button(`${icon('plus', 16)} ${esc(actionText)}`, action, 'btn-primary') : ''}</div>`
function metricIssues(metric?: Metric) {
  if (!metric || (!metric.error && !metric.notificationError)) return ''
  return `<div class="metric-issues">${metric.error ? `<div><strong>采集失败</strong><span>${esc(metric.error)}</span></div>` : ''}${metric.notificationError ? `<div><strong>通知失败</strong><span>${esc(metric.notificationError)}</span></div>` : ''}</div>`
}
function conversationHistory(): AnalysisTurn[] {
  return (state?.analyses || []).filter(result => result.projectId === projectId).slice(-6).map(result => ({ question: result.question, answer: result.answer }))
}
function restoreMessages() {
  messages = (state?.analyses || []).filter(result => result.projectId === projectId).flatMap(result => [
    { role: 'user' as const, text: result.question },
    { role: 'assistant' as const, text: result.answer, result },
  ])
}

let state: AppState | null = null
let page: Page = 'overview'
let modal: Modal = null
let selectedId = ''
let detailId = ''
let projectId = ''
let messages: { role: 'user' | 'assistant'; result?: AnalysisResult; text: string }[] = []
let busy = false
let polling = false
let pollVersion = 0
let toastTimer: number | undefined
let sideOpen = false
let initialError = ''
let pollError = ''
let modelOptions: string[] = []
let connectingId = ''

function toast(message: string, error = false) {
  const node = document.querySelector<HTMLElement>('#toast')
  if (!node) return
  node.innerHTML = `${icon(error ? 'warning' : 'check', 17)}<span>${esc(message)}</span>`
  node.className = `toast visible ${error ? 'error' : ''}`
  clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => { node.className = 'toast' }, 4200)
}
function errorText(error: unknown) { return error instanceof Error ? error.message : String(error) }
async function run(task: () => Promise<void>, success?: string) {
  if (busy) return
  busy = true; pollVersion++; document.body.classList.add('is-busy')
  try { await task(); if (success) toast(success) }
  catch (error) { toast(errorText(error), true) }
  finally { busy = false; document.body.classList.remove('is-busy') }
}
async function reload() { state = await api.GetState(); render() }
function navItem(id: Page, title: string, glyph: string, badge = 0) { return `<button type="button" class="nav-item ${page === id ? 'selected' : ''}" data-page="${id}">${icon(glyph, 19)}<span>${title}</span>${badge ? `<b>${badge}</b>` : ''}</button>` }
function layout(body: string) {
  const titles: Record<Page, string> = { overview: '总览', alerts: '告警中心', servers: '服务器管理', projects: '项目配置', analysis: 'AI 日志分析', settings: '通知与设置' }
  return `<div class="shell"><aside class="sidebar ${sideOpen ? 'open' : ''}"><div class="brand"><div class="brand-icon">${icon('pulse', 24)}</div><div><strong>脉点</strong><small>SERVICE PULSE</small></div></div>
    <div class="workspace"><span class="workspace-mark">${icon('grid', 18)}</span><div><b>监控工作台</b><small>所有服务，一目了然</small></div>${icon('chevron', 15)}</div>
    <div class="nav-label">工作空间</div><nav>${navItem('overview', '总览', 'grid')}${navItem('alerts', '告警中心', 'bell', activeAlerts().length)}${navItem('servers', '服务器管理', 'server')}${navItem('projects', '项目配置', 'folder')}</nav>
    <div class="nav-label nav-label-second">智能工具</div><nav>${navItem('analysis', 'AI 日志分析', 'sparkle')}${navItem('settings', '通知与设置', 'settings')}</nav>
    <div class="sidebar-bottom"><div class="side-tip"><span>${icon('shield', 19)}</span><strong>凭据已加密保护</strong><small>服务器密码不会在界面中显示</small></div><button class="sidebar-lock" data-action="lock" type="button">${icon('lock', 17)} 锁定工作台 ${icon('arrow', 16)}</button></div></aside>
    <div class="mobile-scrim ${sideOpen ? 'show' : ''}" data-action="close-side"></div><main class="main"><header class="topbar"><button class="icon-btn mobile-menu" aria-label="打开菜单" data-action="toggle-side">${icon('menu', 21)}</button><div class="breadcrumb">工作空间 <span>/</span> <b>${titles[page]}</b></div><div class="top-actions">${isDemo ? '<span class="demo-pill">界面演示</span>' : ''}<span class="top-date">${new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })}</span><button class="icon-btn top-bell" data-page="alerts" aria-label="查看告警">${icon('bell', 20)}${activeAlerts().length ? '<i></i>' : ''}</button><div class="avatar">管</div></div></header><div class="content">${body}</div></main></div>`
}
function heading(kicker: string, title: string, desc: string, tools = '') { return `<div class="page-heading"><div><div class="eyebrow">${esc(kicker)}</div><h1>${esc(title)}</h1><p>${esc(desc)}</p></div><div class="heading-tools">${tools}</div></div>` }
function statCard(glyph: string, title: string, figure: string, note: string, tone = '') { return `<div class="stat-card"><div class="stat-top"><span class="stat-icon ${tone}">${icon(glyph, 21)}</span><span class="stat-spark">${icon('arrow', 16)}</span></div><div class="stat-title">${title}</div><div class="stat-value">${figure}</div><div class="stat-note">${note}</div></div>` }
function overviewServerRow(server: ServerInput) {
  const metric = metricFor(server.id), status = statusOf(metric), available = metric && !metric.error
  const disk = metric?.disks?.length ? Math.max(...metric.disks.map(d => d.percent)) : 0
  return `<div class="server-entry"><div class="server-row"><div class="server-identity"><span class="server-symbol">${icon('server', 21)}</span><div><strong>${esc(server.name)}</strong><small>${esc(server.host)}</small></div></div><span class="status ${status}"><i></i>${statusLabel(status)}</span><div class="row-metrics"><div><span>CPU</span><b>${available ? value(metric.cpu) : '—'}</b>${meter(metric?.cpu || 0, true)}</div><div><span>内存</span><b>${available ? value(metric.memory) : '—'}</b>${meter(metric?.memory || 0, true)}</div><div><span>磁盘</span><b>${available ? value(disk) : '—'}</b>${meter(disk, true)}</div></div><button class="row-arrow" aria-label="查看 ${esc(server.name)}" data-action="view-server" data-id="${esc(server.id)}">${icon('chevron', 19)}</button></div>${metricIssues(metric)}</div>`
}
function overview() {
  const servers = state!.servers, healthy = servers.filter(server => statusOf(metricFor(server.id)) === 'healthy').length
  const warning = servers.filter(server => statusOf(metricFor(server.id)) === 'warning').length
  const metrics = state!.metrics.filter(m => !m.error)
  const avgCpu = metrics.length ? Math.round(metrics.reduce((acc, m) => acc + m.cpu, 0) / metrics.length) : 0
  const avgMemory = metrics.length ? Math.round(metrics.reduce((acc, m) => acc + m.memory, 0) / metrics.length) : 0
  return `${heading('OVERVIEW / 实时监控', '早上好，掌握服务的每一次心跳。', '查看所有服务器的运行状态，及时发现潜在风险。', button(`${icon('refresh', 17)} 刷新数据`, 'refresh', 'btn-outline'))}
    <div class="stats-grid">${statCard('server', '监控服务器', String(servers.length).padStart(2, '0'), `${healthy} 台运行正常 · ${warning} 台需要关注`, 'blue')}${statCard('cpu', '平均 CPU 占用', value(avgCpu), '所有在线服务器的平均值', 'violet')}${statCard('memory', '平均内存占用', value(avgMemory), '所有在线服务器的平均值', 'cyan')}${statCard('bell', '活跃告警', String(activeAlerts().length).padStart(2, '0'), activeAlerts().length ? '请及时查看并处理' : '当前没有需要处理的告警', 'orange')}</div>
    <div class="overview-grid"><section class="panel servers-panel"><div class="panel-head"><div><h2>服务器状态</h2><p>实时资源使用情况</p></div><button class="text-link" data-page="servers">管理服务器 ${icon('arrow', 16)}</button></div>
    ${servers.length ? `<div class="server-rows">${servers.map(overviewServerRow).join('')}</div>` : blank('尚未添加服务器', '添加服务器后，资源指标会显示在这里。', 'new-server', '添加服务器')}
    </section><aside class="overview-aside"><section class="panel alert-preview"><div class="panel-head"><div><h2>最近告警</h2><p>关注值得注意的变化</p></div><span class="count-label">${activeAlerts().length} 条活跃</span></div>${activeAlerts().length ? activeAlerts().slice(0, 3).map(alert => alertCard(alert, true)).join('') : `<div class="calm">${icon('check', 22)}<strong>一切运行平稳</strong><small>目前没有活跃告警</small></div>`}<button class="text-link aside-link" data-page="alerts">查看全部告警 ${icon('arrow', 16)}</button></section><section class="insight-card"><div class="insight-icon">${icon('sparkle', 22)}</div><div><h3>让日志自己说话</h3><p>借助 AI 快速定位问题，从海量日志中找到线索。</p><button data-page="analysis">开始分析 ${icon('arrow', 16)}</button></div></section></aside></div>`
}
function alertCard(alert: Alert, small = false) { const server = serverFor(alert.serverId); return `<div class="alert-item ${small ? 'small' : ''}"><span class="alert-symbol">${icon('warning', 19)}</span><div class="alert-info"><strong>${esc(alert.message || `${labelMetric(alert.metric)} 使用率异常`)}</strong><p>${esc(server?.name || alert.serverId)} · ${esc(labelMetric(alert.metric))}${Number.isFinite(alert.value) ? ` ${value(alert.value)}` : ''}</p><small>${fmt(alert.since)}</small></div><span class="alert-level">${alert.active ? '进行中' : '已恢复'}</span></div>` }
function alerts() { const current = activeAlerts(), history = state!.alerts.filter(alert => !alert.active); return `${heading('ALERT CENTER / 异常追踪', '每一次异常，都有迹可循。', '查看资源阈值告警和持续突增提醒。', button(`${icon('settings', 16)} 告警设置`, 'open-settings', 'btn-outline'))}<div class="alert-banner">${icon('shield', 23)}<div><strong>告警规则正在守护你的服务</strong><span>使用率超过 ${esc(state!.settings.threshold)}% 或突增 ${esc(state!.settings.spikeDelta)}% 并持续 ${esc(state!.settings.sustainedMinutes)} 分钟后，将按配置发送通知。</span></div></div><section class="panel full-panel"><div class="panel-head"><div><h2>活跃告警 <span class="heading-count">${current.length}</span></h2><p>当前仍需关注的异常</p></div></div>${current.length ? current.map(a => alertCard(a)).join('') : blank('暂无活跃告警', '所有已连接的服务器当前运行平稳。')}</section>${history.length ? `<section class="panel full-panel history-panel"><div class="panel-head"><div><h2>历史记录</h2><p>已恢复的告警</p></div></div>${history.map(a => alertCard(a)).join('')}</section>` : ''}` }
function serverDetails(metric?: Metric) {
  if (!metric) return '<div class="server-detail"><span class="muted">尚未采集，暂无磁盘挂载点数据。</span></div>'
  return `<div class="server-detail"><div class="disk-detail-head"><strong>磁盘挂载点</strong><span>${metric.disks.length} 个本地挂载点</span></div>${metric.disks.length ? `<div class="disk-list">${metric.disks.map(disk => `<div class="disk-item"><code>${esc(disk.mount)}</code><div>${meter(disk.percent)}</div><b class="${disk.percent > (state?.settings.threshold || 80) ? 'hot-text' : ''}">${value(disk.percent)}</b></div>`).join('')}</div>` : '<span class="muted">本次采集没有返回本地磁盘挂载点。</span>'}</div>`
}
function serverTableEntry(server: ServerInput) {
  const metric = metricFor(server.id), status = statusOf(metric), testing = connectingId === server.id
  return `<div class="server-table-entry"><div class="table-row"><div class="table-identity"><span class="server-symbol">${icon('server', 20)}</span><div><strong>${esc(server.name)}</strong><small>ID: ${esc(server.id.slice(0, 8))}</small></div></div><div class="table-cell"><b>${esc(server.host)}:${esc(server.port)}</b><small>${esc(server.user)}</small></div><div><span class="status ${status}"><i></i>${statusLabel(status)}</span></div><div class="muted">${metric?.timestamp ? fmt(metric.timestamp) : '尚未采集'}</div><div class="row-actions"><button class="icon-btn" title="磁盘与错误详情" aria-label="磁盘与错误详情" data-action="view-server" data-id="${esc(server.id)}">${icon('chevron', 17)}</button><button class="icon-btn ${testing ? 'connecting' : ''}" title="${testing ? '正在连接服务器' : '测试连接'}" aria-label="${testing ? '正在连接服务器' : '测试连接'}" data-action="test-server" data-id="${esc(server.id)}" ${testing ? 'disabled' : ''}>${icon(testing ? 'refresh' : 'link', 17)}</button><button class="icon-btn" title="编辑服务器" aria-label="编辑服务器" data-action="edit-server" data-id="${esc(server.id)}">${icon('edit', 17)}</button><button class="icon-btn danger-icon" title="删除服务器" aria-label="删除服务器" data-action="delete-server" data-id="${esc(server.id)}">${icon('trash', 17)}</button></div></div>${metricIssues(metric)}${detailId === server.id ? serverDetails(metric) : ''}</div>`
}
function servers() { return `${heading('INFRASTRUCTURE / 主机管理', '连接你的每一台服务器。', '集中管理 SSH 凭据与连接，所有密码均由本地保险库保护。', button(`${icon('plus', 17)} 添加服务器`, 'new-server', 'btn-primary'))}<div class="table-panel panel"><div class="table-head"><span>服务器</span><span>连接信息</span><span>运行状态</span><span>最近采集</span><span>操作</span></div>${state!.servers.length ? state!.servers.map(serverTableEntry).join('') : blank('还没有服务器', '添加一台 Linux 服务器开始监控。', 'new-server', '添加服务器')}</div><div class="helper-card">${icon('lock', 19)} <span>服务器凭据会保存在本地加密保险库中。编辑时留空密码可保留原有凭据。</span></div>` }
function projects() { return `${heading('PROJECTS / 日志来源', '把项目与日志，连接起来。', '将项目关联到服务器，配置项目目录和日志路径。', button(`${icon('plus', 17)} 新建项目`, 'new-project', 'btn-primary'))}<div class="project-grid">${state!.projects.map(project => `<article class="project-card"><div class="project-card-top"><div class="project-glyph">${icon('folder', 23)}</div><div class="row-actions"><button class="icon-btn" title="编辑项目" aria-label="编辑项目" data-action="edit-project" data-id="${esc(project.id)}">${icon('edit', 17)}</button><button class="icon-btn danger-icon" title="删除项目" aria-label="删除项目" data-action="delete-project" data-id="${esc(project.id)}">${icon('trash', 17)}</button></div></div><h3>${esc(project.name)}</h3><p>${icon('server', 15)} ${esc(serverFor(project.serverId)?.name || '服务器已移除')}</p><div class="project-path"><span>项目路径</span><code>${esc(project.projectPath)}</code></div><div class="project-path"><span>日志路径</span><code>${esc(project.logPath)}</code></div><button class="project-analyze" data-action="analyze-project" data-id="${esc(project.id)}">使用 AI 分析 ${icon('arrow', 17)}</button></article>`).join('')}${state!.projects.length ? `<button class="add-project" data-action="new-project">${icon('plus', 24)}<strong>添加新项目</strong><span>连接服务器上的更多日志</span></button>` : blank('暂无项目配置', '先添加服务器，再创建项目并指定日志路径。', 'new-project', '新建项目')}</div>` }
function analysis() {
  const project = state!.projects.find(p => p.id === projectId)
  const history = state!.analyses.filter(result => result.projectId === projectId)
  return `${heading('AI ANALYSIS / 智能诊断', '让每一行日志，都有答案。', '选择已配置的项目，提问并获取基于日志的分析结果。')}
    <div class="analysis-layout"><aside class="analysis-side panel"><h3>分析项目</h3><p>选择一个日志来源</p><div class="project-select-list">${state!.projects.map(p => `<button class="project-select ${p.id === projectId ? 'active' : ''}" data-action="select-project" data-id="${esc(p.id)}"><span>${icon('folder', 18)}</span><div><strong>${esc(p.name)}</strong><small>${esc(serverFor(p.serverId)?.name || '服务器已移除')}</small></div>${p.id === projectId ? icon('check', 16) : ''}</button>`).join('') || '<div class="muted pad">暂无项目，请先在项目配置中添加。</div>'}</div><div class="analysis-records"><strong>分析记录 <span>${history.length}</span></strong>${history.length ? history.map((result, index) => `<button type="button" data-action="view-analysis" data-index="${index}" title="${esc(result.question)}"><span>${esc(result.question)}</span><small>${fmt(result.generatedAt)}</small></button>`).reverse().join('') : '<p>此项目暂无分析记录</p>'}</div><div class="analysis-side-note">${icon('shield', 18)}<span>分析仅使用所选项目配置的日志</span></div></aside>
    <section class="chat panel"><div class="chat-head"><div class="chat-avatar">${icon('sparkle', 20)}</div><div><strong>日志分析助手</strong><small>${project ? `正在分析 · ${esc(project.name)}` : '请选择项目'}</small></div><span class="chat-live"><i></i> 已就绪</span></div><div class="chat-body" id="chat-body">${messages.length ? messages.map(m => `<div class="chat-message ${m.role}" ${m.result ? `data-record="${(messages.indexOf(m) - 1) / 2}"` : ''}><div class="message-avatar">${icon(m.role === 'assistant' ? 'sparkle' : 'server', 17)}</div><div class="message-content"><div class="message-bubble">${m.role === 'assistant' ? `<div class="markdown">${DOMPurify.sanitize(marked.parse(m.text, { async: false }) as string)}</div>` : esc(m.text)}</div>${m.result ? `<div class="message-meta">${esc(m.result.source || '日志分析')} · ${fmt(m.result.generatedAt)}</div><button class="export-link" data-action="export" data-index="${messages.indexOf(m)}">${icon('download', 15)} 导出 Markdown</button>` : ''}</div></div>`).join('') : `<div class="chat-welcome"><div class="welcome-icon">${icon('sparkle', 30)}</div><h2>从日志里，找到答案</h2><p>选择项目后描述你遇到的问题，AI 将结合日志内容帮助你定位原因。</p><div class="suggestions"><button data-action="suggest" data-question="最近有哪些错误？">最近有哪些错误？ ${icon('arrow', 14)}</button><button data-action="suggest" data-question="帮我分析服务响应变慢的原因">服务为什么变慢？ ${icon('arrow', 14)}</button></div></div>`}</div><form id="chat-form" class="chat-form"><div class="chat-input-wrap"><textarea name="question" id="question" rows="2" placeholder="描述你遇到的问题，例如：为什么订单服务频繁超时？" ${project ? '' : 'disabled'} required></textarea><button class="send-btn" type="submit" aria-label="发送问题" ${project ? '' : 'disabled'}>${icon('send', 19)}</button></div><small>分析结果仅供参考，请结合实际运行情况判断。按 Ctrl + Enter 发送。</small></form></section></div>`
}
function notificationState(channel: string) { return state!.notifications.find(item => item.channel === channel) }
function botStatus(channel: string) {
  const item = notificationState(channel)
  const labels: Record<string, string> = { disabled: '未启用', connecting: '连接中', online: '已连接', error: '连接异常' }
  return `<div class="bot-connection ${item?.status || 'disabled'}" data-channel="${channel}"><span class="status ${item?.status === 'online' ? 'healthy' : item?.status === 'error' ? 'offline' : ''}"><i></i>${labels[item?.status || 'disabled']}</span><small>${esc(item?.message || '保存设置后建立 WebSocket 连接')}</small></div>`
}
function targetList(channel: string, selected: string) {
  const targets = notificationState(channel)?.targets || []
  const all = Array.from(new Map([...targets, ...(selected ? [{ id: selected, name: '当前目标', kind: '' }] : [])].map(item => [item.id, item])).values())
  return all.map(target => `<option value="${esc(target.id)}">${esc(target.name)} · ${esc(target.id)}</option>`).join('')
}
function feishuSettings(s: Settings) {
  return `<div class="channel-field"><div class="channel-icon feishu">飞</div><div class="channel-info"><strong>飞书机器人</strong><small>官方 AppID / AppSecret，WebSocket 接收事件，OpenAPI 发送告警</small></div><label class="channel-toggle"><input type="checkbox" name="feishuEnabled" ${s.feishuEnabled ? 'checked' : ''} /><span>启用</span></label><button class="btn btn-outline small-btn" type="button" data-action="test-feishu">发送测试</button></div>${botStatus('feishu')}<div class="fields-grid">${field('AppID', 'feishuAppID', s.feishuAppID, { placeholder: 'cli_…', required: s.feishuEnabled })}${field('AppSecret', 'feishuAppSecret', '', { type: 'password', placeholder: s.feishuSecretSet ? '已配置 · 留空保留当前密钥' : '输入飞书 AppSecret', required: s.feishuEnabled && !s.feishuSecretSet, hint: '加密保存在本地，界面不可查看或复制' })}</div><label class="field"><span>目标会话 Chat ID</span><input name="feishuTargetID" value="${esc(s.feishuTargetID)}" list="feishu-targets" placeholder="选择已发现的会话或输入 chat_id" autocomplete="off" /><datalist id="feishu-targets">${targetList('feishu', s.feishuTargetID)}</datalist><small>给机器人发送一条消息后，事件会将会话加入列表</small></label><div class="channel-tools">${button(`${icon('refresh', 14)} 刷新会话`, 'refresh-targets', 'btn-text')}<span>请先在飞书后台启用 im.message.receive_v1 事件与消息发送权限</span></div><label class="clear-secret"><input type="checkbox" name="clearFeishu" /> 清除飞书应用配置并停用</label>`
}
function qqSettings(s: Settings) {
  return `<div class="channel-field second-channel"><div class="channel-icon qq">Q</div><div class="channel-info"><strong>QQ 机器人</strong><small>API v2 群聊 / C2C 单聊，AppID / AppSecret 鉴权</small></div><label class="channel-toggle"><input type="checkbox" name="qqEnabled" ${s.qqEnabled ? 'checked' : ''} /><span>启用</span></label><button class="btn btn-outline small-btn" type="button" data-action="test-qq">发送测试</button></div><div class="qq-warning">${icon('warning', 16)}<span>保存后先连接 QQ Gateway，无需目标 OpenID。若使用 OpenClaw 机器人，页面中的“灵魂”仍需由 OpenClaw Gateway 单独连接。<a href="https://q.qq.com/qqbot/openclaw/faq.html" target="_blank" rel="noreferrer">查看指引</a></span></div>${botStatus('qq')}<div class="fields-grid">${field('AppID', 'qqAppID', s.qqAppID, { placeholder: 'QQ 机器人 AppID', required: s.qqEnabled })}${field('AppSecret', 'qqAppSecret', '', { type: 'password', placeholder: s.qqSecretSet ? '已配置 · 留空保留当前密钥' : '输入 QQ AppSecret', required: s.qqEnabled && !s.qqSecretSet, hint: '加密保存在本地，界面不可查看或复制' })}</div><div class="fields-grid"><label class="field"><span>目标类型</span><select name="qqTargetType"><option value="group" ${s.qqTargetType !== 'c2c' ? 'selected' : ''}>群聊</option><option value="c2c" ${s.qqTargetType === 'c2c' ? 'selected' : ''}>C2C 单聊</option></select><small>群聊使用 group_openid，单聊使用 user_openid</small></label><label class="field"><span>目标 OpenID</span><input name="qqTargetID" value="${esc(s.qqTargetID)}" list="qq-targets" placeholder="选择已发现会话或输入 OpenID" autocomplete="off" /><datalist id="qq-targets">${targetList('qq', s.qqTargetID)}</datalist><small>在群内 @机器人或发起单聊后即可发现</small></label></div><div class="channel-tools">${button(`${icon('refresh', 14)} 刷新会话`, 'refresh-targets', 'btn-text')}<span>发送测试或告警前才需要目标；主动消息受平台频率与配额限制</span></div><label class="clear-secret"><input type="checkbox" name="clearQQ" /> 清除 QQ 应用配置并停用</label>`
}
function settings() { const s = state!.settings; const baseURL = s.modelBaseURL || modelBaseURLs[s.modelPlatform || 'custom'] || ''; const models = Array.from(new Set([s.modelName, ...modelOptions].filter(model => model.trim()))); return `${heading('PREFERENCES / 个性设置', '把重要的事，及时告诉你。', '设置采集间隔、告警规则、消息渠道与 AI 分析服务。')}
  <form id="settings-form" class="settings-layout"><div class="settings-main"><section class="panel settings-section"><div class="section-title"><span class="section-icon blue">${icon('pulse', 20)}</span><div><h2>监控与告警</h2><p>设置指标采集周期及异常判定条件</p></div></div><div class="fields-grid">${field('采集间隔（秒）', 'pollSeconds', s.pollSeconds, { type: 'number', min: 5, max: 86400, required: true, hint: '建议 30 秒，数值越小采集越频繁' })}${field('使用率预警阈值（%）', 'threshold', s.threshold, { type: 'number', min: 1, max: 100, required: true, hint: 'CPU、内存或磁盘超过阈值时预警' })}${field('突增幅度（%）', 'spikeDelta', s.spikeDelta, { type: 'number', min: 1, max: 100, required: true, hint: '资源占用在短时间内增长的幅度' })}${field('持续时间（分钟）', 'sustainedMinutes', s.sustainedMinutes, { type: 'number', min: 1, max: 1440, required: true, hint: '突增持续达到该时间后发送通知' })}</div></section>
  <section class="panel settings-section"><div class="section-title"><span class="section-icon orange">${icon('bell', 20)}</span><div><h2>消息通知</h2><p>按渠道启用告警推送；同一告警在单个渠道失败 3 次后停止本次投递</p></div></div>${feishuSettings(s)}${qqSettings(s)}</section>
  <section class="panel settings-section"><div class="section-title"><span class="section-icon violet">${icon('sparkle', 20)}</span><div><h2>AI 分析服务</h2><p>配置模型平台，使用 API Key 获取当前账号可用的官方模型</p></div></div><div class="fields-grid"><label class="field"><span>平台</span><select name="modelPlatform"><option value="deepseek" ${s.modelPlatform === 'deepseek' ? 'selected' : ''}>DeepSeek</option><option value="openai" ${s.modelPlatform === 'openai' ? 'selected' : ''}>OpenAI</option><option value="custom" ${!s.modelPlatform || s.modelPlatform === 'custom' ? 'selected' : ''}>OpenAI 兼容接口</option></select><small>切换平台会填充默认地址</small></label>${field('BaseURL（OpenAI 兼容根地址）', 'modelBaseURL', baseURL, { placeholder: 'https://api.deepseek.com', hint: '请求将使用 /models 和 /chat/completions' })}</div>${field('API Key', 'modelAPIKey', '', { type: 'password', placeholder: s.modelKeySet ? '已配置 · 留空保留密钥' : '输入 API Key', hint: '加密保存在本地；留空保留已配置密钥，界面不可查看或复制' })}<div class="model-picker"><label class="field"><span>模型名称</span><select name="modelName"><option value="">请选择可用模型</option>${models.map(model => `<option value="${esc(model)}" ${model === s.modelName ? 'selected' : ''}>${esc(model)}</option>`).join('')}</select><small>列表来自模型平台的 GET /models 接口</small></label><button class="btn btn-outline model-fetch" type="button" data-action="fetch-models">${icon('refresh', 15)} 获取可用模型</button></div></section></div><aside class="settings-side"><div class="save-card"><div class="save-card-icon">${icon('shield', 21)}</div><h3>配置已就绪？</h3><p>保存后，新设置将在下一轮监控中生效。</p><button type="submit" class="btn btn-primary">保存所有设置 ${icon('arrow', 17)}</button></div><div class="settings-hint">${icon('lock', 17)} 敏感信息不会在界面中显示。</div></aside></form>` }
function vault() {
  const setup = !state?.initialized
  return `<div class="vault-shell"><div class="vault-brand"><div class="brand-icon">${icon('pulse', 25)}</div><strong>脉点</strong><small>SERVICE PULSE</small></div><div class="vault-content">${isDemo ? '<span class="demo-pill vault-demo">界面演示 · 使用示例数据</span>' : ''}<div class="vault-decoration">${icon(setup ? 'shield' : 'lock', 47)}</div><div class="eyebrow">${setup ? 'WELCOME / 初始化' : 'WELCOME BACK / 欢迎回来'}</div><h1>${setup ? '开启你的监控工作台' : '守护服务，从这里开始。'}</h1><p>${setup ? '创建本地保险库，安全保存服务器连接凭据。' : '输入保险库密码，解锁服务器和监控数据。'}</p><form id="vault-form" class="vault-form">${field('保险库密码', 'passphrase', '', { type: 'password', placeholder: setup ? '设置一个安全的密码' : '输入保险库密码', required: true })}${setup ? field('确认密码', 'confirm', '', { type: 'password', placeholder: '再次输入密码', required: true }) : ''}<button type="submit" class="btn btn-primary vault-submit">${setup ? '创建保险库' : '解锁工作台'} ${icon('arrow', 18)}</button></form><div class="vault-footnote">${icon('shield', 16)} 密码用于本地加密，请妥善保管。</div>${initialError ? `<p class="startup-error">${esc(initialError)}</p>` : ''}</div><div class="vault-footer">© ${new Date().getFullYear()} 脉点 · 让服务状态清晰可见</div></div>`
}
function modalContent() {
  if (!modal) return ''
  const editing = !!selectedId
  let title = '', subtitle = '', content = ''
  if (modal === 'server') {
    const server = state!.servers.find(s => s.id === selectedId)
    title = editing ? '编辑服务器' : '添加服务器'; subtitle = '配置 SSH 连接信息，开始监控资源状态。'
    content = `<form id="server-form"><div class="modal-fields">${field('服务器名称', 'name', server?.name || '', { required: true, placeholder: '例如 生产服务器 A' })}${field('主机地址', 'host', server?.host || '', { required: true, placeholder: 'IP 地址或域名' })}<div class="fields-grid">${field('SSH 端口', 'port', server?.port || 22, { type: 'number', min: 1, max: 65535, required: true })}${field('登录用户', 'user', server?.user || '', { required: true, placeholder: '例如 deploy' })}</div>${field('SSH 密码', 'password', '', { type: 'password', placeholder: editing ? '留空保留现有密码' : '输入 SSH 登录密码', required: !editing, hint: editing ? '留空将保留现有密码' : '密码会写入本地加密保险库' })}${field('保险库密码', 'vaultPassword', '', { type: 'password', placeholder: '用于授权修改服务器', required: true })}</div><div class="modal-footer"><button type="button" class="btn btn-outline" data-action="close-modal">取消</button><button type="submit" class="btn btn-primary">${editing ? '保存修改' : '添加服务器'}</button></div></form>`
  } else if (modal === 'project') {
    const project = state!.projects.find(p => p.id === selectedId)
    title = editing ? '编辑项目' : '新建项目'; subtitle = '指定服务器、项目路径与日志文件位置。'
    content = `<form id="project-form"><div class="modal-fields">${field('项目名称', 'name', project?.name || '', { required: true, placeholder: '例如 订单服务' })}<label class="field"><span>关联服务器</span><select name="serverId" required><option value="">请选择服务器</option>${state!.servers.map(s => `<option value="${esc(s.id)}" ${project?.serverId === s.id ? 'selected' : ''}>${esc(s.name)} · ${esc(s.host)}</option>`).join('')}</select><small></small></label>${field('项目目录', 'projectPath', project?.projectPath || '', { required: true, placeholder: '/opt/services/my-app' })}${field('日志文件路径', 'logPath', project?.logPath || '', { required: true, placeholder: '/var/log/my-app/app.log', hint: '填写服务器上的绝对路径' })}</div><div class="modal-footer"><button type="button" class="btn btn-outline" data-action="close-modal">取消</button><button type="submit" class="btn btn-primary">${editing ? '保存修改' : '创建项目'}</button></div></form>`
  } else {
    const isServer = modal === 'delete-server', item = isServer ? state!.servers.find(s => s.id === selectedId) : state!.projects.find(p => p.id === selectedId)
    title = isServer ? '删除服务器？' : '删除项目？'; subtitle = `确定删除「${item?.name || ''}」吗？此操作无法撤销。`
    content = `<form id="delete-form">${isServer ? `<div class="modal-fields">${field('保险库密码', 'vaultPassword', '', { type: 'password', placeholder: '输入密码以确认删除', required: true })}</div>` : ''}<div class="modal-footer"><button type="button" class="btn btn-outline" data-action="close-modal">取消</button><button type="submit" class="btn btn-danger">确认删除</button></div></form>`
  }
  return `<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title"><div class="modal-head"><div><h2 id="modal-title">${esc(title)}</h2><p>${esc(subtitle)}</p></div><button class="icon-btn" aria-label="关闭" data-action="close-modal">${icon('close', 20)}</button></div>${content}</div></div>`
}
function render() {
  const active = state?.initialized && state?.unlocked
  app.innerHTML = `${active ? layout(({ overview, alerts, servers, projects, analysis, settings })[page]()) : vault()}${active ? modalContent() : ''}<div id="toast" class="toast" role="status" aria-live="polite"></div>`
  if (page === 'analysis') document.querySelector('#chat-body')?.scrollTo({ top: 999999 })
}

app.addEventListener('click', event => {
  const target = event.target as Element
  if (target.classList.contains('modal-backdrop')) { modal = null; render(); return }
  const control = target.closest<HTMLElement>('[data-page],[data-action]')
  if (!control) return
  if (control.dataset.page) { page = control.dataset.page as Page; sideOpen = false; if (page === 'analysis') { if (!projectId) projectId = state?.projects[0]?.id || ''; restoreMessages() } render(); return }
  const action = control.dataset.action, id = control.dataset.id || ''
  switch (action) {
    case 'toggle-side': sideOpen = !sideOpen; render(); break
    case 'close-side': sideOpen = false; render(); break
    case 'close-modal': modal = null; render(); break
    case 'new-server': case 'edit-server': modal = 'server'; selectedId = id; render(); break
    case 'delete-server': modal = 'delete-server'; selectedId = id; render(); break
    case 'new-project': case 'edit-project': if (!state?.servers.length) { toast('请先添加一台服务器', true); break } modal = 'project'; selectedId = id; render(); break
    case 'delete-project': modal = 'delete-project'; selectedId = id; render(); break
    case 'view-server': detailId = page === 'servers' && detailId === id ? '' : id; page = 'servers'; render(); break
    case 'select-project': projectId = id; restoreMessages(); render(); break
    case 'analyze-project': projectId = id; restoreMessages(); page = 'analysis'; render(); break
    case 'open-settings': page = 'settings'; render(); break
    case 'view-analysis': { const record = document.querySelector<HTMLElement>(`.chat-message.assistant[data-record="${Number(control.dataset.index)}"]`); record?.scrollIntoView({ behavior: 'smooth', block: 'center' }); break }
    case 'suggest': { const input = document.querySelector<HTMLTextAreaElement>('#question'); if (input) { input.value = control.dataset.question || ''; input.focus() } break }
    case 'refresh': void run(async () => { const metrics = await api.RefreshMetrics(); if (state) state.metrics = metrics; await reload() }, '指标已更新'); break
    case 'test-server': {
      connectingId = id; render()
      void run(async () => { const result = await api.TestConnection(id); await reload() }, 'SSH 连接成功，服务器状态已更新').finally(() => {
        connectingId = ''
        const control = document.querySelector<HTMLButtonElement>(`[data-action="test-server"][data-id="${CSS.escape(id)}"]`)
        if (control) { control.disabled = false; control.classList.remove('connecting'); control.title = '测试连接'; control.setAttribute('aria-label', '测试连接'); control.innerHTML = icon('link', 17) }
      })
      break
    }
    case 'fetch-models': void run(async () => { const form = document.querySelector<HTMLFormElement>('#settings-form'); if (!form) return; const data = new FormData(form); const platform = String(data.get('modelPlatform') || 'custom'), baseURL = String(data.get('modelBaseURL') || '').trim(), apiKey = String(data.get('modelAPIKey') || ''); modelOptions = await api.FetchModels(platform, baseURL, apiKey); const select = form.querySelector<HTMLSelectElement>('select[name="modelName"]'); const selected = select?.value || ''; if (select) { select.replaceChildren(new Option('请选择可用模型', '')); for (const model of modelOptions) select.add(new Option(model, model)); select.value = selected } if (state) state.settings = { ...state.settings, modelPlatform: platform, modelBaseURL: baseURL, modelAPIKey: '' } }, '模型列表已更新'); break
    case 'refresh-targets': void run(async () => { await reload() }, '会话列表已刷新；新会话需先向机器人发送消息'); break
    case 'test-feishu': case 'test-qq': void run(async () => { const form = document.querySelector<HTMLFormElement>('#settings-form'); if (!form) return; await api.TestNotification(action === 'test-feishu' ? 'feishu' : 'qq', settingsFromForm(form)) }, '测试通知已发送'); break
    case 'lock': void run(async () => { await api.LockVault(); messages = []; await reload() }, '工作台已锁定'); break
    case 'export': { const result = messages[Number(control.dataset.index)]?.result; if (result && window.confirm('报告包含日志摘录，可能含有个人信息、令牌或其他敏感数据。确定导出并妥善保管吗？')) void run(async () => { const path = await api.ExportAnalysis(result, ''); toast(`已导出到 ${path}`) }); break }
  }
})
app.addEventListener('keydown', event => { if (event.key === 'Escape' && modal) { modal = null; render() } if (event.key === 'Enter' && event.ctrlKey && (event.target as HTMLElement).id === 'question') { event.preventDefault(); document.querySelector<HTMLFormElement>('#chat-form')?.requestSubmit() } })
app.addEventListener('change', event => {
  const target = event.target as HTMLElement
  if (!(target instanceof HTMLSelectElement) || target.name !== 'modelPlatform') return
  const form = target.closest('form'), base = form?.querySelector<HTMLInputElement>('input[name="modelBaseURL"]'), model = form?.querySelector<HTMLSelectElement>('select[name="modelName"]')
  const current = base?.value.trim() || ''
  if (base && (!current || Object.values(modelBaseURLs).includes(current))) base.value = modelBaseURLs[target.value] ?? ''
  modelOptions = []
  if (model) model.innerHTML = '<option value="">请获取新平台的可用模型</option>'
})
function settingsFromForm(form: HTMLFormElement): Settings {
  const data = new FormData(form), text = (key: string) => String(data.get(key) || '').trim(), num = (key: string) => Number(data.get(key))
  return { pollSeconds: num('pollSeconds'), threshold: num('threshold'), spikeDelta: num('spikeDelta'), sustainedMinutes: num('sustainedMinutes'), notificationsVersion: 2, feishuEnabled: !data.has('clearFeishu') && data.has('feishuEnabled'), feishuAppID: data.has('clearFeishu') ? '' : text('feishuAppID'), feishuAppSecret: data.has('clearFeishu') ? '' : text('feishuAppSecret'), feishuSecretSet: state!.settings.feishuSecretSet, feishuTargetID: data.has('clearFeishu') ? '' : text('feishuTargetID'), feishuWebhook: '', qqEnabled: !data.has('clearQQ') && data.has('qqEnabled'), qqAppID: data.has('clearQQ') ? '' : text('qqAppID'), qqAppSecret: data.has('clearQQ') ? '' : text('qqAppSecret'), qqSecretSet: state!.settings.qqSecretSet, qqTargetType: text('qqTargetType') || 'group', qqTargetID: data.has('clearQQ') ? '' : text('qqTargetID'), qqWebhook: '', modelPlatform: text('modelPlatform'), modelBaseURL: text('modelBaseURL'), modelEndpoint: '', modelName: text('modelName'), modelAPIKey: text('modelAPIKey'), modelKeySet: state!.settings.modelKeySet }
}
app.addEventListener('submit', event => {
  event.preventDefault()
  const form = event.target as HTMLFormElement
  const data = new FormData(form), text = (key: string) => String(data.get(key) || '').trim(), num = (key: string) => Number(data.get(key))
  switch (form.id) {
    case 'vault-form': void run(async () => { const passphrase = text('passphrase'); if (!passphrase) throw new Error('请输入保险库密码'); if (!state?.initialized) { if (passphrase !== text('confirm')) throw new Error('两次输入的密码不一致'); await api.InitializeVault(passphrase, []) } else await api.UnlockVault(passphrase); await reload() }, state?.initialized ? '已解锁工作台' : '保险库已创建'); break
    case 'server-form': void run(async () => { const passphrase = text('vaultPassword'); const server: ServerInput = { id: selectedId || uid(), name: text('name'), host: text('host'), port: num('port'), user: text('user'), password: String(data.get('password') || '') }; if (!server.name || !server.host || !server.user || !passphrase || !Number.isInteger(server.port) || server.port < 1 || server.port > 65535) throw new Error('请填写有效的服务器连接信息'); await api.UpsertServer(server, passphrase); modal = null; await reload() }, selectedId ? '服务器已更新' : '服务器已添加'); break
    case 'project-form': void run(async () => { const project: Project = { id: selectedId || uid(), name: text('name'), serverId: text('serverId'), projectPath: text('projectPath'), logPath: text('logPath') }; if (!project.name || !project.serverId || !project.projectPath.startsWith('/') || !project.logPath.startsWith('/')) throw new Error('请填写项目名称与服务器上的绝对路径'); await api.SaveProject(project); modal = null; await reload() }, selectedId ? '项目已更新' : '项目已创建'); break
    case 'delete-form': void run(async () => { if (modal === 'delete-server') { const passphrase = text('vaultPassword'); if (!passphrase) throw new Error('请输入保险库密码'); await api.DeleteServer(selectedId, passphrase) } else await api.DeleteProject(selectedId); modal = null; await reload() }, '已删除'); break
    case 'settings-form': void run(async () => { const settings = settingsFromForm(form); if ([settings.pollSeconds, settings.threshold, settings.spikeDelta, settings.sustainedMinutes].some(n => !Number.isInteger(n) || n <= 0) || settings.threshold > 100 || settings.spikeDelta > 100) throw new Error('请填写有效的监控参数'); await api.SaveSettings(settings); await reload() }, '设置已保存'); break
    case 'chat-form': { const question = text('question'); if (!projectId || !question) return; const selectedProject = projectId, history = conversationHistory(); messages.push({ role: 'user', text: question }); render(); void run(async () => { await api.AnalyzeLog(selectedProject, question, history); state = await api.GetState(); if (projectId === selectedProject) { restoreMessages(); render() } }).finally(() => { if (projectId === selectedProject && messages.length % 2 !== 0) { restoreMessages(); render() } }); break }
  }
})

async function start() { try { state = await api.GetState(); projectId = state.projects[0]?.id || ''; restoreMessages(); render() } catch (error) { initialError = errorText(error); state = { initialized: false, unlocked: false, servers: [], projects: [], settings: {} as Settings, metrics: [], alerts: [], notifications: [], analyses: [] }; render(); toast(`无法加载应用状态：${initialError}`, true) } }

function canPoll() {
  const focused = document.activeElement
  return !!state?.initialized && !!state.unlocked && (page === 'overview' || page === 'alerts' || page === 'servers' || page === 'settings') && !modal && !busy && !polling && !document.hidden &&
    !(focused instanceof HTMLElement && focused.matches('input, textarea, select, [contenteditable="true"]'))
}
function refreshNotificationUI() {
  for (const channel of ['feishu', 'qq']) {
    const current = document.querySelector<HTMLElement>(`.bot-connection[data-channel="${channel}"]`)
    if (current) current.outerHTML = botStatus(channel)
    const input = document.querySelector<HTMLInputElement>(`input[list="${channel}-targets"]`)
    const list = document.querySelector<HTMLDataListElement>(`#${channel}-targets`)
    if (input && list) list.innerHTML = targetList(channel, input.value)
  }
}
async function pollState() {
  if (!canPoll()) return
  polling = true
  const startingPage = page, version = pollVersion
  try {
    const next = await api.GetState()
    pollError = ''
    if (version === pollVersion && startingPage === page && !modal && !busy && !document.hidden &&
      !(document.activeElement instanceof HTMLElement && document.activeElement.matches('input, textarea, select, [contenteditable="true"]')) &&
      JSON.stringify(next) !== JSON.stringify(state)) {
      if (startingPage === 'settings') { state = next; refreshNotificationUI() } else { state = next; render() }
    }
  } catch (error) {
    const message = errorText(error)
    if (message !== pollError) { pollError = message; toast(`状态刷新失败：${message}`, true) }
  } finally { polling = false }
}
void start()
window.setInterval(() => { void pollState() }, 10_000)
