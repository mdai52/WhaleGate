<script setup lang="ts">
/**
 * 密钥一键导入：按各客户端的填法生成配置，可复制或下载。
 *
 * 密钥明文只在创建那一刻展示一次，之后无法回读，
 * 因此这里必须由使用者粘贴明文——这是有意的安全取舍。
 */
import { ref } from 'vue'
import { CopyOutlined, DownloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { CLIENT_PRESETS, type ClientPreset } from '@/utils/clients'

defineProps<{
  /** 当前密钥的打码展示值，仅作提示 */
  maskedKey: string
}>()

const open = defineModel<boolean>('open', { required: true })

/** 使用者粘贴的完整明文 */
const fullKey = ref('')

const origin = window.location.origin

function endpointFor(preset: ClientPreset) {
  return preset.endpoint === 'root' ? origin : `${origin}/v1`
}

function snippetFor(preset: ClientPreset) {
  const key = fullKey.value.trim() || '<在此粘贴完整明文>'
  if (preset.endpoint === 'root') {
    return `export ANTHROPIC_BASE_URL="${origin}"\nexport ANTHROPIC_AUTH_TOKEN="${key}"`
  }
  return `Base URL:  ${origin}/v1\nAPI Key:   ${key}\nModel:     <模型名>`
}

async function copy(text: string, tip: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

function download(preset: ClientPreset) {
  const text = snippetFor(preset)
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
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
  <a-drawer v-model:open="open" title="一键导入" width="520" :footer="null">
    <a-alert
      type="info"
      show-icon
      style="margin-bottom: 16px"
      message="密钥明文只在创建时展示一次"
      description="网关只保存密钥哈希，无法回读明文。请在下方粘贴创建时保存的完整密钥；若已丢失，请重新创建一个密钥。"
    />

    <a-descriptions bordered size="small" :column="1">
      <a-descriptions-item label="当前密钥">{{ maskedKey || '—' }}</a-descriptions-item>
    </a-descriptions>

    <a-form layout="vertical" style="margin-top: 12px">
      <a-form-item label="完整密钥明文">
        <a-input-password
          v-model:value="fullKey"
          placeholder="sk-...（创建时保存的完整密钥）"
          allow-clear
        />
      </a-form-item>
    </a-form>

    <a-collapse>
      <a-collapse-panel
        v-for="preset in CLIENT_PRESETS"
        :key="preset.key"
        :header="preset.name"
      >
        <a-space direction="vertical" style="width: 100%">
          <div class="wg-muted">{{ preset.desc }}</div>

          <a-alert
            v-if="preset.blockedReason"
            type="warning"
            show-icon
            :message="`暂不可导入：${preset.blockedReason}`"
          />

          <template v-else>
            <a-descriptions bordered size="small" :column="1">
              <a-descriptions-item label="API 地址">
                <code>{{ endpointFor(preset) }}</code>
                <a-button
                  type="link"
                  size="small"
                  @click="copy(endpointFor(preset), '地址已复制')"
                >
                  <CopyOutlined />
                </a-button>
              </a-descriptions-item>
            </a-descriptions>

            <ol class="wg-tips">
              <li v-for="(tip, index) in preset.tips" :key="index">{{ tip }}</li>
            </ol>

            <a-space wrap>
              <a-button size="small" @click="copy(snippetFor(preset), '配置已复制')">
                <CopyOutlined />
                复制配置
              </a-button>
              <a-button v-if="preset.filename" size="small" @click="download(preset)">
                <DownloadOutlined />
                下载配置
              </a-button>
            </a-space>
          </template>
        </a-space>
      </a-collapse-panel>
    </a-collapse>
  </a-drawer>
</template>

<style scoped>
.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}

.wg-tips {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
  line-height: 1.9;
}
</style>
