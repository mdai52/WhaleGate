<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  ApiOutlined,
  ArrowRightOutlined,
  DeleteOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { mcpApi } from '@/api/mcp'
import { parseJSON } from '@/utils/json'
import type { ExposedTool, MCPServer } from '@/api/types'

const loading = ref(false)
/** 按服务 ID 记录"连接中/测试中"，避免一个全局开关让所有行同时转圈 */
const connectingIds = ref<Set<number>>(new Set())
const testingIds = ref<Set<number>>(new Set())
const servers = ref<MCPServer[]>([])
const tools = ref<ExposedTool[]>([])

const drawerOpen = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)

const form = reactive({
  name: '',
  transport: 'stdio',
  command: '',
  args: '',
  env: '',
  url: '',
  headers: '',
  auto_execute: false,
  enabled: true,
})

const callOpen = ref(false)
const callToolName = ref('')
const callArgs = ref('{}')
const callResult = ref('')
const calling = ref(false)

const statusMeta: Record<string, { text: string; color: string }> = {
  idle: { text: '未连接', color: 'default' },
  connected: { text: '已连接', color: 'green' },
  error: { text: '异常', color: 'red' },
  disabled: { text: '已禁用', color: 'default' },
}

const autoTools = computed(() => tools.value.filter((t) => t.auto_execute).length)

/** 空状态下的接入步骤：让人不点「新增服务」也知道接下来会发生什么 */
const guideSteps = [
  { title: '新增服务', desc: '填启动命令或服务地址，stdio / HTTP 都支持' },
  { title: '连接并发现工具', desc: '点「连接」拉取工具列表，可先用「测试」确认连通' },
  { title: '选择注入方式', desc: '透传给客户端自己执行，或开启网关代执行' },
]

/** 常用模板：一键把表单填好，用户只需改路径与令牌 */
interface McpTemplate {
  key: string
  title: string
  desc: string
  transport: 'stdio' | 'http'
  command?: string
  args?: string
  env?: string
  url?: string
  headers?: string
}

const templates: McpTemplate[] = [
  {
    key: 'filesystem',
    title: 'Filesystem',
    desc: '读写本地文件，最适合用来验证整条链路',
    transport: 'stdio',
    command: 'npx',
    args: '-y @modelcontextprotocol/server-filesystem /tmp',
  },
  {
    key: 'fetch',
    title: 'Fetch',
    desc: '抓取网页并转成 Markdown 喂给模型',
    transport: 'stdio',
    command: 'python3',
    args: '-m mcp_server_fetch',
  },
  {
    key: 'remote',
    title: '远程 HTTP 服务',
    desc: '接入托管在别处的 MCP 服务，常带令牌鉴权',
    transport: 'http',
    url: 'https://mcp.example.com/mcp',
    headers: 'Authorization: Bearer <token>',
  },
]

function useTemplate(t: McpTemplate) {
  editingId.value = null
  Object.assign(form, {
    name: t.key,
    transport: t.transport,
    command: t.command ?? '',
    args: t.args ?? '',
    env: t.env ?? '',
    url: t.url ?? '',
    headers: t.headers ?? '',
    auto_execute: false,
    enabled: true,
  })
  drawerOpen.value = true
}

async function load() {
  loading.value = true
  try {
    servers.value = await mcpApi.list()
    tools.value = await mcpApi.tools()
  } catch {
    message.error('加载 MCP 服务失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '',
    transport: 'stdio',
    command: '',
    args: '',
    env: '',
    url: '',
    headers: '',
    auto_execute: false,
    enabled: true,
  })
  drawerOpen.value = true
}

function openEdit(row: MCPServer) {
  editingId.value = row.id
  const args = parseJSON<string[]>(row.args, []).value
  const env = parseJSON<Record<string, string>>(row.env, {}).value
  Object.assign(form, {
    name: row.name,
    transport: row.transport,
    command: row.command,
    args: args.join(' '),
    env: Object.entries(env).map(([k, v]) => `${k}=${v}`).join('\n'),
    url: row.url,
    // 请求头与环境变量一样按行展示，便于手工核对
    headers: Object.entries(parseJSON<Record<string, string>>(row.headers, {}).value)
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n'),
    auto_execute: row.auto_execute,
    enabled: row.enabled,
  })
  drawerOpen.value = true
}

function buildPayload() {
  const payload: Record<string, unknown> = {
    name: form.name.trim(),
    transport: form.transport,
    auto_execute: form.auto_execute,
    enabled: form.enabled,
  }
  if (form.transport === 'stdio') {
    payload.command = form.command.trim()
    payload.args = form.args.trim() ? form.args.trim().split(/\s+/) : []
    const env: Record<string, string> = {}
    for (const line of form.env.split('\n')) {
      const idx = line.indexOf('=')
      if (idx > 0) {
        env[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
      }
    }
    payload.env = env
  } else {
    payload.url = form.url.trim()
    const headers: Record<string, string> = {}
    for (const line of form.headers.split('\n')) {
      const idx = line.indexOf(':')
      if (idx > 0) {
        headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
      }
    }
    payload.headers = headers
  }
  return payload
}

async function submit() {
  if (!form.name.trim()) {
    message.warning('请填写服务名称')
    return
  }
  if (form.transport === 'stdio' && !form.command.trim()) {
    message.warning('stdio 传输需要填写启动命令')
    return
  }
  if (form.transport === 'http' && !form.url.trim()) {
    message.warning('http 传输需要填写服务地址')
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    if (editingId.value) {
      await mcpApi.update(editingId.value, payload)
      message.success('已更新')
    } else {
      await mcpApi.create(payload as never)
      message.success('已创建')
    }
    drawerOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function connect(row: MCPServer) {
  // 按行记录连接中状态：此前是全局 ref，点一行会让所有行一起转圈。
  connectingIds.value.add(row.id)
  try {
    const res = await mcpApi.connect(row.id)
    message.success(`已连接，发现 ${res.tool_count} 个工具`)
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '连接失败')
    await load()
  } finally {
    connectingIds.value.delete(row.id)
  }
}

/** 连通性测试：只连一次看结果，不写库、不改变启用状态 */
async function testServer(row: MCPServer) {
  testingIds.value.add(row.id)
  try {
    const res = await mcpApi.test(row.id)
    message.success(`连通正常：${res.tool_count} 个工具，耗时 ${res.latency_ms} ms`)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '连通性测试失败')
  } finally {
    testingIds.value.delete(row.id)
  }
}

/** 启停服务：停用后不再参与工具发现与调用，配置保留 */
async function toggleServer(row: MCPServer, checked: boolean) {
  await mcpApi.toggle(row.id, checked)
  message.success(checked ? `已启用「${row.name}」` : `已停用「${row.name}」，其工具不再注入`)
  await load()
}

function removeServer(row: MCPServer) {
  Modal.confirm({
    title: '确认删除该 MCP 服务？',
    content: `删除后「${row.name}」的工具将不再注入。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await mcpApi.remove(row.id)
      message.success('已删除')
      await load()
    },
  })
}

async function toggleAutoExecute(row: MCPServer, checked: boolean) {
  await mcpApi.update(row.id, { auto_execute: checked })
  message.success(checked ? '已开启网关代执行' : '已改为工具透传')
  await load()
}

function openCall() {
  callToolName.value = tools.value[0]?.name ?? ''
  callArgs.value = '{}'
  callResult.value = ''
  callOpen.value = true
}

async function submitCall() {
  const parsed = parseJSON<Record<string, unknown>>(callArgs.value, {})
  if (!parsed.ok) {
    message.error('参数 JSON 格式错误：' + parsed.error)
    return
  }
  calling.value = true
  try {
    const res = await mcpApi.call(callToolName.value, parsed.value)
    callResult.value = res.result
  } catch (error) {
    callResult.value = error instanceof Error ? error.message : '调用失败'
  } finally {
    calling.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <div>
          <h2 style="margin: 0">MCP 服务</h2>
          <div class="wg-stats">
            <div class="wg-stat">
              <b>{{ servers.length }}</b><span>服务</span>
            </div>
            <div class="wg-stat">
              <b>{{ tools.length }}</b><span>工具</span>
            </div>
            <div class="wg-stat">
              <b>{{ autoTools }}</b><span>网关代执行</span>
            </div>
          </div>
        </div>
        <a-space>
          <a-button :loading="loading" @click="load">
            <ReloadOutlined />
            刷新
          </a-button>
          <a-tooltip :title="tools.length === 0 ? '先连接服务发现工具后才能调试' : ''">
            <a-button :disabled="tools.length === 0" @click="openCall">调试调用</a-button>
          </a-tooltip>
          <a-button type="primary" @click="openCreate">
            <PlusOutlined />
            新增服务
          </a-button>
        </a-space>
      </div>

      <!-- 两种模式用并列卡片呈现，比一段塞满术语的长文案好读 -->
      <div class="wg-modes">
        <div class="wg-mode">
          <div class="wg-mode-head">
            <ApiOutlined />
            <span>工具透传</span>
            <a-tag color="blue">默认</a-tag>
          </div>
          <p>把工具定义注入请求，由客户端自己执行调用循环。</p>
          <div class="wg-mode-foot">不改计量 · 风险最低 · 客户端需自行实现工具循环</div>
        </div>
        <div class="wg-mode">
          <div class="wg-mode-head">
            <ThunderboltOutlined />
            <span>网关代执行</span>
          </div>
          <p>网关调用 MCP 并把结果回喂模型，直到不再产生工具调用。</p>
          <div class="wg-mode-foot">仅非流式请求 · 多轮 token 累计计费 · 客户端零改动</div>
        </div>
      </div>

      <!-- 空状态给一条可照着走的路径，而不是一张空表格 -->
      <div v-if="servers.length === 0 && !loading" class="wg-guide">
        <div class="wg-guide-steps">
          <div v-for="(s, i) in guideSteps" :key="i" class="wg-guide-step">
            <div class="wg-guide-no">{{ i + 1 }}</div>
            <div>
              <div class="wg-guide-title">{{ s.title }}</div>
              <div class="wg-muted">{{ s.desc }}</div>
            </div>
          </div>
        </div>

        <a-divider class="wg-guide-divider">
          <span class="wg-muted">从模板开始，填好即可连接</span>
        </a-divider>

        <div class="wg-templates">
          <div v-for="t in templates" :key="t.key" class="wg-template" @click="useTemplate(t)">
            <div class="wg-template-title">{{ t.title }}</div>
            <div class="wg-muted">{{ t.desc }}</div>
            <div class="wg-template-go">
              使用此模板
              <ArrowRightOutlined />
            </div>
          </div>
        </div>

        <div class="wg-guide-foot">
          <span class="wg-muted">模板只是预填表单，路径与令牌需要按你的环境调整</span>
        </div>
      </div>

      <a-table
        v-else
        :columns="[
          { title: '名称', dataIndex: 'name', key: 'name' },
          { title: '传输', dataIndex: 'transport', key: 'transport' },
          { title: '状态', dataIndex: 'status', key: 'status' },
          { title: '工具', dataIndex: 'tool_count', key: 'tool_count' },
          { title: '代执行', key: 'auto' },
          { title: '操作', key: 'action', fixed: 'right' },
        ]"
        :data-source="servers"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 720 }"
        :pagination="false"
      >
        <template #emptyText>
          <a-empty description="还没有 MCP 服务，点「新增服务」接入第一个" />
        </template>
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'transport'">
            <a-tag>{{ (record as MCPServer).transport }}</a-tag>
            <span class="wg-muted">
              {{ (record as MCPServer).transport === 'stdio' ? (record as MCPServer).command : (record as MCPServer).url }}
            </span>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-space :size="4">
              <a-tag :color="statusMeta[(record as MCPServer).status]?.color">
                {{ statusMeta[(record as MCPServer).status]?.text ?? (record as MCPServer).status }}
              </a-tag>
              <!-- 停用后工具不再注入，配置保留，随时可再启用 -->
              <a-switch
                size="small"
                :checked="(record as MCPServer).enabled"
                checked-children="启用"
                un-checked-children="停用"
                @change="(checked: boolean) => toggleServer(record as MCPServer, checked)"
              />
            </a-space>
            <div v-if="(record as MCPServer).last_error" class="wg-muted">
              {{ (record as MCPServer).last_error.slice(0, 60) }}
            </div>
          </template>
          <template v-else-if="column.key === 'auto'">
            <a-switch
              size="small"
              :checked="(record as MCPServer).auto_execute"
              @change="(checked: boolean) => toggleAutoExecute(record as MCPServer, checked)"
            />
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button
                size="small"
                :loading="connectingIds.has((record as MCPServer).id)"
                @click="connect(record as MCPServer)"
              >
                <ThunderboltOutlined />
                连接
              </a-button>
              <a-button
                size="small"
                :loading="testingIds.has((record as MCPServer).id)"
                @click="testServer(record as MCPServer)"
              >
                测试
              </a-button>
              <a-button size="small" @click="openEdit(record as MCPServer)">编辑</a-button>
              <a-button danger size="small" type="text" @click="removeServer(record as MCPServer)">
                <DeleteOutlined />
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-card title="工具列表" style="margin-top: 16px">
      <a-empty v-if="tools.length === 0" description="连接服务并发现工具后，这里会列出对外暴露的工具名、说明与调用方式" />
      <a-list v-else :data-source="tools" size="small">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-list-item-meta>
              <template #title>
                <code>{{ (item as ExposedTool).name }}</code>
                <a-tag v-if="(item as ExposedTool).auto_execute" color="green" style="margin-left: 8px">
                  代执行
                </a-tag>
                <a-tag v-else style="margin-left: 8px">透传</a-tag>
              </template>
              <template #description>
                {{ (item as ExposedTool).description || '—' }}
                <span class="wg-muted">
                  ｜ 来源 {{ (item as ExposedTool).server_name }}/{{ (item as ExposedTool).tool_name }}
                </span>
              </template>
            </a-list-item-meta>
          </a-list-item>
        </template>
      </a-list>
    </a-card>

    <a-drawer v-model:open="drawerOpen" :title="editingId ? '编辑 MCP 服务' : '新增 MCP 服务'" :width="520" placement="right">
      <a-form layout="vertical">
        <a-form-item label="服务名称" required>
          <a-input v-model:value="form.name" placeholder="例如：filesystem" allow-clear />
        </a-form-item>
        <a-form-item label="传输方式" required>
          <a-radio-group v-model:value="form.transport">
            <a-radio value="stdio">stdio（本地子进程）</a-radio>
            <a-radio value="http">HTTP（远程服务）</a-radio>
          </a-radio-group>
        </a-form-item>

        <template v-if="form.transport === 'stdio'">
          <a-form-item label="启动命令" required>
            <a-input v-model:value="form.command" placeholder="例如：npx 或 python3" allow-clear />
          </a-form-item>
          <a-form-item label="参数">
            <a-input v-model:value="form.args" placeholder="空格分隔，例如：-y @modelcontextprotocol/server-filesystem /tmp" allow-clear />
          </a-form-item>
          <a-form-item label="环境变量">
            <a-textarea v-model:value="form.env" :rows="3" placeholder="每行一个，格式 KEY=VALUE" />
          </a-form-item>
        </template>
        <template v-else>
          <a-form-item label="服务地址" required>
            <a-input v-model:value="form.url" placeholder="https://mcp.example.com/mcp" allow-clear />
          </a-form-item>
          <!-- 后端支持自定义请求头，常用于带鉴权令牌的远程 MCP 服务 -->
          <a-form-item label="请求头">
            <a-textarea
              v-model:value="form.headers"
              :rows="2"
              placeholder="每行一个，格式 Key: Value，例如 Authorization: Bearer xxx"
            />
          </a-form-item>
        </template>

        <a-form-item label="启用">
          <a-switch v-model:checked="form.enabled" checked-children="启用" un-checked-children="停用" />
          <div class="wg-muted">停用后该服务的工具不再注入，配置保留</div>
        </a-form-item>

        <a-form-item>
          <template #label>
            网关代执行
            <a-tooltip title="开启后由网关调用工具并回喂模型（仅非流式）；关闭则只把工具定义透传给客户端">
              <ApiOutlined class="wg-muted" />
            </a-tooltip>
          </template>
          <a-switch v-model:checked="form.auto_execute" checked-children="开" un-checked-children="关" />
        </a-form-item>

        <a-button type="primary" block :loading="saving" @click="submit">保存</a-button>
      </a-form>
    </a-drawer>

    <a-modal v-model:open="callOpen" title="调试工具调用" :footer="null" :width="560">
      <a-form layout="vertical">
        <a-form-item label="工具">
          <a-select v-model:value="callToolName" show-search :options="tools.map((t) => ({ value: t.name, label: t.name }))" />
        </a-form-item>
        <a-form-item label="参数（JSON）">
          <a-textarea v-model:value="callArgs" :rows="4" />
        </a-form-item>
        <a-button type="primary" block :loading="calling" @click="submitCall">调用</a-button>
        <div v-if="callResult" style="margin-top: 12px">
          <div class="wg-muted" style="margin-bottom: 4px">返回结果</div>
          <a-typography-paragraph>
            <pre style="white-space: pre-wrap">{{ callResult }}</pre>
          </a-typography-paragraph>
        </div>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
code {
  font-size: 12px;
}
pre {
  margin: 0;
  font-size: 12px;
}

/* ---- 顶部统计 ---- */
.wg-stats {
  display: flex;
  gap: 20px;
  margin-top: 8px;
}
.wg-stat {
  display: flex;
  align-items: baseline;
  gap: 5px;
}
.wg-stat b {
  font-size: 18px;
  font-weight: 600;
  color: var(--wg-primary, #1677ff);
}
.wg-stat span {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
}

/* ---- 两种模式 ---- */
.wg-modes {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.wg-mode {
  padding: 12px 16px;
  border: 1px solid #f0f0f0;
  border-radius: 8px;
  background: #fafafa;
}
.wg-mode p {
  margin: 8px 0 6px;
  font-size: 13px;
}
.wg-mode-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.wg-mode-foot {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
}

/* ---- 空状态接入指引 ---- */
.wg-guide {
  max-width: 760px;
  margin: 0 auto;
  padding: 24px 0 8px;
}
.wg-guide-steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
}
.wg-guide-step {
  display: flex;
  gap: 10px;
}
.wg-guide-no {
  flex: none;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--wg-primary, #1677ff);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.wg-guide-title {
  font-weight: 600;
  margin-bottom: 2px;
}
.wg-guide-divider {
  margin: 24px 0 16px;
}
.wg-templates {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
.wg-template {
  padding: 12px 16px;
  border: 1px dashed #d9d9d9;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
}
.wg-template:hover {
  border-color: var(--wg-primary, #1677ff);
  background: #f6f9ff;
}
.wg-template-title {
  font-weight: 600;
  margin-bottom: 4px;
}
.wg-template-go {
  margin-top: 8px;
  font-size: 12px;
  color: var(--wg-primary, #1677ff);
}
.wg-guide-foot {
  margin-top: 16px;
  text-align: center;
}
</style>
