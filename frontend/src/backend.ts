export interface ServerInput { id: string; name: string; host: string; port: number; user: string; password: string }
export interface Project { id: string; name: string; serverId: string; projectPath: string; logPath: string }
export interface Metric { serverId: string; cpu: number; memory: number; disks: { mount: string; percent: number }[]; timestamp: string; error: string; notificationError: string }
export interface Alert { id: string; serverId: string; kind: string; metric: string; value: number; message: string; active: boolean; since: string }
export interface BotTarget { id: string; name: string; kind: string }
export interface NotificationStatus { channel: string; status: string; message: string; targets: BotTarget[] }
export interface Settings { pollSeconds: number; threshold: number; spikeDelta: number; sustainedMinutes: number; notificationsVersion: number; feishuEnabled: boolean; feishuAppID: string; feishuAppSecret: string; feishuSecretSet: boolean; feishuTargetID: string; feishuWebhook: string; qqEnabled: boolean; qqAppID: string; qqAppSecret: string; qqSecretSet: boolean; qqTargetType: string; qqTargetID: string; qqWebhook: string; modelPlatform: string; modelBaseURL: string; modelEndpoint: string; modelName: string; modelAPIKey: string; modelKeySet: boolean }
export interface AppState { initialized: boolean; unlocked: boolean; servers: ServerInput[]; projects: Project[]; settings: Settings; metrics: Metric[]; alerts: Alert[]; notifications: NotificationStatus[]; analyses: AnalysisResult[] }
export interface AnalysisTurn { question: string; answer: string }
export interface AnalysisResult { projectId: string; question: string; answer: string; generatedAt: string; source: string; logExcerpt: string }

interface AppBindings {
  GetState(): Promise<AppState>
  InitializeVault(passphrase: string, servers: ServerInput[]): Promise<void>
  UnlockVault(passphrase: string): Promise<void>
  LockVault(): Promise<void>
  UpsertServer(server: ServerInput, passphrase: string): Promise<void>
  DeleteServer(id: string, passphrase: string): Promise<void>
  TestConnection(id: string): Promise<string>
  RefreshMetrics(): Promise<Metric[]>
  SaveProject(project: Project): Promise<void>
  DeleteProject(id: string): Promise<void>
  SaveSettings(settings: Settings): Promise<void>
  FetchModels(platform: string, baseURL: string, apiKey: string): Promise<string[]>
  AnalyzeLog(projectId: string, question: string, history: AnalysisTurn[]): Promise<AnalysisResult>
  ExportAnalysis(result: AnalysisResult, path: string): Promise<string>
  TestNotification(channel: string, settings: Settings): Promise<void>
}

declare global { interface Window { go?: { main?: { App?: AppBindings } } } }

const now = new Date()
const minutesAgo = (n: number) => new Date(now.getTime() - n * 60_000).toISOString()
const demo: AppState = {
  initialized: true, unlocked: true,
  servers: [
    { id: 'srv-01', name: '生产服务器 A', host: '192.168.1.101', port: 22, user: 'deploy', password: '' },
    { id: 'srv-02', name: '生产服务器 B', host: '192.168.1.102', port: 22, user: 'deploy', password: '' },
    { id: 'srv-03', name: '测试环境', host: '192.168.1.120', port: 22, user: 'tester', password: '' },
  ],
  projects: [
    { id: 'proj-01', name: '订单服务', serverId: 'srv-01', projectPath: '/opt/services/orders', logPath: '/var/log/orders/app.log' },
    { id: 'proj-02', name: '支付网关', serverId: 'srv-02', projectPath: '/opt/services/payment', logPath: '/var/log/payment/error.log' },
  ],
  settings: { pollSeconds: 30, threshold: 80, spikeDelta: 25, sustainedMinutes: 5, notificationsVersion: 2, feishuEnabled: false, feishuAppID: 'cli_demo', feishuAppSecret: '', feishuSecretSet: false, feishuTargetID: 'oc_demo_chat', feishuWebhook: '', qqEnabled: false, qqAppID: '102000000', qqAppSecret: '', qqSecretSet: false, qqTargetType: 'group', qqTargetID: 'GROUP-DEMO', qqWebhook: '', modelPlatform: 'deepseek', modelBaseURL: 'https://api.deepseek.com', modelEndpoint: '', modelName: 'deepseek-chat', modelAPIKey: '', modelKeySet: false },
  analyses: [{ projectId: 'proj-01', question: '最近有哪些错误？', answer: '演示记录：请检查订单服务的超时错误与上游响应。', generatedAt: minutesAgo(20), source: 'srv-01:/var/log/orders/app.log', logExcerpt: 'ERROR upstream request timeout' }],
  notifications: [{ channel: 'feishu', status: 'online', message: 'WebSocket 已连接（演示）', targets: [{ id: 'oc_demo_chat', name: '飞书 group 会话', kind: 'group' }] }, { channel: 'qq', status: 'disabled', message: '', targets: [{ id: 'GROUP-DEMO', name: 'QQ 群聊', kind: 'group' }] }],
  metrics: [
    { serverId: 'srv-01', cpu: 42, memory: 68, disks: [{ mount: '/', percent: 56 }, { mount: '/data', percent: 73 }], timestamp: minutesAgo(1), error: '', notificationError: '' },
    { serverId: 'srv-02', cpu: 87, memory: 76, disks: [{ mount: '/', percent: 64 }, { mount: '/data', percent: 82 }], timestamp: minutesAgo(1), error: '', notificationError: '' },
    { serverId: 'srv-03', cpu: 24, memory: 38, disks: [{ mount: '/', percent: 31 }], timestamp: minutesAgo(2), error: '', notificationError: '' },
  ],
  alerts: [{ id: 'alert-01', serverId: 'srv-02', kind: 'threshold', metric: 'cpu', value: 87, message: 'CPU 使用率超过预警阈值', active: true, since: minutesAgo(12) }],
}

export const isDemo = !window.go?.main?.App
const real = window.go?.main?.App
const copy = <T>(value: T): T => structuredClone(value)
export const api: AppBindings = real || {
  async GetState() { return copy(demo) },
  async InitializeVault(_passphrase, servers) { demo.initialized = true; demo.unlocked = true; demo.servers = copy(servers) },
  async UnlockVault(passphrase) { if (!passphrase) throw new Error('请输入解锁密码'); demo.unlocked = true },
  async LockVault() { demo.unlocked = false },
  async UpsertServer(server) { const index = demo.servers.findIndex(item => item.id === server.id); const safe = { ...server, password: '' }; if (index < 0) demo.servers.push(safe); else demo.servers[index] = safe },
  async DeleteServer(id) { demo.servers = demo.servers.filter(item => item.id !== id); demo.metrics = demo.metrics.filter(item => item.serverId !== id) },
  async TestConnection(id) {
    await new Promise(resolve => setTimeout(resolve, 650))
    if (!demo.metrics.some(metric => metric.serverId === id)) demo.metrics.push({ serverId: id, cpu: 18, memory: 36, disks: [{ mount: '/', percent: 42 }], timestamp: new Date().toISOString(), error: '', notificationError: '' })
    return '连接成功（演示数据）'
  },
  async RefreshMetrics() { return copy(demo.metrics) },
  async SaveProject(project) { const index = demo.projects.findIndex(item => item.id === project.id); if (index < 0) demo.projects.push(copy(project)); else demo.projects[index] = copy(project) },
  async DeleteProject(id) { demo.projects = demo.projects.filter(item => item.id !== id) },
  async SaveSettings(settings) { demo.settings = { ...copy(settings), modelAPIKey: '', feishuAppSecret: '', qqAppSecret: '', modelKeySet: !!settings.modelAPIKey || demo.settings.modelKeySet, feishuSecretSet: !!settings.feishuAppSecret || demo.settings.feishuSecretSet, qqSecretSet: !!settings.qqAppSecret || demo.settings.qqSecretSet } },
  async FetchModels() { return ['deepseek-chat', 'deepseek-reasoner'] },
  async AnalyzeLog(projectId, question, _history) { const result = { projectId, question, answer: '这是界面演示结果。连接 Wails 后，这里会展示基于真实日志的分析内容。\n\n建议检查对应时段的错误日志、请求链路和上游依赖状态。', generatedAt: new Date().toISOString(), source: '界面演示', logExcerpt: '2026-11-05 14:32:18 ERROR upstream request timeout' }; demo.analyses.push(result); return result },
  async ExportAnalysis() { throw new Error('演示模式无法写入本地文件，请在桌面应用中导出') },
  async TestNotification() { throw new Error('演示模式无法发送通知，请在桌面应用中测试') },
}
