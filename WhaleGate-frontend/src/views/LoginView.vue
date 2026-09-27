<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  GithubOutlined,
  LockOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { accountApi } from '@/api/account'
import { authApi } from '@/api/auth'
import { useUserStore } from '@/stores/user'
import type { RegisterPayload } from '@/api/types'
import logoMark from '@/assets/logo-mark.png'
import {
  passkeySupported,
  serializeAssertion,
  toRequestOptions,
} from '@/utils/webauthn'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const mode = ref<'login' | 'register'>('login')
const submitting = ref(false)
// 表单必须绑定 model，否则 async-validator 取不到字段值，会出现"已输入仍提示必填"
const formRef = ref()
const form = reactive<RegisterPayload & { account: string }>({
  account: '',
  username: '',
  password: '',
  email: '',
})

// 登录场景只要求填写；注册场景才校验长度（后端最小 8 位）
const accountRules = [{ required: true, message: '请输入用户名或邮箱' }]
const usernameRules = [{ required: true, message: '请输入用户名' }]
const loginPasswordRules = [{ required: true, message: '请输入密码' }]
const registerPasswordRules = [
  { required: true, message: '请输入密码' },
  { min: 8, message: '密码至少 8 位' },
]

const canPasskey = passkeySupported()

// 切换登录/注册时清除上一次的校验提示
watch(mode, () => {
  formRef.value?.clearValidate()
})

async function finishLogin(token: string) {
  userStore.setToken(token)
  await userStore.fetchProfile()
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard'
  router.push(redirect)
}

async function submit() {
  submitting.value = true
  try {
    if (mode.value === 'login') {
      const result = await authApi.login({ account: form.account, password: form.password })
      userStore.setToken(result.token)
      userStore.profile = result.user
    } else {
      await authApi.register({
        username: form.username,
        password: form.password,
        email: form.email || undefined,
      })
      const result = await authApi.login({ account: form.username, password: form.password })
      userStore.setToken(result.token)
      userStore.profile = result.user
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard'
    router.push(redirect)
  } finally {
    submitting.value = false
  }
}

/** GitHub 第三方登录 */
async function githubLogin() {
  try {
    const res = await accountApi.githubAuthorize('login')
    window.location.href = res.authorize_url
  } catch {
    message.warning('服务端未启用 GitHub 登录')
  }
}

/** 通行密钥登录 */
async function passkeyLogin() {
  if (!canPasskey) {
    message.warning('当前浏览器不支持通行密钥')
    return
  }
  submitting.value = true
  try {
    const begin = await accountApi.passkeyLoginBegin()
    const assertion = (await navigator.credentials.get({
      publicKey: toRequestOptions(begin.options),
    })) as PublicKeyCredential | null
    if (!assertion) {
      message.warning('未获取到凭据')
      return
    }
    const result = await accountApi.passkeyLoginFinish(begin.session_id, serializeAssertion(assertion))
    await finishLogin(result.token)
  } catch (e) {
    message.error('通行密钥登录失败：' + ((e as Error).message || '请重试'))
  } finally {
    submitting.value = false
  }
}

/** GitHub 回调会带一次性票据回到登录页 */
onMounted(async () => {
  const ticket = route.query.ticket
  if (typeof ticket === 'string' && ticket) {
    try {
      const res = await accountApi.exchangeTicket(ticket)
      await finishLogin(res.token)
    } catch {
      message.error('第三方登录票据已过期，请重新发起')
    }
  }
})
</script>

<template>
  <div class="wg-login">
    <a-card class="wg-login-card" :bordered="false">
      <div class="wg-login-brand">
        <img :src="logoMark" class="wg-login-logo" alt="WhaleGate logo" />
        <h1>鲸闸 WhaleGate</h1>
        <p>统一的大模型 API 网关</p>
      </div>

      <a-tabs v-model:active-key="mode" centered>
        <a-tab-pane key="login" tab="登录" />
        <a-tab-pane key="register" tab="注册" />
      </a-tabs>

      <a-form ref="formRef" :model="form" layout="vertical" @finish="submit">
        <template v-if="mode === 'login'">
          <a-form-item label="账号" name="account" :rules="accountRules">
            <a-input v-model:value="form.account" size="large" placeholder="用户名或邮箱" allow-clear>
              <template #prefix><UserOutlined /></template>
            </a-input>
          </a-form-item>
        </template>
        <template v-else>
          <a-form-item label="用户名" name="username" :rules="usernameRules">
            <a-input v-model:value="form.username" size="large" placeholder="3-64 位字母、数字或下划线" allow-clear>
              <template #prefix><UserOutlined /></template>
            </a-input>
          </a-form-item>
          <a-form-item label="邮箱" name="email">
            <a-input v-model:value="form.email" size="large" placeholder="选填，用于找回账号" allow-clear />
          </a-form-item>
        </template>

        <a-form-item
          label="密码"
          name="password"
          :rules="mode === 'login' ? loginPasswordRules : registerPasswordRules"
        >
          <a-input-password v-model:value="form.password" size="large" placeholder="请输入密码">
            <template #prefix><LockOutlined /></template>
          </a-input-password>
        </a-form-item>

        <a-button type="primary" html-type="submit" size="large" block :loading="submitting">
          {{ mode === 'login' ? '登录' : '注册并登录' }}
        </a-button>
      </a-form>

      <template v-if="mode === 'login'">
        <a-divider plain>其它登录方式</a-divider>
        <a-space direction="vertical" style="width: 100%">
          <a-button block size="large" :disabled="!canPasskey" :loading="submitting" @click="passkeyLogin">
            <SafetyCertificateOutlined />
            使用通行密钥登录
          </a-button>
          <a-button block size="large" @click="githubLogin">
            <GithubOutlined />
            使用 GitHub 登录
          </a-button>
        </a-space>
        <div v-if="!canPasskey" class="wg-tip">
          当前环境不支持通行密钥（需 HTTPS 或 localhost）
        </div>
      </template>
    </a-card>
  </div>
</template>

<style scoped>
.wg-login {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: linear-gradient(135deg, var(--wg-primary-soft) 0%, #f5f7fa 100%);
  background-image: url('/login-bg.jpg');
  background-size: cover;
  background-position: center;
  background-repeat: no-repeat;
}

/* 遮罩保证卡片与文字在背景图上仍然清晰可读 */
.wg-login::before {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(135deg, rgba(2, 59, 130, 0.45) 0%, rgba(245, 247, 250, 0.65) 100%);
}

.wg-login > * {
  position: relative;
}

.wg-login-card {
  width: 100%;
  max-width: 400px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.08);
}

.wg-login-brand {
  text-align: center;
  margin-bottom: 8px;
}

.wg-login-logo {
  width: 72px;
  height: 72px;
  object-fit: contain;
  margin-bottom: 8px;
}

.wg-login-brand h1 {
  font-size: 22px;
  margin-bottom: 4px;
  color: var(--wg-primary);
}

.wg-login-brand p {
  color: rgba(0, 0, 0, 0.45);
  margin: 0;
}

.wg-tip {
  margin-top: 8px;
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
  text-align: center;
}
</style>
