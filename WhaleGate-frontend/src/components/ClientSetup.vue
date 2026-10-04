<script setup lang="ts">
/**
 * AI 客户端接入指引：把网关地址、密钥、模型按各客户端的填法分别给出，
 * 可逐项复制或整份下载，省去对照文档手抄。
 *
 * 客户端列表与配置生成复用 utils/clients，与密钥页的「一键导入」同源，
 * 避免两处各自维护而逐渐不一致。
 */
import { computed } from 'vue'
import { CopyOutlined, DownloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { CLIENT_PRESETS, endpointFor, snippetFor, type ClientPreset } from '@/utils/clients'

const props = defineProps<{
  /** 网关根地址，如 https://gw.example.com */
  baseURL: string
  /** 用户填写的 API Key */
  apiKey: string
  /** 用户填写的模型名 */
  model: string
}>()

const ctx = computed(() => ({
  origin: props.baseURL,
  apiKey: props.apiKey,
  model: props.model,
}))

const openAIBase = computed(() => `${props.baseURL}/v1`)

function addressOf(preset: ClientPreset) {
  return endpointFor(preset, props.baseURL)
}

function configOf(preset: ClientPreset) {
  return snippetFor(preset, ctx.value)
}

async function copy(text: string, tip = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

function download(preset: ClientPreset) {
  const blob = new Blob([configOf(preset)], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = preset.filename ?? `${preset.key}.txt`
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
      description="所有客户端都只需「网关地址 + 密钥（+ 模型名）」三项。菜单路径可能随客户端版本变化，以你安装的版本为准。"
    />

    <a-descriptions bordered size="small" :column="1" style="margin-bottom: 16px">
      <a-descriptions-item label="OpenAI 兼容地址">
        <code>{{ openAIBase }}</code>
        <a-button type="link" size="small" @click="copy(openAIBase)">
          <CopyOutlined />
        </a-button>
      </a-descriptions-item>
      <a-descriptions-item label="Anthropic 地址（Claude Code 用）">
        <code>{{ baseURL }}</code>
        <a-button type="link" size="small" @click="copy(baseURL)">
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
      <a-col v-for="preset in CLIENT_PRESETS" :key="preset.key" :xs="24" :md="12">
        <a-card size="small" :title="preset.name" class="wg-client-card">
          <template #extra>
            <a-tag :color="preset.endpoint === 'root' ? 'purple' : 'blue'">
              {{ preset.endpoint === 'root' ? 'Anthropic 原生' : 'OpenAI 兼容' }}
            </a-tag>
          </template>
          <div class="wg-muted" style="margin-bottom: 8px">{{ preset.desc }}</div>
          <ol class="wg-client-tips">
            <li v-for="(tip, index) in preset.tips" :key="index">{{ tip }}</li>
          </ol>
          <a-space wrap size="small">
            <a-button size="small" @click="copy(addressOf(preset), '地址已复制')">
              <CopyOutlined />
              复制地址
            </a-button>
            <a-button size="small" @click="copy(configOf(preset), '配置已复制')">
              <CopyOutlined />
              复制配置
            </a-button>
            <a-button v-if="preset.filename" size="small" @click="download(preset)">
              <DownloadOutlined />
              下载配置
            </a-button>
          </a-space>
        </a-card>
      </a-col>
    </a-row>
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
