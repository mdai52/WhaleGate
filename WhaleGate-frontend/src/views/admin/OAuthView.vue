<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { message } from 'ant-design-vue'
import { CloudUploadOutlined, CopyOutlined } from '@ant-design/icons-vue'
import { oauthApi } from '@/api/oauth'
import { credentialApi } from '@/api/credential'
import { useIsMobile } from '@/composables/useIsMobile'
import type { OAuthProvider, OAuthStart } from '@/api/types'

const isMobile = useIsMobile()
const loading = ref(false)
const providers = ref<OAuthProvider[]>([])

// 授权中的会话
const modalOpen = ref(false)
const active = ref<OAuthStart | null>(null)
const activeName = ref('')
const authCode = ref('')
const polling = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

// Vertex 服务账号导入
const vertexRegion = ref('us-central1')
const vertexFile = ref<File | null>(null)
const importing = ref(false)

/** 设备授权流：打开链接输码即可，无需回站 */
const deviceProviders = computed(() => providers.value.filter((p) => p.flow === 'device_code'))
/** 授权码流：授权后把地址栏 / 页面上的授权码粘贴回来 */
const codeProviders = computed(() => providers.value.filter((p) => p.flow !== 'device_code'))

const avatarPalette = ['#1677ff', '#722ed1', '#13c2c2', '#fa8c16', '#52c41a', '#eb2f96', '#2f54eb']

function avatarColor(id: string) {
  let sum = 0
  for (const ch of id) {
    sum += ch.charCodeAt(0)
  }
  return avatarPalette[sum % avatarPalette.length]
}

function avatarText(name: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
}

async function load() {
  loading.value = true
  try {
    providers.value = await oauthApi.providers()
  } finally {
    loading.value = false
  }
}

async function start(provider: OAuthProvider) {
  try {
    const res = await oauthApi.start(provider.id)
    active.value = res
    activeName.value = provider.name
    authCode.value = ''
    modalOpen.value = true
    if (res.verification_url) {
      // 设备流：自动打开授权页，减少一步操作
      window.open(res.verification_url, '_blank')
    }
    startPolling(provider)
  } catch {
    message.error('发起授权失败，请稍后重试')
  }
}

function startPolling(provider: OAuthProvider) {
  stopPolling()
  if (provider.flow !== 'device_code') {
    return
  }
  polling.value = true
  timer = setInterval(async () => {
    if (!active.value) {
      stopPolling()
      return
    }
    try {
      const res = await oauthApi.poll(provider.id, active.value.state, activeName.value)
      stopPolling()
      message.success('授权成功，凭证已保存，可在「认证文件」中查看并绑定到渠道')
      modalOpen.value = false
      void res
    } catch {
      /* 仍在等待授权，继续轮询 */
    }
  }, 5000)
}

function stopPolling() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  polling.value = false
}

async function complete() {
  if (!active.value) {
    return
  }
  if (!authCode.value.trim()) {
    message.warning('请粘贴授权码')
    return
  }
  try {
    await oauthApi.complete(active.value.provider, active.value.state, authCode.value.trim(), activeName.value)
    stopPolling()
    message.success('授权成功，凭证已保存，可在「认证文件」中查看并绑定到渠道')
    modalOpen.value = false
  } catch {
    message.error('授权失败，请检查授权码是否完整（若地址栏含 code= 与 state=，只复制 code= 后面的部分）')
  }
}

function openAuthorize() {
  if (active.value?.authorize_url) {
    window.open(active.value.authorize_url, '_blank')
  }
}

async function copyUserCode(code?: string) {
  if (!code) return
  try {
    await navigator.clipboard.writeText(code)
    message.success('已复制')
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

function onVertexFile(file: File) {
  vertexFile.value = file
  return false
}

async function importVertex() {
  if (!vertexFile.value) {
    message.warning('请先选择服务账号 JSON 文件')
    return
  }
  importing.value = true
  try {
    const name = `vertex-${vertexRegion.value || 'default'}`
    await credentialApi.import(vertexFile.value, name)
    message.success('服务账号已导入，可在「认证文件」中查看')
    vertexFile.value = null
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导入失败，请确认是 Google Cloud service account JSON 文件')
  } finally {
    importing.value = false
  }
}

onBeforeUnmount(stopPolling)
load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 4px">
        <h2 style="margin: 0">OAuth 登录</h2>
      </div>
      <a-alert
        type="info"
        show-icon
        style="margin: 12px 0 20px"
        message="通过授权登录上游 AI 服务，自动获取并保存认证文件；凭证可在「认证文件」中管理，并绑定到渠道替代静态 API Key。"
      />

      <a-empty
        v-if="!loading && providers.length === 0"
        description="暂无可用供应商；内置供应商加载失败时可在配置文件 oauth.providers 中手动添加"
      />

      <!-- 设备授权流 -->
      <template v-if="deviceProviders.length">
        <a-divider orientation="left" class="wg-group-title">设备授权登录（打开链接输码即可）</a-divider>
        <div class="wg-provider-list">
          <div v-for="p in deviceProviders" :key="p.id" class="wg-provider-card">
            <div class="wg-avatar" :style="{ background: avatarColor(p.id) }">
              {{ avatarText(p.name) }}
            </div>
            <div class="wg-provider-main">
              <div class="wg-provider-name">{{ p.name }}</div>
              <div class="wg-provider-desc">{{ p.description || p.id }}</div>
            </div>
            <a-button type="primary" @click="start(p)">开始授权登录</a-button>
          </div>
        </div>
      </template>

      <!-- 授权码流 -->
      <template v-if="codeProviders.length">
        <a-divider orientation="left" class="wg-group-title">授权码登录（授权后粘贴授权码）</a-divider>
        <div class="wg-provider-list">
          <div v-for="p in codeProviders" :key="p.id" class="wg-provider-card">
            <div class="wg-avatar" :style="{ background: avatarColor(p.id) }">
              {{ avatarText(p.name) }}
            </div>
            <div class="wg-provider-main">
              <div class="wg-provider-name">{{ p.name }}</div>
              <div class="wg-provider-desc">{{ p.description || p.id }}</div>
            </div>
            <a-button type="primary" @click="start(p)">开始授权登录</a-button>
          </div>
        </div>
      </template>

      <!-- 其他登录方式 -->
      <a-divider orientation="left" class="wg-group-title">其他登录方式</a-divider>
      <div class="wg-provider-list">
        <div class="wg-provider-card">
          <div class="wg-avatar" style="background: #4285f4">V</div>
          <div class="wg-provider-main">
            <div class="wg-provider-name">Vertex 服务账号导入</div>
            <div class="wg-provider-desc">
              上传 Google Cloud 服务账号 JSON（service account key），自动解析并加密保存为 Vertex 凭证。
            </div>
            <div class="wg-vertex-form">
              <a-input v-model:value="vertexRegion" addon-before="项目区域" placeholder="us-central1" style="max-width: 280px" />
              <a-upload :before-upload="onVertexFile" :show-upload-list="false" accept=".json,application/json">
                <a-button>
                  <CloudUploadOutlined />
                  {{ vertexFile ? vertexFile.name : '选择服务账号 JSON 文件' }}
                </a-button>
              </a-upload>
              <a-button type="primary" :loading="importing" @click="importVertex">导入凭证</a-button>
            </div>
          </div>
        </div>
      </div>
    </a-card>

    <!-- 授权会话弹窗 -->
    <a-modal
      v-model:open="modalOpen"
      :title="`正在登录：${activeName}`"
      :width="isMobile ? '100%' : 560"
      :footer="null"
      @cancel="stopPolling"
    >
      <template v-if="active?.flow === 'device_code'">
        <a-steps :current="polling ? 1 : 0" size="small" style="margin-bottom: 16px">
          <a-step title="打开授权页" />
          <a-step title="输入授权码" />
          <a-step title="完成" />
        </a-steps>
        <p>1. 已在新窗口打开授权页（如未打开，
          <a :href="active.verification_url" target="_blank">点此打开</a>）</p>
        <p>2. 在页面中输入以下授权码：</p>
        <div class="wg-user-code">
          <code>{{ active.user_code || '—' }}</code>
          <a-button size="small" @click="copyUserCode(active.user_code)">
            <CopyOutlined />
            复制
          </a-button>
        </div>
        <a-alert
          style="margin-top: 16px"
          :type="polling ? 'info' : 'warning'"
          show-icon
          :message="polling ? '正在等待你在授权页完成确认…' : '授权已中断'"
        />
      </template>

      <template v-else>
        <a-steps :current="authCode ? 1 : 0" size="small" style="margin-bottom: 16px">
          <a-step title="打开授权页并登录" />
          <a-step title="粘贴授权码" />
          <a-step title="完成" />
        </a-steps>
        <p>
          1. 点击按钮打开授权页并登录：
          <a-button size="small" type="primary" @click="openAuthorize">打开授权页</a-button>
        </p>
        <p>2. 授权完成后，从跳转页或浏览器地址栏复制授权码（code= 后面那段）粘贴到这里：</p>
        <a-textarea v-model:value="authCode" :rows="3" placeholder="粘贴授权码" />
        <a-button type="primary" block style="margin-top: 12px" @click="complete">完成登录</a-button>
      </template>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-group-title {
  font-weight: 600;
  color: rgba(0, 0, 0, 0.75);
}

.wg-provider-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.wg-provider-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border: 1px solid #f0f0f0;
  border-radius: 10px;
  transition: box-shadow 0.2s;
}

.wg-provider-card:hover {
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
}

.wg-avatar {
  flex: none;
  width: 44px;
  height: 44px;
  border-radius: 10px;
  color: #fff;
  font-size: 20px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.wg-provider-main {
  flex: 1;
  min-width: 0;
}

.wg-provider-name {
  font-weight: 600;
  margin-bottom: 4px;
}

.wg-provider-desc {
  color: rgba(0, 0, 0, 0.45);
  font-size: 13px;
  line-height: 1.5;
}

.wg-vertex-form {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 12px;
}

.wg-user-code {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: #fafafa;
  border: 1px dashed #d9d9d9;
  border-radius: 8px;
}

.wg-user-code code {
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 2px;
}

@media (max-width: 640px) {
  .wg-provider-card {
    flex-wrap: wrap;
  }
}
</style>
