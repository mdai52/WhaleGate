<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CopyOutlined, DeleteOutlined, PlusOutlined, StopOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { apiKeyApi } from '@/api/apikey'
import { useUserStore } from '@/stores/user'
import type { APIKeyItem } from '@/api/types'

const userStore = useUserStore()
const loading = ref(false)
const items = ref<APIKeyItem[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })

const createOpen = ref(false)
const creating = ref(false)
const newKey = ref('')
const form = reactive({ name: '', expires_in_days: undefined as number | undefined })

const columns = [
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '密钥', dataIndex: 'masked_key', key: 'masked_key' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
  { title: '最近使用', dataIndex: 'last_used_at', key: 'last_used_at' },
  { title: '累计请求', dataIndex: 'request_count', key: 'request_count' },
  { title: '累计 Token', dataIndex: 'total_tokens', key: 'total_tokens' },
  { title: '操作', key: 'action', fixed: 'right' as const },
]

/** 顶部统计：当前页所有密钥的合计 */
const stats = computed(() => ({
  count: pagination.total,
  requests: items.value.reduce((acc, i) => acc + (i.request_count || 0), 0),
  tokens: items.value.reduce((acc, i) => acc + (i.total_tokens || 0), 0),
}))

function formatNumber(n: number) {
  return n.toLocaleString('zh-CN')
}

async function load() {
  loading.value = true
  try {
    const res = await apiKeyApi.list({ page: pagination.current, page_size: pagination.pageSize })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function openCreate() {
  newKey.value = ''
  form.name = ''
  form.expires_in_days = undefined
  createOpen.value = true
}

async function submitCreate() {
  creating.value = true
  try {
    const res = await apiKeyApi.create({
      name: form.name || '默认密钥',
      expires_in_days: form.expires_in_days,
    })
    newKey.value = res.key
    await load()
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newKey.value)
    message.success('已复制到剪贴板')
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

function revoke(record: APIKeyItem) {
  Modal.confirm({
    title: '确认吊销该密钥？',
    content: `吊销后「${record.name}」将立即失效，且无法恢复。`,
    okText: '吊销',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await apiKeyApi.revoke(record.id)
      message.success('已吊销')
      await load()
    },
  })
}

function remove(record: APIKeyItem) {
  Modal.confirm({
    title: '确认删除该密钥？',
    content: `删除后「${record.name}」的记录将不再展示。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await apiKeyApi.remove(record.id)
      message.success('已删除')
      await load()
    },
  })
}

function statusText(record: APIKeyItem) {
  if (record.revoked_at) return { text: '已吊销', color: 'red' }
  if (record.expires_at && new Date(record.expires_at) < new Date()) return { text: '已过期', color: 'orange' }
  return record.status === 1 ? { text: '正常', color: 'green' } : { text: '已禁用', color: 'default' }
}

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString('zh-CN') : '—'
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
        <h2 style="margin: 0">密钥管理</h2>
        <a-button type="primary" @click="openCreate">
          <PlusOutlined />
          新建密钥
        </a-button>
      </div>

      <a-alert
        v-if="!newKey && items.length === 0 && !loading"
        type="info"
        show-icon
        message="还没有密钥"
        description="点击右上角「新建密钥」生成 sk- 开头的调用凭证，明文只展示一次。"
        style="margin-bottom: 16px"
      />

      <a-row :gutter="[16, 16]" style="margin-bottom: 16px">
        <a-col :xs="24" :sm="8">
          <a-card size="small">
            <a-statistic title="密钥数量" :value="stats.count" :value-style="{ fontSize: '22px' }" />
            <div class="wg-stat-desc">当前账号持有的 API 密钥数量</div>
          </a-card>
        </a-col>
        <a-col :xs="24" :sm="8">
          <a-card size="small">
            <a-statistic title="累计请求" :value="stats.requests" :value-style="{ fontSize: '22px' }" />
            <div class="wg-stat-desc">全部密钥累计请求数（当前页合计）</div>
          </a-card>
        </a-col>
        <a-col :xs="24" :sm="8">
          <a-card size="small">
            <a-statistic title="累计 Token" :value="stats.tokens" :value-style="{ fontSize: '22px' }" />
            <div class="wg-stat-desc">全部密钥累计 Token 使用量（当前页合计）</div>
          </a-card>
        </a-col>
      </a-row>

      <a-table
        :columns="columns"
        :data-source="items"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 1040 }"
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
          <template v-if="column.key === 'masked_key'">
            <code>{{ (record as APIKeyItem).masked_key }}</code>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="statusText(record as APIKeyItem).color">
              {{ statusText(record as APIKeyItem).text }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'qpm'">
            {{ (record as APIKeyItem).qpm || (userStore.profile ? '默认' : '默认') }}
          </template>
          <template v-else-if="column.key === 'created_at'">
            {{ formatTime((record as APIKeyItem).created_at) }}
          </template>
          <template v-else-if="column.key === 'last_used_at'">
            {{ formatTime((record as APIKeyItem).last_used_at) }}
          </template>
          <template v-else-if="column.key === 'request_count'">
            {{ formatNumber((record as APIKeyItem).request_count || 0) }}
          </template>
          <template v-else-if="column.key === 'total_tokens'">
            {{ formatNumber((record as APIKeyItem).total_tokens || 0) }}
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button danger size="small" :disabled="!!(record as APIKeyItem).revoked_at" @click="revoke(record as APIKeyItem)">
                <StopOutlined />
                吊销
              </a-button>
              <a-button danger size="small" type="text" @click="remove(record as APIKeyItem)">
                <DeleteOutlined />
                删除
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="createOpen" title="新建密钥" :footer="null" :width="480">
      <template v-if="!newKey">
        <a-form layout="vertical" @finish="submitCreate">
          <a-form-item label="名称">
            <a-input v-model:value="form.name" placeholder="例如：生产环境-后端服务" allow-clear />
          </a-form-item>
          <a-form-item label="有效天数">
            <a-input-number v-model:value="form.expires_in_days" :min="1" :max="3650" style="width: 100%" placeholder="留空表示永不过期" />
          </a-form-item>
          <a-button type="primary" html-type="submit" block :loading="creating">生成密钥</a-button>
        </a-form>
      </template>
      <template v-else>
        <a-result status="success" title="密钥已生成" sub-title="请立即复制保存，关闭后将无法再次查看明文。">
          <template #extra>
            <a-input :value="newKey" readonly>
              <template #suffix>
                <a-button type="link" size="small" @click="copyKey">
                  <CopyOutlined />
                  复制
                </a-button>
              </template>
            </a-input>
            <a-button type="primary" style="margin-top: 16px" @click="createOpen = false">我已保存</a-button>
          </template>
        </a-result>
      </template>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-stat-desc {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
  margin-top: 4px;
}
</style>
