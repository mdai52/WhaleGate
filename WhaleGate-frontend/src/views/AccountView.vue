<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import dayjs from 'dayjs'
import {
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  DisconnectOutlined,
  GithubOutlined,
  InfoCircleOutlined,
  MailOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { accountApi } from '@/api/account'
import { useUserStore } from '@/stores/user'
import {
  passkeySupported,
  serializeAttestation,
  toCreationOptions,
} from '@/utils/webauthn'
import type { BindingsResult, UserIdentity, WebAuthnCredential } from '@/api/types'

const userStore = useUserStore()

const loading = ref(false)
const binding = ref(false)
const data = ref<BindingsResult | null>(null)

const githubBound = computed<UserIdentity | undefined>(() =>
  data.value?.identities.find((i) => i.provider === 'github'),
)
const passkeys = computed<WebAuthnCredential[]>(() => data.value?.passkeys ?? [])
const canPasskey = computed(() => data.value?.passkey_enabled === true && passkeySupported())

async function load() {
  loading.value = true
  try {
    data.value = await accountApi.bindings()
  } catch {
    message.error('加载绑定信息失败')
  } finally {
    loading.value = false
  }
}

/** 添加通行密钥 */
async function addPasskey() {
  if (!canPasskey.value) {
    message.warning('当前浏览器或站点配置不支持通行密钥')
    return
  }
  binding.value = true
  try {
    const begin = await accountApi.passkeyRegisterBegin('通行密钥')
    const credential = (await navigator.credentials.create({
      publicKey: toCreationOptions(begin.options),
    })) as PublicKeyCredential | null
    if (!credential) {
      message.warning('未获取到凭据')
      return
    }
    await accountApi.passkeyRegisterFinish(begin.session_id, serializeAttestation(credential))
    message.success('通行密钥已添加')
    await load()
  } catch (e) {
    message.error('添加失败：' + ((e as Error).message || '请重试'))
  } finally {
    binding.value = false
  }
}

/** 绑定 GitHub */
async function bindGithub() {
  if (data.value?.github_enabled !== true) {
    message.warning('服务端未启用 GitHub 登录')
    return
  }
  try {
    const res = await accountApi.githubAuthorize('bind')
    window.open(res.authorize_url, '_blank')
    message.info('请在打开的窗口中完成 GitHub 授权，然后把授权码粘贴到下一步')
    showCodeModal(res.authorize_url)
  } catch {
    message.error('获取授权地址失败')
  }
}

const codeModalOpen = ref(false)
const authCode = ref('')

function showCodeModal(_url: string) {
  authCode.value = ''
  codeModalOpen.value = true
}

/** 从粘贴的回调 URL 或授权码中提取 code 与 state 完成绑定 */
async function submitGithubCode() {
  const raw = authCode.value.trim()
  if (!raw) {
    message.warning('请粘贴回调地址或授权码')
    return
  }
  const parsed = new URL(raw.includes('?') ? raw : 'https://x/?' + raw)
  const code = parsed.searchParams.get('code')
  const state = parsed.searchParams.get('state')
  if (!code || !state) {
    message.warning('未能从中解析出 code 与 state')
    return
  }
  binding.value = true
  try {
    await accountApi.githubBind(code, state)
    message.success('GitHub 绑定成功')
    codeModalOpen.value = false
    await load()
  } catch {
    message.error('绑定失败，请确认授权码是否有效')
  } finally {
    binding.value = false
  }
}

function unbindIdentity(item: UserIdentity) {
  Modal.confirm({
    title: '确认解除绑定？',
    content: `解除后将无法使用 ${item.provider} 登录该账号。`,
    okText: '解除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await accountApi.unbindIdentity(item.id)
      message.success('已解除绑定')
      await load()
    },
  })
}

function removePasskey(item: WebAuthnCredential) {
  Modal.confirm({
    title: '确认删除该通行密钥？',
    content: `删除后「${item.name}」将无法用于登录。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await accountApi.deletePasskey(item.id)
      message.success('已删除')
      await load()
    },
  })
}

function formatTime(value?: string | null) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—'
}

async function copyText(value?: string | null) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    message.success('已复制')
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card :loading="loading" title="账号与安全" class="wg-account-card">
      <!-- 账号信息 -->
      <section class="wg-section-block">
        <h3 class="wg-section-title"><UserOutlined /> 账号信息</h3>
        <div class="wg-info-grid">
          <div class="wg-info-item">
            <div class="wg-info-label"><UserOutlined /> 登录账号</div>
            <div class="wg-info-value">
              {{ userStore.profile?.username || '—' }}
              <a-button
                v-if="userStore.profile?.username"
                type="link"
                size="small"
                class="wg-copy-btn"
                @click="copyText(userStore.profile?.username)"
              >
                <CopyOutlined />
              </a-button>
            </div>
          </div>
          <div class="wg-info-item">
            <div class="wg-info-label"><SafetyCertificateOutlined /> 角色</div>
            <div class="wg-info-value">
              <a-tag :color="userStore.isAdmin ? 'red' : 'blue'">
                {{ userStore.isAdmin ? '管理员' : '普通用户' }}
              </a-tag>
            </div>
          </div>
          <div class="wg-info-item">
            <div class="wg-info-label"><MailOutlined /> 邮箱</div>
            <div class="wg-info-value">
              {{ userStore.profile?.email || '未设置' }}
              <a-button
                v-if="userStore.profile?.email"
                type="link"
                size="small"
                class="wg-copy-btn"
                @click="copyText(userStore.profile?.email)"
              >
                <CopyOutlined />
              </a-button>
            </div>
          </div>
        </div>
      </section>

      <a-divider />

      <!-- 安全状态 -->
      <section class="wg-section-block">
        <h3 class="wg-section-title"><CheckCircleOutlined /> 安全状态</h3>
        <div class="wg-security-grid">
          <div class="wg-security-item" :class="{ 'is-active': passkeys.length > 0 }">
            <SafetyCertificateOutlined class="wg-security-icon" />
            <div>
              <div class="wg-security-name">通行密钥</div>
              <div class="wg-security-status">
                {{ passkeys.length > 0 ? `已添加 ${passkeys.length} 个` : '未添加' }}
              </div>
            </div>
          </div>
          <div class="wg-security-item" :class="{ 'is-active': !!githubBound }">
            <GithubOutlined class="wg-security-icon" />
            <div>
              <div class="wg-security-name">GitHub</div>
              <div class="wg-security-status">
                {{ githubBound ? `已绑定：${githubBound.display_name || githubBound.provider_uid}` : '未绑定' }}
              </div>
            </div>
          </div>
        </div>
      </section>

      <a-divider />

      <!-- 通行密钥 -->
      <section class="wg-section-block">
        <div class="wg-section-header">
          <h3 class="wg-section-title"><SafetyCertificateOutlined /> 通行密钥（Passkey）</h3>
          <a-button
            type="primary"
            :disabled="!canPasskey"
            :loading="binding"
            @click="addPasskey"
          >
            <PlusOutlined />
            添加通行密钥
          </a-button>
        </div>
        <p class="wg-muted">
          使用指纹、人脸或设备 PIN 免密登录；凭据只保存在你的设备上，不会上传到服务器。
        </p>

        <a-alert
          v-if="!canPasskey"
          type="info"
          show-icon
          :message="'当前环境不支持通行密钥'"
          :description="'需要 HTTPS 或 localhost，且浏览器支持 WebAuthn。如果你使用 IP 访问或浏览器版本过低，将无法注册。'"
          style="margin: 12px 0"
        />

        <a-empty
          v-else-if="!loading && passkeys.length === 0"
          description="尚未添加通行密钥"
          style="padding: 32px 0"
        >
          <template #extra>
            <a-button type="primary" @click="addPasskey">
              <PlusOutlined />
              立即添加
            </a-button>
          </template>
        </a-empty>
        <a-list v-else-if="passkeys.length > 0" :data-source="passkeys" item-layout="horizontal" size="small">
          <template #renderItem="{ item }">
            <a-list-item class="wg-passkey-item">
              <a-list-item-meta :title="(item as WebAuthnCredential).name">
                <template #description>
                  <span class="wg-muted">
                    添加于 {{ formatTime((item as WebAuthnCredential).created_at) }}
                    <template v-if="(item as WebAuthnCredential).last_used_at">
                      · 最近使用 {{ formatTime((item as WebAuthnCredential).last_used_at) }}
                    </template>
                  </span>
                </template>
              </a-list-item-meta>
              <template #actions>
                <a-button
                  size="small"
                  danger
                  type="text"
                  @click="removePasskey(item as WebAuthnCredential)"
                >
                  <DeleteOutlined />
                  删除
                </a-button>
              </template>
            </a-list-item>
          </template>
        </a-list>
      </section>

      <a-divider />

      <!-- 第三方账号 -->
      <section class="wg-section-block">
        <h3 class="wg-section-title"><GithubOutlined /> 第三方账号</h3>
        <p class="wg-muted">绑定后可使用第三方账号快捷登录本系统。</p>

        <div class="wg-bind-list">
          <div class="wg-bind-card">
            <div class="wg-bind-main">
              <div class="wg-bind-name">
                <GithubOutlined />
                GitHub
                <a-tag v-if="githubBound" color="green" size="small">已绑定</a-tag>
                <a-tag v-else color="default" size="small">未绑定</a-tag>
              </div>
              <div class="wg-muted">
                <template v-if="githubBound">
                  {{ githubBound.display_name || githubBound.provider_uid }}
                </template>
                <template v-else-if="data?.github_enabled !== true">
                  <InfoCircleOutlined /> 管理员未开启 GitHub OAuth 登录
                </template>
                <template v-else>点击右侧按钮绑定 GitHub 账号</template>
              </div>
            </div>
            <a-space>
              <a-button
                v-if="!githubBound"
                type="primary"
                size="small"
                :disabled="data?.github_enabled !== true"
                :title="data?.github_enabled !== true ? '服务端未配置 GitHub OAuth' : ''"
                @click="bindGithub"
              >
                <GithubOutlined />
                绑定
              </a-button>
              <a-button v-else size="small" danger @click="unbindIdentity(githubBound)">
                <DisconnectOutlined />
                解除
              </a-button>
            </a-space>
          </div>
        </div>
      </section>
    </a-card>

    <a-modal
      v-model:open="codeModalOpen"
      title="完成 GitHub 绑定"
      ok-text="绑定"
      cancel-text="取消"
      :confirm-loading="binding"
      @ok="submitGithubCode"
    >
      <a-form layout="vertical">
        <a-form-item
          label="回调地址或授权码"
          extra="授权完成后浏览器会跳转到一个地址，把它整段粘贴到这里；也可只粘贴 code=xxx&state=yyy"
        >
          <a-input v-model:value="authCode" placeholder="http://.../api/v1/auth/github/callback?code=...&state=..." allow-clear />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-page {
  padding: 24px;
}

.wg-account-card :deep(.ant-card-head-title) {
  font-weight: 600;
}

.wg-section-block {
  margin-bottom: 4px;
}

.wg-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 4px;
}

.wg-section-title {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  margin: 4px 0 0;
}

/* 账号信息 */
.wg-info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
  margin-top: 12px;
}

.wg-info-item {
  background: #fafafa;
  border-radius: 10px;
  padding: 14px 16px;
}

.wg-info-label {
  color: rgba(0, 0, 0, 0.45);
  font-size: 13px;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.wg-info-value {
  font-size: 15px;
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 4px;
  word-break: break-all;
}

.wg-copy-btn {
  padding: 0 4px;
  height: auto;
  color: rgba(0, 0, 0, 0.45);
}

.wg-copy-btn:hover {
  color: #1677ff;
}

/* 安全状态 */
.wg-security-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px;
  margin-top: 12px;
}

.wg-security-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border: 1px solid #f0f0f0;
  border-radius: 10px;
  background: #fff;
  transition: all 0.2s;
}

.wg-security-item.is-active {
  border-color: #b7eb8f;
  background: #f6ffed;
}

.wg-security-icon {
  font-size: 22px;
  color: rgba(0, 0, 0, 0.25);
}

.wg-security-item.is-active .wg-security-icon {
  color: #52c41a;
}

.wg-security-name {
  font-weight: 600;
  font-size: 14px;
}

.wg-security-status {
  color: rgba(0, 0, 0, 0.45);
  font-size: 13px;
  margin-top: 2px;
}

/* 通行密钥 */
.wg-passkey-item :deep(.ant-list-item-action) {
  margin-inline-start: 24px;
}

/* 第三方绑定 */
.wg-bind-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}

.wg-bind-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  border: 1px solid #f0f0f0;
  border-radius: 10px;
}

.wg-bind-main {
  min-width: 0;
}

.wg-bind-name {
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

@media (max-width: 640px) {
  .wg-page {
    padding: 12px;
  }

  .wg-info-grid,
  .wg-security-grid {
    grid-template-columns: 1fr;
  }

  .wg-bind-card {
    flex-wrap: wrap;
  }
}
</style>
