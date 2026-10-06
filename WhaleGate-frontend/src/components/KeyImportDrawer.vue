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
import {
  CLIENT_PRESETS,
  configFor,
  endpointFor,
  snippetFor,
  type ClientPreset,
} from '@/utils/clients'

defineProps<{
  /** 当前密钥的打码展示值，仅作提示 */
  maskedKey: string
}>()

const open = defineModel<boolean>('open', { required: true })

/** 使用者粘贴的完整明文 */
const fullKey = ref('')

const origin = window.location.origin

function addressOf(preset: ClientPreset) {
  return endpointFor(preset, origin)
}

function configOf(preset: ClientPreset) {
  return snippetFor(preset, { origin, apiKey: fullKey.value })
}

async function copy(text: string, tip: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

/** 只给真正用配置文件接入的客户端提供下载，避免给出无法导入的假文件 */
function fileOf(preset: ClientPreset) {
  return configFor(preset, { origin, apiKey: fullKey.value })
}

function download(preset: ClientPreset) {
  const file = fileOf(preset)
  if (!file) {
    return
  }
  const blob = new Blob([file.text], { type: file.mime })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = file.filename
  link.click()
  URL.revokeObjectURL(url)
  message.success(`${file.filename} 已下载`)
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

          <a-descriptions bordered size="small" :column="1">
            <a-descriptions-item :label="preset.endpoint === 'root' ? '网关地址' : 'API 地址'">
              <code>{{ addressOf(preset) }}</code>
              <a-button type="link" size="small" @click="copy(addressOf(preset), '地址已复制')">
                <CopyOutlined />
              </a-button>
            </a-descriptions-item>
          </a-descriptions>

          <ol class="wg-tips">
            <li v-for="(tip, index) in preset.tips" :key="index">{{ tip }}</li>
          </ol>

          <a-space wrap>
            <a-button size="small" @click="copy(configOf(preset), '配置已复制')">
              <CopyOutlined />
              复制配置
            </a-button>
            <a-button v-if="fileOf(preset)" size="small" @click="download(preset)">
              <DownloadOutlined />
              下载配置
            </a-button>
            <a-tooltip v-else title="该客户端没有配置文件形态，请按上面的要点在界面中填写">
              <a-button size="small" disabled>
                <DownloadOutlined />
                无配置文件
              </a-button>
            </a-tooltip>
          </a-space>
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
