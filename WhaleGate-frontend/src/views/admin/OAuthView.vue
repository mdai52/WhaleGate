<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { message } from 'ant-design-vue'
import { oauthApi } from '@/api/oauth'
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

const providerName = computed(
  () => providers.value.find((p) => p.id === active.value?.provider)?.name ?? active.value?.provider ?? '',
)

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
    startPolling(provider)
  } catch {
    message.error('发起授权失败')
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
      message.success('授权成功，凭证已保存')
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
    message.success('授权成功，凭证已保存')
    modalOpen.value = false
  } catch {
    message.error('授权失败，请检查授权码是否正确')
  }
}

function openAuthorize() {
  if (active.value?.authorize_url) {
    window.open(active.value.authorize_url, '_blank')
  }
}

onBeforeUnmount(stopPolling)
load()
</script>

<template>
  <div class="wg-page">
    <a-card :loading="loading">
      <div class="wg-card-title" style="margin-bottom: 8px">
        <h2 style="margin: 0">OAuth 登录</h2>
      </div>
      <a-alert
        type="info"
        show-icon
        message="通过授权登录上游服务，自动获取并保存认证文件；凭证可绑定到渠道，替代静态 API Key。"
        style="margin-bottom: 16px"
      />

      <a-empty v-if="!loading && providers.length === 0" description="暂无可用供应商，请在配置文件 oauth.providers 中添加" />

      <div class="wg-provider-list">
        <div v-for="p in providers" :key="p.id" class="wg-provider-card">
          <div class="wg-provider-main">
            <div class="wg-provider-name">{{ p.name }}</div>
            <div class="wg-provider-desc">{{ p.description || p.id }}</div>
            <div class="wg-provider-meta">
              <a-tag :color="p.flow === 'device_code' ? 'purple' : 'blue'">
                {{ p.flow === 'device_code' ? '设备码授权' : '授权码 (PKCE)' }}
              </a-tag>
              <a-tag v-for="s in p.scopes || []" :key="s">{{ s }}</a-tag>
            </div>
          </div>
          <a-button type="primary" @click="start(p)">开始 {{ p.name.replace(' OAuth', '') }} 登录</a-button>
        </div>
      </div>
    </a-card>

    <a-modal
      v-model:open="modalOpen"
      :title="`${providerName} 授权`"
      :footer="null"
      :width="isMobile ? '100%' : 560"
      @cancel="stopPolling"
    >
      <template v-if="active">
        <!-- 授权码流程 -->
        <template v-if="active.authorize_url">
          <a-alert
            type="info"
            show-icon
            message="第一步：点击下方按钮完成上游登录授权"
            style="margin-bottom: 12px"
          />
          <a-button type="primary" block @click="openAuthorize">打开授权页面</a-button>
          <a-typography-paragraph class="wg-url" copyable :ellipsis="{ rows: 2 }">
            {{ active.authorize_url }}
          </a-typography-paragraph>

          <a-alert
            type="warning"
            show-icon
            message="第二步：授权后把页面返回的授权码粘贴到这里"
            style="margin: 16px 0 12px"
          />
          <a-input v-model:value="authCode" placeholder="粘贴授权码（code=... 中的一段）" allow-clear />
          <a-button type="primary" block style="margin-top: 12px" @click="complete">完成授权</a-button>
        </template>

        <!-- 设备码流程 -->
        <template v-else>
          <a-alert type="info" show-icon message="请在浏览器打开验证地址并输入以下代码" style="margin-bottom: 12px" />
          <a-typography-paragraph class="wg-url" copyable>
            {{ active.verification_url || '（未返回验证地址）' }}
          </a-typography-paragraph>
          <div class="wg-usercode">
            <span class="wg-muted">设备码：</span>
            <a-typography-text copyable>{{ active.user_code || '—' }}</a-typography-text>
          </div>
          <div style="margin-top: 16px">
            <a-spin v-if="polling" size="small" />
            <span v-if="polling" style="margin-left: 8px">正在等待授权完成…</span>
            <span v-else class="wg-muted">会话 10 分钟内有效</span>
          </div>
        </template>
      </template>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-provider-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.wg-provider-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
  border: 1px solid #f0f0f0;
  border-radius: 12px;
  transition: box-shadow 0.2s;
}

.wg-provider-card:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.06);
}

.wg-provider-name {
  font-size: 15px;
  font-weight: 600;
  margin-bottom: 4px;
}

.wg-provider-desc {
  color: rgba(0, 0, 0, 0.55);
  margin-bottom: 8px;
}

.wg-provider-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.wg-url {
  font-size: 12px;
  background: #f5f5f5;
  padding: 8px;
  border-radius: 6px;
  margin: 12px 0 0;
  word-break: break-all;
}

.wg-usercode {
  font-size: 14px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}

@media (max-width: 576px) {
  .wg-provider-card {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
