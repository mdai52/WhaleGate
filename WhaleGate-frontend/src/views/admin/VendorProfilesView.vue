<template>
  <div>
    <a-card :body-style="{ padding: '16px' }">
      <div class="vp-head">
        <div>
          <h3 class="vp-title">厂商适配档案</h3>
          <div class="wg-muted">
            各家「OpenAI 兼容协议」的差异（端点、鉴权、思维链字段名、usage 口径、错误外壳、视频/生图端点）
            都在这里维护。按上游地址自动匹配，新建渠道时无需额外配置。
          </div>
        </div>
        <div class="vp-actions">
          <a-button @click="reload">
            <ReloadOutlined />
            重载
          </a-button>
          <a-button type="primary" @click="openCreate">
            <PlusOutlined />
            新增厂商
          </a-button>
        </div>
      </div>

      <a-table
        :columns="columns"
        :data-source="rows"
        :loading="loading"
        row-key="id"
        size="small"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'enabled'">
            <a-tag :color="record.enabled ? 'green' : 'default'">
              {{ record.enabled ? '启用' : '停用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'builtin'">
            <a-tag v-if="record.builtin" color="blue">内置</a-tag>
            <span v-else class="wg-muted">自定义</span>
          </template>
          <template v-else-if="column.key === 'match_hosts'">
            <span class="vp-hosts">{{ record.match_hosts || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-button size="small" type="link" @click="openEdit(record)">编辑</a-button>
            <a-popconfirm
              title="删除后该厂商将按通用 OpenAI 形态处理，确定？"
              @confirm="remove(record.id)"
            >
              <a-button size="small" type="link" danger>删除</a-button>
            </a-popconfirm>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-drawer
      v-model:open="drawer"
      :title="editing ? `编辑 ${editing.code}` : '新增厂商'"
      width="560"
      @close="drawer = false"
    >
      <a-form layout="vertical" size="small">
        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item label="代码（唯一，与地址识别关联）">
              <a-input v-model:value="form.code" :disabled="!!editing" placeholder="如 deepseek" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="名称">
              <a-input v-model:value="form.config.name" placeholder="如 DeepSeek 深度求索" />
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item label="分组">
              <a-select v-model:value="form.config.group" :options="selectOf(options.groups)" />
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item label="排序（大者在前）">
              <a-input-number v-model:value="form.sort" style="width: 100%" />
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item label="状态">
              <a-switch v-model:checked="form.enabled" checked-children="启用" un-checked-children="停用" />
            </a-form-item>
          </a-col>
        </a-row>

        <a-form-item
          label="匹配主机"
          extra="上游地址里出现的关键词，逗号分隔；也支持端口号（如 11434，仅内网地址生效）"
        >
          <a-textarea v-model:value="form.match_hosts" :rows="2" placeholder="api.deepseek.com" />
        </a-form-item>
        <a-form-item label="模型名关键词" extra="地址识别不到时按模型名兜底，逗号分隔">
          <a-input v-model:value="modelHintsText" placeholder="deepseek,deepseek-chat" />
        </a-form-item>

        <a-divider orientation="left">端点与鉴权</a-divider>
        <a-row :gutter="12">
          <a-col :span="24">
            <a-form-item label="官方 OpenAI 兼容端点">
              <a-input v-model:value="form.config.base_url" placeholder="https://api.deepseek.com/v1" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="鉴权风格">
              <a-select v-model:value="form.config.auth_style" :options="selectOf(options.auth)" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="鉴权头名（raw 风格时必填）">
              <a-input v-model:value="form.config.auth_header" placeholder="api-key" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="对话路径">
              <a-input v-model:value="form.config.chat_path" placeholder="/chat/completions，支持 {model}" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="模型列表路径">
              <a-input v-model:value="form.config.models_path" placeholder="/models" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="版本前缀（已填则不再拼接）">
              <a-input v-model:value="form.config.base_suffix" placeholder="compatible-mode/v1" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="附加查询串">
              <a-input v-model:value="form.config.query" placeholder="api-version=2024-10-01-preview" />
            </a-form-item>
          </a-col>
        </a-row>

        <a-divider orientation="left">字段命名与响应</a-divider>
        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item label="思维链字段">
              <a-select
                v-model:value="form.config.reasoning"
                :options="selectOf(options.reasoning)"
                allow-clear
              />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="输出上限字段">
              <a-input v-model:value="form.config.max_tokens" placeholder="max_tokens" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="usage 口径">
              <a-select v-model:value="form.config.usage" :options="selectOf(options.usage)" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="响应体风格">
              <a-select v-model:value="form.config.response" :options="selectOf(options.response)" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="错误体风格">
              <a-select v-model:value="form.config.error" :options="selectOf(options.error)" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="思考开关参数名">
              <a-input v-model:value="form.config.thinking_param" placeholder="enable_thinking" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="支持流式 usage">
              <a-switch v-model:checked="form.config.stream_usage" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="视频端点风格">
              <a-select v-model:value="form.config.video" :options="selectOf(options.video)" allow-clear />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="生图端点风格">
              <a-select v-model:value="form.config.image" :options="selectOf(options.image)" allow-clear />
            </a-form-item>
          </a-col>
        </a-row>

        <a-form-item label="备注">
          <a-input v-model:value="form.config.note" placeholder="给管理员看的接入提示" />
        </a-form-item>
      </a-form>

      <template #footer>
        <div class="vp-footer">
          <a-button @click="drawer = false">取消</a-button>
          <a-button type="primary" :loading="saving" @click="submit">保存</a-button>
        </div>
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { vendorApi } from '@/api/vendor'
import type { VendorConfig, VendorOptions, VendorProfile } from '@/api/types'

const rows = ref<VendorProfile[]>([])
const loading = ref(false)
const saving = ref(false)
const drawer = ref(false)
const editing = ref<VendorProfile | null>(null)

const options = reactive<VendorOptions>({
  reasoning: [], usage: [], response: [], error: [], auth: [], video: [], image: [], groups: [],
})

interface VendorForm {
  code: string
  sort: number
  enabled: boolean
  match_hosts: string
  config: VendorConfig
}

function emptyForm(): VendorForm {
  return {
    code: '',
    sort: 50,
    enabled: true,
    match_hosts: '',
    config: {
      name: '', group: '其他', base_url: '', auth_style: 'bearer',
      chat_path: '/chat/completions', models_path: '/models',
      reasoning: 'reasoning_content', max_tokens: 'max_tokens',
      usage: 'openai', response: 'openai', error: 'openai',
      stream_usage: true, video: 'openai', image: 'openai',
    },
  }
}

const form = ref<VendorForm>(emptyForm())
const modelHintsText = ref('')

const columns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '代码', dataIndex: 'code', key: 'code', width: 130 },
  { title: '分组', dataIndex: 'group_name', key: 'group_name', width: 80 },
  { title: '匹配主机', dataIndex: 'match_hosts', key: 'match_hosts' },
  { title: '状态', key: 'enabled', width: 80 },
  { title: '来源', key: 'builtin', width: 90 },
  { title: '操作', key: 'action', width: 130 },
]

function selectOf(list: string[]) {
  return (list ?? []).map((v) => ({ label: v || '（空）', value: v }))
}

async function load() {
  loading.value = true
  try {
    rows.value = await vendorApi.list()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载失败')
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  try {
    Object.assign(options, await vendorApi.options())
  } catch {
    // 维度拿不到时下拉为空，不影响列表展示
  }
}

function openCreate() {
  editing.value = null
  form.value = emptyForm()
  modelHintsText.value = ''
  drawer.value = true
}

function openEdit(row: VendorProfile) {
  editing.value = row
  form.value = {
    code: row.code,
    sort: row.sort,
    enabled: row.enabled,
    match_hosts: row.match_hosts,
    config: { ...row.config },
  }
  modelHintsText.value = (row.config.model_hints ?? []).join(',')
  drawer.value = true
}

async function submit() {
  saving.value = true
  try {
    const cfg: VendorConfig = {
      ...form.value.config,
      model_hints: modelHintsText.value
        .split(/[,，\n]/)
        .map((s) => s.trim())
        .filter(Boolean),
    }
    const payload = { code: form.value.code, sort: form.value.sort, enabled: form.value.enabled, config: cfg }
    if (editing.value) {
      await vendorApi.update(editing.value.id, payload)
    } else {
      await vendorApi.create(payload)
    }
    message.success('已保存并生效')
    drawer.value = false
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove(id: number) {
  try {
    await vendorApi.remove(id)
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败')
  }
}

async function reload() {
  try {
    const res = await vendorApi.reload()
    message.success(`已重载，生效档案 ${res.count} 个`)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '重载失败')
  }
}

onMounted(async () => {
  await loadOptions()
  await load()
})
</script>

<style scoped>
.vp-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
}

.vp-title {
  margin: 0 0 4px;
  font-size: 16px;
}

.vp-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.vp-hosts {
  color: var(--wg-text-secondary, #6b7280);
  font-size: 12px;
  word-break: break-all;
}

.vp-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
