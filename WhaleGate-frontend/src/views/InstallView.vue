<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CheckCircleOutlined, RocketOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { systemApi } from '@/api/system'
import { useUserStore } from '@/stores/user'
import logoMark from '@/assets/logo-mark.png'

const router = useRouter()
const userStore = useUserStore()

const loading = ref(true)
const submitting = ref(false)
const done = ref(false)

const form = reactive({
  username: 'admin',
  password: '',
  confirm: '',
  email: '',
  nickname: '系统管理员',
})

const rules = {
  username: [
    { required: true, message: '请填写管理员账号' },
    { min: 3, message: '账号至少 3 个字符' },
  ],
  password: [
    { required: true, message: '请设置管理员密码' },
    { min: 8, message: '密码至少 8 位' },
  ],
  confirm: [{ required: true, message: '请再次输入密码' }],
}

const steps = [
  { title: '创建管理员', description: '设置账号与密码' },
  { title: '添加渠道', description: '填入上游地址与密钥，自动探测模型' },
  { title: '创建密钥', description: '生成 sk- 开头的调用凭证' },
  { title: '开始调用', description: '按接入指南对接你的应用' },
]

async function check() {
  loading.value = true
  try {
    const res = await systemApi.status()
    // 已完成初始化则直接进入登录
    if (res.initialized) {
      await router.replace({ name: 'login' })
    }
  } catch {
    message.error('无法连接服务端，请检查后端是否已启动')
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (form.password !== form.confirm) {
    message.error('两次输入的密码不一致')
    return
  }
  submitting.value = true
  try {
    const res = await systemApi.install({
      username: form.username.trim(),
      password: form.password,
      email: form.email.trim() || undefined,
      nickname: form.nickname.trim() || undefined,
    })
    userStore.token = res.token
    localStorage.setItem('wg_token', res.token)
    done.value = true
    message.success('初始化完成，已自动登录')
    await userStore.fetchProfile()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '初始化失败')
  } finally {
    submitting.value = false
  }
}

onMounted(check)
</script>

<template>
  <div class="wg-install">
    <a-card class="wg-install-card" :bordered="false">
      <div v-if="loading">
        <a-skeleton active :paragraph="{ rows: 4 }" />
      </div>

      <div v-else-if="done">
        <a-result status="success" title="安装完成" sub-title="管理员账号已创建，接下来按引导完成配置即可开始调用。">
          <template #extra>
            <a-steps direction="vertical" size="small" :current="0">
              <a-step v-for="(s, i) in steps" :key="i" :title="s.title" :description="s.description" />
            </a-steps>
            <a-space style="margin-top: 24px">
              <a-button type="primary" @click="router.push('/admin/channels')">
                <RocketOutlined />
                去添加渠道
              </a-button>
              <a-button @click="router.push('/guide')">查看接入指南</a-button>
            </a-space>
          </template>
        </a-result>
      </div>

      <div v-else>
        <div class="wg-install-brand">
          <img :src="logoMark" class="wg-install-logo" alt="WhaleGate" />
          <h1>初始化鲸闸 WhaleGate</h1>
          <p>系统尚未初始化，请创建第一个管理员账号</p>
        </div>

        <a-form :model="form" :rules="rules" layout="vertical" @finish="submit">
          <a-form-item label="管理员账号" name="username">
            <a-input v-model:value="form.username" placeholder="例如：admin" allow-clear />
          </a-form-item>
          <a-form-item label="登录密码" name="password">
            <a-input-password v-model:value="form.password" placeholder="至少 8 位，建议包含字母与数字" />
          </a-form-item>
          <a-form-item label="确认密码" name="confirm">
            <a-input-password v-model:value="form.confirm" placeholder="请再次输入密码" />
          </a-form-item>
          <a-row :gutter="12">
            <a-col :span="12">
              <a-form-item label="昵称">
                <a-input v-model:value="form.nickname" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item label="邮箱">
                <a-input v-model:value="form.email" placeholder="选填，用于找回账号" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-button type="primary" block size="large" html-type="submit" :loading="submitting">
            <CheckCircleOutlined />
            完成安装
          </a-button>
        </a-form>

        <a-alert
          style="margin-top: 16px"
          type="info"
          show-icon
          message="安装后建议"
          description="进入「渠道管理」填入上游 API 地址与密钥，点击自动探测即可识别协议与模型列表；再到「密钥管理」生成 sk- 开头的调用凭证，即可按接入指南对接应用。"
        />
      </div>
    </a-card>
  </div>
</template>

<style scoped>
.wg-install {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: url('/login-bg.jpg') center / cover no-repeat;
}

.wg-install::before {
  content: '';
  position: absolute;
  inset: 0;
  background: rgba(2, 59, 130, 0.55);
}

.wg-install-card {
  position: relative;
  width: 100%;
  max-width: 520px;
  border-radius: 12px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
}

.wg-install-brand {
  text-align: center;
  margin-bottom: 24px;
}

.wg-install-logo {
  width: 64px;
  height: 64px;
  object-fit: contain;
  margin-bottom: 8px;
}

.wg-install-brand h1 {
  font-size: 20px;
  margin-bottom: 4px;
  color: var(--wg-primary);
}

.wg-install {
  position: relative;
}
</style>
