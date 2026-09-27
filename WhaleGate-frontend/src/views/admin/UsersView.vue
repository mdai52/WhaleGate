<script setup lang="ts">
import { nextTick, onMounted, reactive, ref, watch } from 'vue'
import { PlusOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { adminApi } from '@/api/admin'
import { useUserStore } from '@/stores/user'
import type { RegisterPayload, UserProfile } from '@/api/types'

const userStore = useUserStore()
const loading = ref(false)
const items = ref<UserProfile[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })
const keyword = ref('')

const createOpen = ref(false)
const creating = ref(false)
// 校验需要绑定 model，否则 async-validator 取不到值
const createFormRef = ref()
const form = reactive<RegisterPayload>({ username: '', password: '', email: '', role: 'user' })

const usernameRules = [{ required: true, message: '请输入用户名' }]
const passwordRules = [
  { required: true, message: '请输入密码' },
  { min: 8, message: '密码至少 8 位' },
]

// 打开弹窗时清空上一次的校验提示
watch(createOpen, (open) => {
  if (open) {
    nextTick(() => createFormRef.value?.clearValidate())
  }
})

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '角色', dataIndex: 'role', key: 'role' },
  { title: '剩余额度（元）', dataIndex: 'quota', key: 'quota' },
  { title: '累计消耗（元）', dataIndex: 'used_quota', key: 'used_quota' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '操作', key: 'action' },
]

const POINTS_PER_YUAN = 1000
const toYuan = (points: number) => (points / POINTS_PER_YUAN).toFixed(3)

async function load() {
  loading.value = true
  try {
    const res = await adminApi.listUsers({
      page: pagination.current,
      page_size: pagination.pageSize,
      keyword: keyword.value || undefined,
    })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function toggleStatus(record: UserProfile) {
  const next = record.status === 1 ? 2 : 1
  Modal.confirm({
    title: next === 1 ? '确认启用该用户？' : '确认禁用该用户？',
    content: `用户：${record.username}`,
    okText: '确定',
    cancelText: '取消',
    onOk: async () => {
      await adminApi.setUserStatus(record.id, next)
      message.success('已更新')
      await load()
    },
  })
}

const rechargeOpen = ref(false)
const rechargeTarget = ref<UserProfile | null>(null)
/** 充值金额，单位元；负数表示扣减 */
const rechargeYuan = ref(100)

function openRecharge(record: UserProfile) {
  rechargeTarget.value = record
  rechargeYuan.value = 100
  rechargeOpen.value = true
}

async function submitRecharge() {
  const target = rechargeTarget.value
  if (!target) {
    return
  }
  const yuan = Number(rechargeYuan.value)
  if (!Number.isFinite(yuan) || yuan === 0) {
    message.warning('请输入非零金额')
    return
  }
  // 1 元 = 1000 点
  const delta = Math.round(yuan * 1000)
  try {
    await adminApi.changeQuota(target.id, delta)
    message.success(`已为「${target.username}」${yuan > 0 ? '充值' : '扣减'} ${Math.abs(yuan)} 元`)
    rechargeOpen.value = false
    await load()
  } catch {
    message.error('额度调整失败')
  }
}

async function submitCreate() {
  creating.value = true
  try {
    await adminApi.createUser({
      username: form.username,
      password: form.password,
      email: form.email || undefined,
      role: form.role,
    })
    message.success('用户已创建')
    createOpen.value = false
    await load()
  } finally {
    creating.value = false
  }
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <h2 style="margin: 0">用户管理</h2>
        <a-space>
          <a-input-search
            v-model:value="keyword"
            placeholder="搜索用户名 / 邮箱 / 昵称"
            allow-clear
            style="width: 240px"
            @search="load"
          />
          <a-button type="primary" @click="createOpen = true">
            <PlusOutlined />
            新建用户
          </a-button>
        </a-space>
      </div>

      <a-table
        :columns="columns"
        :data-source="items"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 880 }"
        :pagination="{
          current: pagination.current,
          pageSize: pagination.pageSize,
          total: pagination.total,
          showSizeChanger: true,
          showTotal: (total: number) => `共 ${total} 条`,
        }"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'role'">
            <a-tag :color="(record as UserProfile).role === 'admin' ? 'blue' : 'default'">
              {{ (record as UserProfile).role === 'admin' ? '管理员' : '普通用户' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'quota'">
            {{ toYuan((record as UserProfile).quota) }}
          </template>
          <template v-else-if="column.key === 'used_quota'">
            {{ toYuan((record as UserProfile).used_quota) }}
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="(record as UserProfile).status === 1 ? 'green' : 'red'">
              {{ (record as UserProfile).status === 1 ? '正常' : '已禁用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="toggleStatus(record as UserProfile)">
                {{ (record as UserProfile).status === 1 ? '禁用' : '启用' }}
              </a-button>
              <a-button size="small" :disabled="!userStore.isAdmin" @click="openRecharge(record as UserProfile)">
                额度
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="createOpen" title="新建用户" :footer="null" :width="440">
      <a-form ref="createFormRef" :model="form" layout="vertical" @finish="submitCreate">
        <a-form-item label="用户名" name="username" :rules="usernameRules">
          <a-input v-model:value="form.username" placeholder="3-64 位字母、数字或下划线" allow-clear />
        </a-form-item>
        <a-form-item label="邮箱" name="email">
          <a-input v-model:value="form.email" placeholder="选填" allow-clear />
        </a-form-item>
        <a-form-item label="密码" name="password" :rules="passwordRules">
          <a-input-password v-model:value="form.password" placeholder="至少 8 位" />
        </a-form-item>
        <a-form-item label="角色" name="role">
          <a-select v-model:value="form.role">
            <a-select-option value="user">普通用户</a-select-option>
            <a-select-option value="admin">管理员</a-select-option>
          </a-select>
        </a-form-item>
        <a-button type="primary" html-type="submit" block :loading="creating">创建</a-button>
      </a-form>
    </a-modal>

    <a-modal
      v-model:open="rechargeOpen"
      :title="rechargeTarget ? `调整「${rechargeTarget.username}」额度` : '调整额度'"
      ok-text="确认"
      cancel-text="取消"
      :width="400"
      @ok="submitRecharge"
    >
      <a-form layout="vertical">
        <a-form-item label="金额（元）" extra="正数充值，负数扣减；1 元 = 1000 点">
          <a-input-number v-model:value="rechargeYuan" :step="10" style="width: 100%" />
        </a-form-item>
        <a-form-item v-if="rechargeTarget" label="当前余额">
          <span>{{ (rechargeTarget.quota / 1000).toFixed(3) }} 元</span>
          <span class="wg-muted" style="margin-left: 8px">
            调整后 {{ ((rechargeTarget.quota + Math.round((Number(rechargeYuan) || 0) * 1000)) / 1000).toFixed(3) }} 元
          </span>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}
</style>
