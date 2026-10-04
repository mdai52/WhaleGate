<script setup lang="ts">
/**
 * AI 客户端接入指引：把网关地址、密钥、模型三个值按各客户端的填法分别给出，
 * 可逐项复制或整份下载，省去对照文档手抄。
 *
 * 只收录确认兼容 OpenAI 协议的客户端；GUI 客户端的菜单位置可能随版本变化，
 * 因此给出「要点」而非死记路径。
 */
import { computed } from 'vue'
import { CopyOutlined, DownloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'

const props = defineProps<{
  /** 网关根地址，如 https://gw.example.com */
  baseURL: string
  /** 用户填写的 API Key */
  apiKey: string
  /** 用户填写的模型名 */
  model: string
}>()

/** 各客户端需要填的值 */
interface ClientPreset {
  key: string
  name: string
  desc: string
  /** 关键填写要点 */
  tips: string[]
  /** 该客户端需要的地址形态：多数客户端要求带 /v1 */
  endpoint?: 'with-v1' | 'root'
  /** 可下载的配置文件 */
  download?: { filename: string; kind: 'openai-json' | 'openai-env' }
}

const PRESETS: ClientPreset[] = [
  {
    key: 'cherry-studio',
    name: 'Cherry Studio',
    desc: '桌面客户端，支持多提供商与知识库',
    tips: [
      '设置 → 模型服务 → 添加提供商，类型选「OpenAI」',
      'API 地址填下方地址，API 密钥填你的 sk- 密钥',
      '在「模型」里点添加，模型名填下方模型名',
    ],
    endpoint: 'with-v1',
    download: { filename: 'whalegate-openai.json', kind: 'openai-json' },
  },
  {
    key: 'chatbox',
    name: 'ChatBox',
    desc: '桌面与移动端，配置简单',
    tips: [
      '设置 → 模型 → 添加自定义提供方',
      'API 模式选「OpenAI API 兼容」',
      'API 域名填下方地址，模型名填下方模型名',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'nextchat',
    name: 'NextChat',
    desc: '网页版 / 桌面版聊天客户端',
    tips: [
      '设置 → 自定义接口',
      '接口地址填下方地址；若客户端会自动补 /v1，则去掉末尾的 /v1',
      '填入 API Key，并把下方模型名加入自定义模型',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'lobechat',
    name: 'LobeChat',
    desc: '网页版聊天客户端与服务端部署',
    tips: [
      '设置 → 语言模型 → OpenAI',
      '填入 API Key，代理地址填下方地址',
      '模型列表会自动拉取；为空时手动添加下方模型名',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'open-webui',
    name: 'Open WebUI',
    desc: '自托管网页客户端',
    tips: [
      '管理员设置 → 连接 → OpenAI API',
      'API 地址填下方地址，密钥填你的 sk- 密钥',
      '保存后模型下拉会自动出现可用模型',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'cursor',
    name: 'Cursor',
    desc: 'AI 代码编辑器',
    tips: [
      '设置 → Models → OpenAI API Key',
      '打开「Override OpenAI Base URL」，填下方地址',
      '点 Verify 校验，自定义模型名填下方模型名',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'cline',
    name: 'Cline / Roo Code',
    desc: 'VS Code 编码助手插件',
    tips: [
      '设置 → API Provider 选「OpenAI Compatible」',
      'Base URL 填下方地址，API Key 填你的 sk- 密钥',
      'Model ID 填下方模型名',
    ],
    endpoint: 'with-v1',
  },
  {
    key: 'continue',
    name: 'Continue',
    desc: 'VS Code / JetBrains 补全与对话插件',
    tips: [
      '编辑 config.json 的 models 数组，新增一条 provider 为 openai 的配置',
      'apiBase 填下方地址，apiKey 填你的 sk- 密钥',
      'model 填下方模型名',
    ],
    endpoint: 'with-v1',
  },
]

const openAIBase = computed(() => `${props.baseURL}/v1`)

function endpointOf(preset: ClientPreset): string {
  return preset.endpoint === 'root' ? props.baseURL : openAIBase.value
}

/** 通用 OpenAI 兼容配置（部分客户端支持导入）。 */
function openAIJSON(): string {
  return JSON.stringify(
    {
      provider: 'openai',
      name: 'WhaleGate',
      base_url: openAIBase.value,
      api_key: props.apiKey,
      model: props.model,
    },
    null,
    2,
  )
}

function openAIEnv(): string {
  return `OPENAI_BASE_URL=${openAIBase.value}
OPENAI_API_KEY=${props.apiKey}
OPENAI_MODEL=${props.model}`
}

async function copy(text: string, tip = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

function download(filename: string, kind: 'openai-json' | 'openai-env') {
  const content = kind === 'openai-json' ? openAIJSON() : openAIEnv()
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
  message.success('配置文件已下载')
}
</script>

<template>
  <div class="wg-clients">
    <a-alert
      type="info"
      show-icon
      style="margin-bottom: 16px"
      message="先填入你的密钥，下面的地址与密钥会同步更新"
      description="以下客户端均只需「网关地址 + API Key + 模型名」三项。菜单路径可能随客户端版本变化，以你安装的版本为准。"
    />

    <a-descriptions bordered size="small" :column="1" style="margin-bottom: 16px">
      <a-descriptions-item label="OpenAI 兼容地址">
        <code>{{ openAIBase }}</code>
        <a-button type="link" size="small" @click="copy(openAIBase)">
          <CopyOutlined />
        </a-button>
      </a-descriptions-item>
      <a-descriptions-item label="API Key">
        <code>{{ apiKey || '（未填写）' }}</code>
        <a-button type="link" size="small" :disabled="!apiKey" @click="copy(apiKey)">
          <CopyOutlined />
        </a-button>
      </a-descriptions-item>
      <a-descriptions-item label="模型名">
        <code>{{ model || '（未填写）' }}</code>
        <a-button type="link" size="small" :disabled="!model" @click="copy(model)">
          <CopyOutlined />
        </a-button>
      </a-descriptions-item>
    </a-descriptions>

    <a-row :gutter="[16, 16]">
      <a-col v-for="preset in PRESETS" :key="preset.key" :xs="24" :md="12">
        <a-card size="small" :title="preset.name" class="wg-client-card">
          <template #extra>
            <a-tag color="blue">OpenAI 兼容</a-tag>
          </template>
          <div class="wg-muted" style="margin-bottom: 8px">{{ preset.desc }}</div>
          <ol class="wg-client-tips">
            <li v-for="(tip, index) in preset.tips" :key="index">{{ tip }}</li>
          </ol>
          <a-space wrap size="small">
            <a-button size="small" @click="copy(endpointOf(preset), '地址已复制')">
              <CopyOutlined />
              复制地址
            </a-button>
            <a-button
              v-if="preset.download"
              size="small"
              @click="download(preset.download.filename, preset.download.kind)"
            >
              <DownloadOutlined />
              下载配置
            </a-button>
          </a-space>
        </a-card>
      </a-col>
    </a-row>

    <a-alert
      type="warning"
      show-icon
      style="margin-top: 16px"
      message="Claude Code / CCSwitch 暂时无法直接接入"
      description="这两个工具要求服务端提供 Anthropic Messages API（/v1/messages）。本网关目前支持把 Anthropic 作为上游渠道，但尚未提供 Anthropic 原生入站入口，因此无法直接把它们指向本网关。"
    />
  </div>
</template>

<style scoped>
.wg-client-card {
  height: 100%;
}

.wg-client-tips {
  margin: 0 0 12px;
  padding-left: 18px;
  color: rgba(0, 0, 0, 0.65);
  font-size: 13px;
  line-height: 1.8;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
