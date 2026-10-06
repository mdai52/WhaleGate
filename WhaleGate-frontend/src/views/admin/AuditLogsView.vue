<script setup lang="ts">
/**
 * 安全审计：登录、改密、密钥、渠道、额度、倍率、凭证、绑定等管理动作全留痕。
 *
 * 与「全局日志」的分工：全局日志记录调用链路（谁调了什么模型、花了多少点），
 * 这里记录管理动作与安全事件（谁改了配置、谁登录失败），两者互补。
 */
import { computed, reactive, ref } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import dayjs, { type Dayjs } from 'dayjs'
import { message } from 'ant-design-vue'
import http from '@/api/http'
import { adminApi } from '@/api/admin'
import { useIsMobile } from '@/composables/useIsMobile'
import type { AuditLog } from '@/api/types'

const isMobile = useIsMobile()
const loading = ref(false)
const items = ref<AuditLog[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })

const filters = reactive({
  user_id: undefined as number | undefined,
  action: undefined as string | undefined,
  range: null as [Dayjs, Dayjs] | null,
})

/** 动作下拉：与后端 model 包中的审计动作常量一一对应 */
const actionOptions = [
  { value: 'login', label: '登录' },
  { value: 'logout', label: '登出' },
  { value: 'register', label: '注册' },
  { value: 'change_password', label: '修改密码' },
  { value: 'key.create', label: '创建密钥' },
  { value: 'key.update', label: '更新密钥' },
  { value: 'key.revoke', label: '吊销密钥' },
  { value: 'key.delete', label: '删除密钥' },
  { value: 'channel.create', label: '创建渠道' },
  { value: 'channel.update', label: '更新渠道' },
  { value: 'channel.delete', label: '删除渠道' },
  { value: 'channel.test', label: '渠道连通性测试' },
  { value: 'quota.adjust', label: '调整额度' },
  { value: 'user.create', label: '创建用户' },
  { value: 'user.status', label: '启停用户' },
  { value: 'ratio.upsert', label: '保存倍率' },
  { value: 'ratio.delete', label: '删除倍率' },
  { value: 'group.upsert', label: '保存分组' },
  { value: 'group.delete', label: '删除分组' },
  { value: 'credential.import', label: '导入凭证' },
  { value: 'credential.bind', label: '绑定凭证' },
  { value: 'credential.delete', label: '删除凭证' },
  { value: 'identity.bind', label: '绑定第三方账号' },
  { value: 'identity.unbind', label: '解绑第三方账号' },
  { value: 'passkey.register', label: '注册通行密钥' },
  { value: 'passkey.delete', label: '删除通行密钥' },
  { value: 'settings.update', label: '修改系统设置' },
]

const actionLabelMap = computed(() => {
  const map: Record<string, string> = {}
  for (const item of actionOptions) {
    map[item.value] = item.label
  }
  return map
})

function actionLabel(action: string) {
  return actionLabelMap.value[action] ?? action
}

function buildParams() {
  const params: Record<string, unknown> = {
    page: pagination.current,
    page_size: pagination.pageSize,
  }
  if (filters.user_id) {
    params.user_id = filters.user_id
  }
  if (filters.action) {
    params.action = filters.action
  }
  if (filters.range?.[0]) {
    params.start = filters.range[0].toISOString()
  }
  if (filters.range?.[1]) {
    params.end = filters.range[1].toISOString()
  }
  return params
}

async function load() {
  loading.value = true
  try {
    const res = await adminApi.listAuditLogs(buildParams())
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

const exporting = ref(false)

/** 按当前筛选条件导出 CSV，与「全局日志」的导出保持同一形态 */
async function exportCsv() {
  exporting.value = true
  try {
    const params: Record<string, unknown> = {}
    if (filters.user_id) {
      params.user_id = filters.user_id
    }
    if (filters.action) {
      params.action = filters.action
    }
    if (filters.range?.[0]) {
      params.start = filters.range[0].toISOString()
    }
    if (filters.range?.[1]) {
      params.end = filters.range[1].toISOString()
    }
    const res = await http.get('/admin/audit-logs/export', { params, responseType: 'blob' })
    const disposition = String(res.headers['content-disposition'] ?? '')
    const match = /filename="?([^";]+)"?/.exec(disposition)
    const url = URL.createObjectURL(res.data as Blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = match?.[1] ?? 'audit-logs.csv'
    anchor.click()
    URL.revokeObjectURL(url)
    message.success('已导出 CSV')
  } catch {
    message.error('导出失败')
  } finally {
    exporting.value = false
  }
}

function resetFilters() {
  filters.user_id = undefined
  filters.action = undefined
  filters.range = null
  pagination.current = 1
  void load()
}

const failedCount = computed(() => items.value.filter((i) => i.status === 2).length)

const columns = [
  { title: '时间', key: 'created_at', width: 170 },
  { title: '操作人', key: 'actor', width: 160 },
  { title: '动作', key: 'action', width: 150 },
  { title: '对象', key: 'target', width: 160 },
  { title: '状态', key: 'status', width: 90 },
  { title: '来源 IP', key: 'ip', width: 130 },
  { title: '明细', key: 'detail' },
]

function formatTime(value: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '—'
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <div>
          <h2 style="margin: 0">安全审计</h2>
          <span class="wg-muted">
            管理动作与安全事件留痕，含操作人、IP、UA 与追踪 ID；当前页失败 {{ failedCount }} 条
          </span>
        </div>
        <a-space>
          <a-button :loading="exporting" @click="exportCsv">
            <DownloadOutlined />
            导出 CSV
          </a-button>
          <a-button :loading="loading" @click="load">
            <ReloadOutlined />
            刷新
          </a-button>
        </a-space>
      </div>

      <a-form layout="inline" class="wg-filters">
        <a-form-item label="用户 ID">
          <a-input-number
            v-model:value="filters.user_id"
            :min="1"
            placeholder="不限"
            style="width: 110px"
          />
        </a-form-item>
        <a-form-item label="动作">
          <a-select
            v-model:value="filters.action"
            placeholder="不限"
            allow-clear
            show-search
            style="width: 180px"
            :options="actionOptions"
          />
        </a-form-item>
        <a-form-item label="时间范围">
          <a-range-picker v-model:value="filters.range" show-time style="width: 320px" />
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-button type="primary" @click="load">查询</a-button>
            <a-button @click="resetFilters">重置</a-button>
          </a-space>
        </a-form-item>
      </a-form>

      <a-table
        :columns="columns"
        :data-source="items"
        :loading="loading"
        row-key="id"
        size="small"
        :scroll="{ x: isMobile ? 1000 : undefined }"
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
          <template v-if="column.key === 'created_at'">
            {{ formatTime((record as AuditLog).created_at) }}
          </template>
          <template v-else-if="column.key === 'actor'">
            <span>{{ (record as AuditLog).username || '—' }}</span>
            <span v-if="(record as AuditLog).user_id" class="wg-muted">
              （#{{ (record as AuditLog).user_id }}）
            </span>
          </template>
          <template v-else-if="column.key === 'action'">
            {{ actionLabel((record as AuditLog).action) }}
          </template>
          <template v-else-if="column.key === 'target'">
            <span v-if="(record as AuditLog).target_type">
              {{ (record as AuditLog).target_type }}
              <span v-if="(record as AuditLog).target_id" class="wg-muted">
                / {{ (record as AuditLog).target_id }}
              </span>
            </span>
            <span v-else class="wg-muted">—</span>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="(record as AuditLog).status === 1 ? 'green' : 'red'">
              {{ (record as AuditLog).status === 1 ? '成功' : '失败' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'ip'">
            {{ (record as AuditLog).ip || '—' }}
          </template>
          <template v-else-if="column.key === 'detail'">
            <div>{{ (record as AuditLog).detail || '—' }}</div>
            <div v-if="(record as AuditLog).trace_id" class="wg-muted">
              trace: {{ (record as AuditLog).trace_id }}
            </div>
          </template>
        </template>
      </a-table>
    </a-card>
  </div>
</template>

<style scoped>
.wg-filters {
  margin-bottom: 16px;
  row-gap: 8px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
