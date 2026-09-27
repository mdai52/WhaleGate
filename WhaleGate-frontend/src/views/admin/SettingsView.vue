<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { LockOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { authApi } from '@/api/auth'
import { settingsApi } from '@/api/settings'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const loading = ref(false)
const saving = ref(false)
const selfUseMode = ref(false)

const pwd = reactive({
  old_password: '',
  new_password: '',
  confirm: '',
})
const pwdSaving = ref(false)

const passwordRules = {
  old_password: [{ required: true, message: '请输入当前密码' }],
  new_password: [
    { required: true, message: '请输入新密码' },
    { min: 8, message: '新密码至少 8 位' },
  ],
  confirm: [{ required: true, message: '请再次输入新密码' }],
}

const pwdFormRef = ref()

async function load() {
  loading.value = true
  try {
    const res = await settingsApi.get()
    selfUseMode.value = res.self_use_mode
  } catch {
    message.error('加载设置失败')
  } finally {
    loading.value = false
  }
}

async function toggleSelfUse(checked: boolean) {
  saving.value = true
  try {
    const res = await settingsApi.update({ self_use_mode: checked })
    selfUseMode.value = res.self_use_mode
    message.success(res.self_use_mode ? '已开启自用模式，所有调用不再计费' : '已关闭自用模式，恢复计费')
  } catch {
    selfUseMode.value = !checked
    message.error('更新失败')
  } finally {
    saving.value = false
  }
}

async function submitPassword() {
  if (pwd.new_password !== pwd.confirm) {
    message.error('两次输入的新密码不一致')
    return
  }
  pwdSaving.value = true
  try {
    const res = await authApi.changePassword(pwd.old_password, pwd.new_password)
    message.success('密码已修改')
    // 接口会签发新令牌，刷新本地会话
    if (res.token) {
      userStore.token = res.token
      localStorage.setItem('wg_token', res.token)
    }
    pwd.old_password = ''
    pwd.new_password = ''
    pwd.confirm = ''
  } catch (error) {
    message.error(error instanceof Error ? error.message : '修改失败，请检查当前密码')
  } finally {
    pwdSaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card :loading="loading" title="自用模式">
      <template #extra>
        <a-button size="small" @click="load">
          <ReloadOutlined />
          刷新
        </a-button>
      </template>

      <a-alert
        :type="selfUseMode ? 'warning' : 'info'"
        show-icon
        style="margin-bottom: 16px"
        :message="selfUseMode ? '自用模式已开启' : '自用模式已关闭'"
        :description="
          selfUseMode
            ? '当前所有用户调用接口均不扣费、免费使用，但仍会记录调用日志与 token 用量（标记为自用）。'
            : '当前按倍率表正常计费：调用前预扣额度，结束后按实际用量结算。'
        "
      />

      <div class="wg-switch-row">
        <div>
          <div class="wg-switch-title">启用自用模式</div>
          <div class="wg-muted">仅管理员可配置，开关后立即生效（最长 5 秒缓存延迟）</div>
        </div>
        <a-switch
          :checked="selfUseMode"
          :loading="saving"
          checked-children="开"
          un-checked-children="关"
          @change="toggleSelfUse"
        />
      </div>
    </a-card>

    <a-card title="修改密码" style="margin-top: 16px">
      <template #extra>
        <span class="wg-muted">当前账号：{{ userStore.profile?.username || '—' }}</span>
      </template>

      <a-form
        ref="pwdFormRef"
        :model="pwd"
        :rules="passwordRules"
        layout="vertical"
        style="max-width: 420px"
        @finish="submitPassword"
      >
        <a-form-item label="当前密码" name="old_password">
          <a-input-password v-model:value="pwd.old_password" placeholder="请输入当前密码" autocomplete="current-password" />
        </a-form-item>
        <a-form-item label="新密码" name="new_password">
          <a-input-password v-model:value="pwd.new_password" placeholder="至少 8 位，建议包含字母与数字" autocomplete="new-password" />
        </a-form-item>
        <a-form-item label="确认新密码" name="confirm">
          <a-input-password v-model:value="pwd.confirm" placeholder="请再次输入新密码" autocomplete="new-password" />
        </a-form-item>
        <a-button type="primary" html-type="submit" :loading="pwdSaving">
          <LockOutlined />
          修改密码
        </a-button>
      </a-form>
    </a-card>
  </div>
</template>

<style scoped>
.wg-switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0;
}

.wg-switch-title {
  font-weight: 600;
  margin-bottom: 4px;
}
</style>
