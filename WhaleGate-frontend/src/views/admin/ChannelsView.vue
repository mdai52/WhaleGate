<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { DeleteOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { channelApi } from '@/api/channel'
import { useIsMobile } from '@/composables/useIsMobile'
import { parseJSON } from '@/utils/json'
import AliasEditor from '@/components/AliasEditor.vue'
import KeyValueEditor from '@/components/KeyValueEditor.vue'
import ParamSchemaEditor from '@/components/ParamSchemaEditor.vue'
import type {
  Channel,
  ChannelInput,
  ChannelType,
  DetectResult,
  DetectedModel,
  ModelCatalog,
} from '@/api/types'

const isMobile = useIsMobile()

const loading = ref(false)
const saving = ref(false)
const items = ref<Channel[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })

const drawerOpen = ref(false)
const editingId = ref<number | null>(null)

const form = reactive<ChannelInput & {
  api_key?: string
}>({
  name: '',
  type: 'openai',
  base_url: '',
  api_key: '',
  models: [],
  weight: 1,
  priority: 0,
  status: 1,
  timeout_seconds: 0,
  max_retries: 0,
})

// ---------------------------------------------------------------- 自动探测
const detecting = ref(false)
const detectResult = ref<DetectResult | null>(null)
const detectedModels = ref<DetectedModel[]>([])
const catalogModels = ref<ModelCatalog[]>([])
const modelKeyword = ref('')
const modelCapability = ref<string>('chat')

const capabilityMeta: Record<string, { text: string; color: string }> = {
  chat: { text: '对话', color: 'blue' },
  vision: { text: '视觉', color: 'cyan' },
  tool: { text: '工具', color: 'purple' },
  image: { text: '生图', color: 'magenta' },
  embedding: { text: '向量', color: 'geekblue' },
  audio: { text: '语音', color: 'orange' },
  reasoning: { text: '推理', color: 'red' },
}

async function runDetect() {
  if (!form.base_url.trim()) {
    message.warning('请先填写上游地址')
    return
  }
  if (!form.api_key) {
    message.warning('请先填写上游密钥')
    return
  }
  detecting.value = true
  try {
    const res = await channelApi.detect({
      base_url: form.base_url.trim(),
      api_key: form.api_key,
      type: form.type,
    })
    detectResult.value = res
    detectedModels.value = res.models
    // 自动回填：类型、名称（仅新建且未填时）、默认选中对话类模型
    form.type = res.suggested_type
    if (!isEdit.value && !form.name.trim()) {
      form.name = res.suggested_name
    }
    const chatModels = res.models
      .filter((m) => (m.capabilities ?? []).includes('chat'))
      .map((m) => m.id)
    form.models = chatModels.length ? chatModels : res.models.map((m) => m.id)
    message.success(`探测成功：${res.detected_type} 协议，发现 ${res.model_count} 个模型`)
  } catch (error) {
    detectResult.value = null
    message.error(error instanceof Error ? error.message : '自动探测失败，请检查地址与密钥')
  } finally {
    detecting.value = false
  }
}

async function loadCatalog() {
  try {
    const res = await channelApi.catalog({ page_size: 500 })
    catalogModels.value = res.items
  } catch {
    catalogModels.value = []
  }
}

/** 模型下拉选项：优先用探测结果，否则用全局目录 */
const modelOptions = computed(() => {
  const source = detectedModels.value.length
    ? detectedModels.value.map((m) => ({
        value: m.id,
        label: m.display_name && m.display_name !== m.id ? `${m.id}（${m.display_name}）` : m.id,
        caps: m.capabilities ?? [],
      }))
    : catalogModels.value.map((m) => ({
        value: m.model_id,
        label: m.display_name && m.display_name !== m.model_id
          ? `${m.model_id}（${m.display_name}）`
          : m.model_id,
        caps: parseJSON<string[]>(m.capabilities, []).value,
      }))
  const kw = modelKeyword.value.trim().toLowerCase()
  return source.filter(
    (item) =>
      (!kw || item.value.toLowerCase().includes(kw)) &&
      (!modelCapability.value || item.caps.includes(modelCapability.value)),
  )
})

function selectAllVisible() {
  const ids = modelOptions.value.map((i) => i.value)
  const merged = new Set([...(form.models ?? []), ...ids])
  form.models = Array.from(merged)
}

function clearModels() {
  form.models = []
}

// 高级配置：可视化编辑（无需手写 JSON）
const advanced = reactive({
  mapping: {} as Record<string, string>,
  aliases: {} as Record<string, { model?: string; override?: Record<string, unknown> }>,
  override: {} as Record<string, string>,
  schema: {} as { supported?: string[]; exclude?: string[]; defaults?: Record<string, unknown> },
})

/** 别名可引用的上游模型：登记模型 + 探测发现的模型 */
const aliasModelOptions = computed(() => {
  const set = new Set<string>(form.models ?? [])
  for (const m of detectedModels.value) {
    set.add(m.id)
  }
  return Array.from(set)
})

function resetAdvanced() {
  advanced.mapping = {}
  advanced.aliases = {}
  advanced.override = {}
  advanced.schema = {}
}

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '类型', dataIndex: 'type', key: 'type' },
  { title: '上游地址', dataIndex: 'base_url', key: 'base_url' },
  { title: '模型', dataIndex: 'models', key: 'models' },
  { title: '权重/优先级', key: 'route' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '失败', dataIndex: 'fail_count', key: 'fail_count' },
  { title: '操作', key: 'action', fixed: 'right' as const },
]

const statusMeta: Record<number, { text: string; color: string }> = {
  1: { text: '正常', color: 'green' },
  2: { text: '已禁用', color: 'default' },
  3: { text: '已熔断', color: 'red' },
}

function modelList(raw: string): string[] {
  const result = parseJSON<string[]>(raw, [])
  return result.ok && Array.isArray(result.value) ? result.value : []
}

const isEdit = computed(() => editingId.value !== null)

async function load() {
  loading.value = true
  try {
    const res = await channelApi.list({ page: pagination.current, page_size: pagination.pageSize })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '',
    type: 'openai' as ChannelType,
    base_url: '',
    api_key: '',
    models: [],
    weight: 1,
    priority: 0,
    status: 1,
    timeout_seconds: 0,
    max_retries: 0,
  })
  resetAdvanced()
  detectResult.value = null
  detectedModels.value = []
  modelKeyword.value = ''
  drawerOpen.value = true
  void loadCatalog()
}

function openEdit(row: Channel) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name,
    type: row.type,
    base_url: row.base_url,
    api_key: '',
    models: modelList(row.models),
    weight: row.weight,
    priority: row.priority,
    status: row.status,
    timeout_seconds: row.timeout_seconds,
    max_retries: row.max_retries,
  })
  Object.assign(advanced, {
    mapping: parseJSON<Record<string, string>>(row.model_mapping, {}).value,
    aliases: parseJSON<Record<string, { model?: string; override?: Record<string, unknown> }>>(row.model_alias, {}).value,
    override: parseJSON<Record<string, string>>(row.request_override, {}).value,
    schema: parseJSON<{ supported?: string[]; exclude?: string[]; defaults?: Record<string, unknown> }>(
      row.param_schema,
      {},
    ).value,
  })
  drawerOpen.value = true
}

function buildPayload(): ChannelInput | null {
  const payload: ChannelInput = {
    name: form.name.trim(),
    type: form.type,
    base_url: form.base_url.trim(),
    weight: form.weight,
    priority: form.priority,
    status: form.status,
    timeout_seconds: form.timeout_seconds,
    max_retries: form.max_retries,
  }
  if (form.api_key) {
    payload.api_key = form.api_key
  }
  if (form.models?.length) {
    payload.models = form.models
  }

  // 自动探测元数据：随渠道一起保存
  if (detectResult.value) {
    payload.capabilities = detectResult.value.capabilities
    payload.context_window = detectResult.value.context_window
    payload.max_output = detectResult.value.max_output
  }

  // 高级配置：可视化编辑器输出的结构化对象直接序列化，无需用户写 JSON
  if (Object.keys(advanced.mapping).length) {
    payload.model_mapping = advanced.mapping
  }
  if (Object.keys(advanced.aliases).length) {
    payload.model_alias = advanced.aliases as ChannelInput['model_alias']
  }
  if (Object.keys(advanced.override).length) {
    payload.request_override = parseValueRecord(advanced.override)
  }
  if (Object.keys(advanced.schema).length) {
    payload.param_schema = advanced.schema as ChannelInput['param_schema']
  }
  return payload
}

/** 把 KV 编辑器中的字符串值解析为数字 / 布尔 / JSON */
function parseValueRecord(record: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, raw] of Object.entries(record)) {
    const t = raw.trim()
    if (t === 'true') {
      out[key] = true
    } else if (t === 'false') {
      out[key] = false
    } else if (t !== '' && !Number.isNaN(Number(t))) {
      out[key] = Number(t)
    } else if (t.startsWith('{') || t.startsWith('[')) {
      try {
        out[key] = JSON.parse(t)
      } catch {
        out[key] = raw
      }
    } else {
      out[key] = raw
    }
  }
  return out
}

async function submit() {
  if (!form.name.trim()) {
    message.warning('请填写渠道名称')
    return
  }
  if (!form.base_url.trim()) {
    message.warning('请填写上游地址')
    return
  }
  const payload = buildPayload()
  if (!payload) {
    return
  }

  saving.value = true
  try {
    if (isEdit.value && editingId.value) {
      await channelApi.update(editingId.value, payload)
      message.success('渠道已更新')
    } else {
      await channelApi.create(payload)
      message.success('渠道已创建')
    }
    drawerOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function testChannel(row: Channel) {
  try {
    const res = await channelApi.test(row.id)
    message.success(`连通正常，HTTP ${res.status}，延迟 ${res.latency_ms}ms`)
    await load()
  } catch {
    message.error('连通性测试失败，请检查地址与密钥')
  }
}

function removeChannel(row: Channel) {
  Modal.confirm({
    title: '确认删除该渠道？',
    content: `删除后「${row.name}」不再参与路由。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await channelApi.remove(row.id)
      message.success('已删除')
      await load()
    },
  })
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <h2 style="margin: 0">渠道管理</h2>
        <a-button type="primary" @click="openCreate">
          <PlusOutlined />
          新建渠道
        </a-button>
      </div>

      <a-table
        :columns="columns"
        :data-source="items"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 1080 }"
        :pagination="{
          current: pagination.current,
          pageSize: pagination.pageSize,
          total: pagination.total,
          showSizeChanger: true,
          showTotal: (total: number) => `共 ${total} 条`,
        }"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'type'">
            <a-tag :color="(record as Channel).type === 'gemini' ? 'purple' : 'blue'">
              {{ (record as Channel).type }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'base_url'">
            <span class="wg-ellipsis">{{ (record as Channel).base_url }}</span>
          </template>
          <template v-else-if="column.key === 'models'">
            <template v-if="modelList((record as Channel).models).length">
              <a-tag v-for="m in modelList((record as Channel).models).slice(0, 2)" :key="m">{{ m }}</a-tag>
              <span v-if="modelList((record as Channel).models).length > 2" class="wg-muted">
                +{{ modelList((record as Channel).models).length - 2 }}
              </span>
            </template>
            <span v-else class="wg-muted">通配</span>
          </template>
          <template v-else-if="column.key === 'route'">
            {{ (record as Channel).weight }} / {{ (record as Channel).priority }}
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="statusMeta[(record as Channel).status].color">
              {{ statusMeta[(record as Channel).status].text }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="openEdit(record as Channel)">编辑</a-button>
              <a-button size="small" @click="testChannel(record as Channel)">
                <ThunderboltOutlined />
                测试
              </a-button>
              <a-button size="small" danger type="text" @click="removeChannel(record as Channel)">
                <DeleteOutlined />
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-drawer
      v-model:open="drawerOpen"
      :title="isEdit ? '编辑渠道' : '新建渠道'"
      :width="isMobile ? '100%' : 720"
      placement="right"
    >
      <a-form layout="vertical">
        <a-row :gutter="16">
          <a-col :xs="24" :md="12">
            <a-form-item label="渠道名称" required>
              <a-input v-model:value="form.name" placeholder="例如：OpenAI 主线路" allow-clear />
            </a-form-item>
          </a-col>
          <a-col :xs="24" :md="12">
            <a-form-item label="协议类型" required>
              <a-select v-model:value="form.type">
                <a-select-option value="openai">OpenAI 兼容</a-select-option>
                <a-select-option value="gemini">Gemini 原生</a-select-option>
              </a-select>
            </a-form-item>
          </a-col>
        </a-row>

        <a-form-item label="上游地址" required>
          <a-input
            v-model:value="form.base_url"
            placeholder="openai: https://api.openai.com/v1 ｜ gemini: https://generativelanguage.googleapis.com/v1beta"
            allow-clear
          />
        </a-form-item>

        <a-form-item>
          <template #label>
            上游密钥
            <span v-if="isEdit" class="wg-muted">（留空表示不修改）</span>
          </template>
          <a-input-password v-model:value="form.api_key" placeholder="sk-... 或 AIza..." allow-clear />
        </a-form-item>

        <a-form-item>
          <a-space>
            <a-button :loading="detecting" @click="runDetect">
              <ThunderboltOutlined />
              自动探测模型
            </a-button>
            <span class="wg-muted">填写地址与密钥后点击，自动识别协议类型、模型列表与能力</span>
          </a-space>
        </a-form-item>

        <a-alert
          v-if="detectResult"
          :type="detectResult.warning ? 'warning' : 'success'"
          show-icon
          style="margin-bottom: 16px"
        >
          <template #message>
            探测到 {{ detectResult.detected_type }} 协议 · {{ detectResult.model_count }} 个模型 ·
            {{ detectResult.latency_ms }}ms
          </template>
          <template #description>
            <div style="margin-bottom: 6px">
              <a-tag v-for="cap in detectResult.capabilities" :key="cap" :color="capabilityMeta[cap]?.color">
                {{ capabilityMeta[cap]?.text ?? cap }}
              </a-tag>
            </div>
            <div class="wg-muted">
              命中端点：{{ detectResult.endpoint }}
              <template v-if="detectResult.context_window">
                ｜ 最大上下文 {{ (detectResult.context_window / 1000).toFixed(0) }}K
              </template>
              <template v-if="detectResult.max_output">
                ｜ 最大输出 {{ detectResult.max_output }}
              </template>
            </div>
            <div v-if="detectResult.warning" style="margin-top: 6px">
              {{ detectResult.warning }}
            </div>
          </template>
        </a-alert>

        <a-form-item label="支持模型">
          <template #label>
            支持模型
            <span class="wg-muted">（留空表示通配，允许任意模型）</span>
          </template>
          <a-space direction="vertical" style="width: 100%">
            <a-space wrap>
              <a-input-search
                v-model:value="modelKeyword"
                placeholder="搜索模型名"
                allow-clear
                style="width: 200px"
              />
              <a-select v-model:value="modelCapability" style="width: 120px">
                <a-select-option value="">全部能力</a-select-option>
                <a-select-option value="chat">对话</a-select-option>
                <a-select-option value="vision">视觉</a-select-option>
                <a-select-option value="tool">工具</a-select-option>
                <a-select-option value="image">生图</a-select-option>
                <a-select-option value="embedding">向量</a-select-option>
                <a-select-option value="reasoning">推理</a-select-option>
              </a-select>
              <a-button size="small" @click="selectAllVisible">全选当前筛选</a-button>
              <a-button size="small" @click="clearModels">清空</a-button>
              <span class="wg-muted">已选 {{ form.models?.length ?? 0 }} 个</span>
            </a-space>
            <a-select
              v-model:value="form.models"
              mode="multiple"
              show-search
              allow-clear
              :filter-option="false"
              :options="modelOptions"
              placeholder="点击「自动探测模型」获取列表，或手动输入后回车"
              :token-separators="[',']"
              :max-tag-count="8"
              style="width: 100%"
            />
          </a-space>
        </a-form-item>

        <a-row :gutter="16">
          <a-col :xs="12" :md="6">
            <a-form-item label="权重">
              <a-input-number v-model:value="form.weight" :min="1" style="width: 100%" />
            </a-form-item>
          </a-col>
          <a-col :xs="12" :md="6">
            <a-form-item label="优先级">
              <a-input-number v-model:value="form.priority" :min="0" style="width: 100%" />
            </a-form-item>
          </a-col>
          <a-col :xs="12" :md="6">
            <a-form-item label="超时(秒)">
              <a-input-number v-model:value="form.timeout_seconds" :min="0" style="width: 100%" />
            </a-form-item>
          </a-col>
          <a-col :xs="12" :md="6">
            <a-form-item label="重试次数">
              <a-input-number v-model:value="form.max_retries" :min="0" style="width: 100%" />
            </a-form-item>
          </a-col>
        </a-row>

        <a-form-item label="状态">
          <a-select v-model:value="form.status">
            <a-select-option :value="1">正常</a-select-option>
            <a-select-option :value="2">人工禁用</a-select-option>
          </a-select>
        </a-form-item>

        <a-divider orientation="left">高级配置</a-divider>

        <a-form-item>
          <template #label>
            模型名映射
            <span class="wg-muted">（对外名 → 上游真实名，用户请求对外名时自动改写）</span>
          </template>
          <KeyValueEditor
            v-model="advanced.mapping"
            key-placeholder="对外模型名，如 gpt-4o"
            value-placeholder="上游模型名，如 gpt-4o-2024"
          />
        </a-form-item>

        <a-form-item>
          <template #label>
            模型别名
            <span class="wg-muted">（把一个上游模型 fork 成多个对外模型名，可各自注入参数，如把文本模型包装成不同分辨率的生图模型）</span>
          </template>
          <AliasEditor v-model="advanced.aliases" :model-options="aliasModelOptions" />
        </a-form-item>

        <a-form-item>
          <template #label>
            请求参数注入
            <span class="wg-muted">（强制合并到发往上游的请求体，对所有经过该渠道的请求生效）</span>
          </template>
          <KeyValueEditor
            v-model="advanced.override"
            key-placeholder="请求字段名，如 temperature"
            value-placeholder="值，支持数字 / 布尔 / JSON"
          />
        </a-form-item>

        <a-form-item>
          <template #label>
            参数能力声明
            <span class="wg-muted">（声明上游支持的统一参数；不声明则按协议默认集，用户传不支持的参数会被自动丢弃）</span>
          </template>
          <ParamSchemaEditor v-model="advanced.schema" />
        </a-form-item>
      </a-form>

      <template #footer>
        <div class="wg-drawer-footer">
          <a-button @click="drawerOpen = false">取消</a-button>
          <a-button type="primary" :loading="saving" @click="submit">保存</a-button>
        </div>
      </template>
    </a-drawer>
  </div>
</template>

<style scoped>
.wg-drawer-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.wg-ellipsis {
  display: inline-block;
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}
</style>
