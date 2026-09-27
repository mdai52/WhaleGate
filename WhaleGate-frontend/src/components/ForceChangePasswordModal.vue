<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { authApi } from '@/api/auth'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const open = ref(false)
const submitting = ref(false)

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const required = computed(() => userStore.profile?.must_change_password === true)

watch(
  () => userStore.profile?.must_change_password,
  (must) => {
    open.value = must === true && userStore.isLogin
  },
  { immediate: true },
)

async function submit() {
  if (!form.oldPassword || !form.newPassword) {
    message.warning('请填写原密码与新密码')
    return
  }
  if (form.newPassword.length < 8) {
    message.warning('新密码至少 8 位')
    return
  }
  if (form.newPassword !== form.confirmPassword) {
    message.warning('两次输入的新密码不一致')
    return
  }
  submitting.value = true
  try {
    const res = await authApi.changePassword(form.oldPassword, form.newPassword)
    if (res.token) {
      userStore.setToken(res.token)
    }
    message.success('密码已更新，请妥善保管')
    form.oldPassword = ''
    form.newPassword = ''
    form.confirmPassword = ''
    await userStore.fetchProfile()
  } catch {
    message.error('修改失败，请确认原密码是否正确')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <a-modal
    v-model:open="open"
    title="需要修改密码"
    :closable="!required"
    :mask-closable="!required"
    :keyboard="!required"
    :footer="null"
    :width="420"
  >
    <a-alert
      type="warning"
      show-icon
      message="当前为系统生成的初始密码"
      description="请修改为自己的密码后再继续使用；修改完成前无法进行其它操作。"
      style="margin-bottom: 16px"
    />
    <a-form layout="vertical" @finish="submit">
      <a-form-item label="原密码">
        <a-input-password v-model:value="form.oldPassword" placeholder="容器日志中的初始密码" />
      </a-form-item>
      <a-form-item label="新密码">
        <a-input-password v-model:value="form.newPassword" placeholder="至少 8 位" />
      </a-form-item>
      <a-form-item label="确认新密码">
        <a-input-password v-model:value="form.confirmPassword" placeholder="再次输入新密码" />
      </a-form-item>
      <a-button type="primary" html-type="submit" block :loading="submitting">修改密码</a-button>
    </a-form>
  </a-modal>
</template>
