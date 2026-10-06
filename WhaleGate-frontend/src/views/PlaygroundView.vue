<script setup lang="ts">
/**
 * 参数调试：在写代码之前先确认「自定义参数会被如何分配」。
 *
 * 预览完全复用后端真实转发所用的参数分配逻辑，因此结论与线上一致；
 * 且不向上游发起请求，可以放心反复试。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { CopyOutlined, KeyOutlined, PlayCircleOutlined, ThunderboltOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { apiKeyApi } from '@/api/apikey'
import { playgroundApi } from '@/api/playground'
import type { ParamPreview, PlaygroundModel } from '@/api/playground'
import KeyValueEditor from '@/components/KeyValueEditor.vue'
import { parseScalarValue, parseValueRecord } from '@/utils/json'

const loading = ref(false)
const previewing = ref(false)
const models = ref<PlaygroundModel[]>([])
const model = ref('')
const prompt = ref('')
const customParams = ref<Record<string, string>>({})
const preview = ref<ParamPreview | null>(null)

/** 常用参数：勾选即参与本次预览 */
interface CommonParam {
  name: string
  type: 'number' | 'string' | 'json'
  hint: string
  placeholder: string
}

const COMMON_PARAMS: CommonParam[] = [
  { name: 'temperature', type: 'number', hint: '采样温度，越高越随机', placeholder: '0.7' },
  { name: 'top_p', type: 'number', hint: '核采样，通常与 temperature 二选一', placeholder: '0.9' },
  { name: 'top_k', type: 'number', hint: 'Gemini / Anthropic 有协议映射；OpenAI 渠道按自定义参数透传', placeholder: '40' },
  { name: 'max_tokens', type: 'number', hint: '最大生成 token 数', placeholder: '1024' },
  { name: 'seed', type: 'number', hint: '随机种子，便于结果复现', placeholder: '42' },
  { name: 'frequency_penalty', type: 'number', hint: '频率惩罚', placeholder: '0' },
  { name: 'presence_penalty', type: 'number', hint: '存在惩罚', placeholder: '0' },
  { name: 'repetition_penalty', type: 'number', hint: '重复惩罚', placeholder: '1.1' },
  { name: 'n', type: 'number', hint: '生成候选数量', placeholder: '1' },
  { name: 'stop', type: 'json', hint: '停止词，JSON 数组', placeholder: '["\\n\\n"]' },
  { name: 'response_format', type: 'json', hint: '结构化输出声明', placeholder: '{"type":"json_object"}' },
  { name: 'modalities', type: 'json', hint: '输出模态，生图场景用', placeholder: '["image","text"]' },
]

const enabled = reactive<Record<string, boolean>>({})
const values = reactive<Record<string, string>>({})

const currentModel = computed(() => models.value.find((m) => m.model === model.value))

/** 本次预览实际提交的参数：常用参数 + 自定义参数（后者覆盖前者）。 */
const finalParams = computed<Record<string, unknown>>(() => {
  const out: Record<string, unknown> = {}
  for (const param of COMMON_PARAMS) {
    if (!enabled[param.name]) {
      continue
    }
    const raw = values[param.name]
    if (raw === undefined || raw === '') {
      continue
    }
    out[param.name] = parseScalarValue(raw)
  }
  return { ...out, ...parseValueRecord(customParams.value) }
})

const paramCount = computed(() => Object.keys(finalParams.value).length)

async function loadModels() {
  loading.value = true
  try {
    models.value = await playgroundApi.models()
    if (!model.value && models.value.length) {
      model.value = models.value[0].model
    }
  } catch {
    message.error('加载模型列表失败')
  } finally {
    loading.value = false
  }
}

/**
 * 兼容后端可能返回 null 的集合字段。
 * Go 的 nil slice 会序列化成 null，直接取 .length 会在渲染期抛错并把整块结果打没，
 * 因此这里统一兜底成空数组。
 */
function normalize(data: ParamPreview): ParamPreview {
  return {
    ...data,
    supported: data.supported ?? [],
    excluded: data.excluded ?? [],
    applied: data.applied ?? [],
    dropped: data.dropped ?? [],
    unknown: data.unknown ?? [],
  }
}

async function doPreview() {
  if (!model.value) {
    message.warning('请先选择模型')
    return
  }
  previewing.value = true
  try {
    preview.value = normalize(
      await playgroundApi.preview({
        model: model.value,
        prompt: prompt.value,
        params: finalParams.value,
      }),
    )
  } catch (e) {
    message.error((e as Error).message || '预览失败')
    preview.value = null
  } finally {
    previewing.value = false
  }
}

function resetParams() {
  for (const param of COMMON_PARAMS) {
    enabled[param.name] = false
    values[param.name] = ''
  }
  customParams.value = {}
  preview.value = null
}

/**
 * 丢弃原因。默认透传下只有两类参数会被拦下：
 * 渠道显式 exclude，或「有协议映射但渠道能力集不含它」。
 * 能力表之外的自定义参数会原样转发给上游，不在这里出现。
 */
function dropReason(name: string): string {
  const data = preview.value
  if (!data) {
    return ''
  }
  if (data.excluded?.includes(name)) {
    return '渠道明确排除了该参数（管理员意图优先，透传也不放行）'
  }
  if (data.supported.includes(name)) {
    return '当前协议没有对应字段，且渠道能力集未声明支持'
  }
  return data.declared
    ? '渠道声明的能力中不包含该参数，且有协议字段映射'
    : '该协议默认不支持，可在渠道的「参数能力声明」中显式开启'
}

const payloadText = computed(() =>
  preview.value ? JSON.stringify(preview.value.payload, null, 2) : '',
)

async function copy(text: string, tip = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

// ---------------------------------------------------------------- 实测

const realKey = ref('')
const sending = ref(false)
const realResult = ref<{ status: number; applied: string; dropped: string; body: string } | null>(null)

/** 用真实密钥打一次网关，验证预览结论与线上一致（会计费）。 */
async function sendReal() {
  const key = realKey.value.trim()
  if (!key) {
    message.warning('请粘贴一个 sk- 开头的密钥')
    return
  }
  if (!model.value) {
    message.warning('请先选择模型')
    return
  }
  sending.value = true
  try {
    const res = await fetch(`${window.location.origin}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${key}` },
      body: JSON.stringify({
        model: model.value,
        messages: [{ role: 'user', content: prompt.value.trim() || '你好' }],
        ...finalParams.value,
      }),
    })
    realResult.value = {
      status: res.status,
      applied: res.headers.get('X-WG-Params-Applied') ?? '',
      dropped: res.headers.get('X-WG-Params-Dropped') ?? '',
      body: (await res.text()).slice(0, 4000),
    }
  } catch (e) {
    message.error(`请求失败：${(e as Error).message}`)
  } finally {
    sending.value = false
  }
}

/**
 * 把实测失败翻译成可操作的说明。
 * 401「凭证无效」是最容易误解的一个：它指的是密钥，和所选模型无关，
 * 而列表里展示的打码值形似完整密钥，最常被误复制。
 */
const realHint = computed(() => {
  const res = realResult.value
  if (!res || res.status < 400) {
    return ''
  }
  if (res.status === 401) {
    return (
      '「凭证无效」表示这串密钥在网关里查不到，与所选模型无关——' +
      '请求在鉴权阶段就被拦下，还没走到渠道。常见原因：' +
      '① 复制的是密钥列表里打码后的展示值（形如 sk-Ab3x...****，只用于辨认，不能调用）；' +
      '② 该密钥已被删除；③ 密钥来自其它网关。' +
      '密钥明文只在创建成功那一刻展示一次，丢失后无法找回，需要重新创建。'
    )
  }
  if (res.status === 403) {
    return '密钥或账号处于禁用状态，也可能来源 IP 不在该密钥的白名单内。'
  }
  if (res.status === 402) {
    return '额度不足，请在概览页查看余额或联系管理员充值。'
  }
  if (res.status === 429) {
    return '触发限流：可能是该密钥的 QPM / 并发上限，也可能是上游自身限流。'
  }
  if (res.status >= 500) {
    return '网关或上游异常，可在「调用历史」按 trace_id 查看详情。'
  }
  return ''
})

// ---------------------------------------------------------------- 临时调试密钥

const creatingKey = ref(false)

/** 手上没有可用明文时，直接建一个专用调试密钥并填入，用完可删除。 */
async function createTempKey() {
  creatingKey.value = true
  try {
    const res = await apiKeyApi.create({ name: '调试密钥（参数调试页生成）' })
    realKey.value = res.key
    message.success('已生成并填入调试密钥，用完可在「密钥管理」中删除')
  } catch (e) {
    message.error((e as Error).message || '创建调试密钥失败')
  } finally {
    creatingKey.value = false
  }
}

onMounted(loadModels)
</script>

<template>
  <div class="wg-page">
    <a-card title="参数调试">
      <template #extra>
        <a-button :loading="loading" @click="resetParams">重置</a-button>
      </template>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="先预览，再调用"
        description="预览不向上游发起请求、不计费，结论来自与真实转发完全相同的参数分配逻辑：能立刻看出哪些参数会写入协议字段、哪些会原样透传给上游、哪些被管理员拦下，以及最终请求体写成了什么。"
      />

      <a-row :gutter="[16, 16]">
        <a-col :xs="24" :lg="13">
          <a-form layout="vertical">
            <a-form-item label="模型">
              <a-select
                v-model:value="model"
                show-search
                :loading="loading"
                placeholder="选择要调试的模型"
                :options="models.map((m) => ({ value: m.model, label: m.model }))"
                @change="preview = null"
              />
              <div v-if="currentModel" class="wg-muted wg-model-meta">
                渠道 {{ currentModel.channel }}（{{ currentModel.channel_type }}）
                <template v-if="currentModel.upstream_model !== currentModel.model">
                  ｜上游模型 {{ currentModel.upstream_model }}
                </template>
              </div>
              <a-empty v-if="!loading && !models.length" description="暂无可用模型，请先在渠道管理中添加渠道" />
            </a-form-item>

            <a-form-item label="提示词（可选）">
              <a-textarea
                v-model:value="prompt"
                :rows="2"
                placeholder="仅用于让预览出的请求体更接近真实调用，不会真正发送"
              />
            </a-form-item>
          </a-form>

          <a-divider orientation="left" style="font-size: 13px">常用参数</a-divider>
          <div class="wg-common">
            <div v-for="param in COMMON_PARAMS" :key="param.name" class="wg-common-row">
              <a-checkbox v-model:checked="enabled[param.name]" />
              <code class="wg-common-name">{{ param.name }}</code>
              <a-input
                v-model:value="values[param.name]"
                :placeholder="param.placeholder"
                :disabled="!enabled[param.name]"
                allow-clear
                class="wg-common-input"
              />
              <span class="wg-muted wg-common-hint">{{ param.hint }}</span>
            </div>
          </div>

          <a-divider orientation="left" style="font-size: 13px">自定义参数</a-divider>
          <KeyValueEditor
            v-model="customParams"
            key-placeholder="参数名，如 logit_bias"
            value-placeholder="值，支持数字 / 布尔 / JSON"
            empty-hint="协议未标准化的参数也可以传，网关默认原样透传给上游，由上游决定是否接受。"
          />

          <a-space style="margin-top: 16px">
            <a-button type="primary" :loading="previewing" @click="doPreview">
              <PlayCircleOutlined />
              预览分配结果
            </a-button>
            <span class="wg-muted">共 {{ paramCount }} 个参数</span>
          </a-space>
        </a-col>

        <a-col :xs="24" :lg="11">
          <a-empty v-if="!preview" description="填写参数后点击「预览分配结果」" />

          <template v-else>
            <a-descriptions bordered size="small" :column="1">
              <a-descriptions-item label="渠道">
                {{ preview.channel.name }}（{{ preview.channel.type }}）
              </a-descriptions-item>
              <a-descriptions-item label="上游模型">{{ preview.upstream_model }}</a-descriptions-item>
              <a-descriptions-item label="能力来源">
                <a-tag :color="preview.declared ? 'green' : 'default'">
                  {{ preview.declared ? '渠道显式声明' : '协议默认支持集' }}
                </a-tag>
              </a-descriptions-item>
            </a-descriptions>

            <div class="wg-result-block">
              <div class="wg-result-title">
                已应用
                <a-tag color="green">{{ preview.applied.length }}</a-tag>
              </div>
              <a-space v-if="preview.applied.length" wrap>
                <a-tag v-for="name in preview.applied" :key="name" color="green">{{ name }}</a-tag>
              </a-space>
              <span v-else class="wg-muted">无</span>
            </div>

            <div class="wg-result-block">
              <div class="wg-result-title">
                被丢弃
                <a-tag :color="preview.dropped.length ? 'red' : 'default'">{{ preview.dropped.length }}</a-tag>
              </div>
              <template v-if="preview.dropped.length">
                <div v-for="name in preview.dropped" :key="name" class="wg-drop-item">
                  <a-tag color="red">{{ name }}</a-tag>
                  <span class="wg-muted">{{ dropReason(name) }}</span>
                </div>
              </template>
              <span v-else class="wg-muted">无</span>
            </div>

            <div v-if="preview.unknown?.length" class="wg-result-block">
              <div class="wg-result-title">未标准化参数</div>
              <a-space wrap>
                <a-tag v-for="name in preview.unknown" :key="name" color="orange">{{ name }}</a-tag>
              </a-space>
              <div class="wg-muted wg-tip">
                这些名字不在网关的标准参数表内，默认会<b>原样透传</b>给上游（不改名、不映射），
                由上游决定是否接受；若上游不支持，它的报错会原样返回给调用方。
                拼写错误请对照下方支持列表修正；能否生效仍以「已应用 / 被丢弃」为准。
              </div>
            </div>

            <div v-if="preview.injected && Object.keys(preview.injected).length" class="wg-result-block">
              <div class="wg-result-title">渠道强制注入</div>
              <pre class="wg-json">{{ JSON.stringify(preview.injected, null, 2) }}</pre>
              <div class="wg-muted wg-tip">优先级最高，会覆盖上面分配的结果。</div>
            </div>

            <div class="wg-result-block">
              <div class="wg-result-title">
                最终上游请求体
                <a-button type="link" size="small" @click="copy(payloadText)">
                  <CopyOutlined />
                  复制
                </a-button>
              </div>
              <pre class="wg-json">{{ payloadText }}</pre>
            </div>

            <a-collapse ghost style="margin-top: 8px">
              <a-collapse-panel key="supported" :header="`该渠道支持的参数（${preview.supported.length}）`">
                <a-space wrap>
                  <a-tag v-for="name in preview.supported" :key="name">{{ name }}</a-tag>
                </a-space>
                <div v-if="preview.excluded?.length" class="wg-muted wg-tip">
                  已排除：{{ preview.excluded.join('、') }}
                </div>
                <div v-if="preview.defaults && Object.keys(preview.defaults).length" class="wg-muted wg-tip">
                  默认值：{{ JSON.stringify(preview.defaults) }}
                </div>
              </a-collapse-panel>
            </a-collapse>
          </template>
        </a-col>
      </a-row>
    </a-card>

    <a-card title="实测一次" style="margin-top: 16px">
      <a-alert
        type="warning"
        show-icon
        style="margin-bottom: 12px"
        message="实测会真实调用上游并消耗额度"
        description="用你自己的密钥打一次网关，响应头里的 X-WG-Params-Applied / X-WG-Params-Dropped 应与上面的预览一致。这里必须填创建时展示的完整明文，密钥列表里的打码值（形如 sk-Ab3x...****）无法调用。"
      />
      <a-space wrap style="margin-bottom: 12px">
        <a-input-password
          v-model:value="realKey"
          addon-before="API Key"
          placeholder="sk-...（创建时展示的完整明文）"
          style="width: 360px"
        />
        <a-button type="primary" :loading="sending" @click="sendReal">
          <ThunderboltOutlined />
          发送真实请求
        </a-button>
        <a-button :loading="creatingKey" @click="createTempKey">
          <KeyOutlined />
          生成调试密钥
        </a-button>
      </a-space>

      <template v-if="realResult">
        <a-alert
          v-if="realHint"
          type="error"
          show-icon
          :message="`HTTP ${realResult.status}`"
          :description="realHint"
          style="margin-bottom: 12px"
        />
        <a-descriptions bordered size="small" :column="1">
          <a-descriptions-item label="HTTP 状态">{{ realResult.status }}</a-descriptions-item>
          <a-descriptions-item label="X-WG-Params-Applied">
            <code>{{ realResult.applied || '（无）' }}</code>
          </a-descriptions-item>
          <a-descriptions-item label="X-WG-Params-Dropped">
            <code>{{ realResult.dropped || '（无）' }}</code>
          </a-descriptions-item>
        </a-descriptions>
        <pre class="wg-json" style="margin-top: 12px">{{ realResult.body }}</pre>
      </template>
    </a-card>
  </div>
</template>

<style scoped>
.wg-model-meta {
  margin-top: 4px;
  font-size: 12px;
}

.wg-common-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.wg-common-name {
  flex: none;
  width: 150px;
  font-size: 12px;
}

.wg-common-input {
  flex: none;
  width: 180px;
}

.wg-common-hint {
  flex: 1;
  min-width: 0;
  font-size: 12px;
}

.wg-result-block {
  margin-top: 14px;
}

.wg-result-title {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-bottom: 6px;
  font-weight: 600;
}

.wg-drop-item {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 4px;
}

.wg-tip {
  margin-top: 4px;
  font-size: 12px;
}

.wg-json {
  max-height: 260px;
  margin: 0;
  padding: 10px 12px;
  overflow: auto;
  background: #f5f7fa;
  border-radius: 6px;
  font-family: 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
