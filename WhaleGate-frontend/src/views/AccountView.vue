<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import dayjs from 'dayjs'
import {
  DeleteOutlined,
  GithubOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
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

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card :loading="loading" title="账号与安全">
      <a-descriptions :column="1" size="small" bordered style="margin-bottom: 24px">
        <a-descriptions-item label="登录账号">{{ userStore.profile?.username }}</a-descriptions-item>
        <a-descriptions-item label="角色">
          {{ userStore.isAdmin ? '管理员' : '普通用户' }}
        </a-descriptions-item>
        <a-descriptions-item label="邮箱">{{ userStore.profile?.email || '未设置' }}</a-descriptions-item>
      </a-descriptions>

      <h3 class="wg-section">
        <SafetyCertificateOutlined />
        通行密钥（Passkey）
      </h3>
      <p class="wg-muted">
        使用指纹、人脸或设备 PIN 免密登录；凭据只保存在你的设备上。
      </p>

      <a-empty
        v-if="!loading && passkeys.length === 0"
        description="尚未添加通行密钥"
        style="padding: 24px 0"
      />
      <a-list v-else :data-source="passkeys" item-layout="horizontal" size="small">
        <template #renderItem="{ item }">
          <a-list-item>
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
              <a-button size="small" danger type="text" @click="removePasskey(item as WebAuthnCredential)">
                <DeleteOutlined />
              </a-button>
            </template>
          </a-list-item>
        </template>
      </a-list>

      <a-button
        type="primary"
        style="margin-top: 12px"
        :disabled="!canPasskey"
        :loading="binding"
        @click="addPasskey"
      >
        <PlusOutlined />
        添加通行密钥
      </a-button>
      <div v-if="!canPasskey" class="wg-muted" style="margin-top: 8px">
        当前环境不支持通行密钥（需 HTTPS 或 localhost，且浏览器支持 WebAuthn）
      </div>

      <a-divider />

      <h3 class="wg-section">
        <GithubOutlined />
        第三方账号
      </h3>
      <p class="wg-muted">绑定后可使用第三方账号快捷登录本系统。</p>

      <div class="wg-bind-list">
        <div class="wg-bind-card">
          <div class="wg-bind-main">
            <div class="wg-bind-name">
              <GithubOutlined />
              GitHub
            </div>
            <div class="wg-muted">
              {{ githubBound ? `已绑定：${githubBound.display_name || githubBound.provider_uid}` : '未绑定' }}
            </div>
          </div>
          <a-space>
            <a-button v-if="!githubBound" size="small" @click="bindGithub">绑定</a-button>
            <a-button v-else size="small" danger @click="unbindIdentity(githubBound)">解除</a-button>
          </a-space>
        </div>
      </div>
      <div v-if="data?.github_enabled !== true" class="wg-muted" style="margin-top: 8px">
        服务端未配置 GitHub OAuth（auth.github.enabled / client_id）
      </div>
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
.wg-section {
  font-size: 15px;
  margin: 8px 0 4px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}

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
  padding: 12px 16px;
  border: 1px solid #f0f0f0;
  border-radius: 10px;
}

.wg-bind-name {
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
}
</style>
