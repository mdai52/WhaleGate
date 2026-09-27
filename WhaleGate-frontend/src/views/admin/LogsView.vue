<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import dayjs, { type Dayjs } from 'dayjs'
import { DownloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import http from '@/api/http'
import { adminApi } from '@/api/admin'
import type { CallLog } from '@/api/types'

const loading = ref(false)
const items = ref<CallLog[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })

const filters = reactive({
  user_id: undefined as number | undefined,
  model: '',
  status: undefined as number | undefined,
  range: null as [Dayjs, Dayjs] | null,
})

const columns = [
  { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 170 },
  { title: '用户', dataIndex: 'user_id', key: 'user_id', width: 80 },
  { title: '模型', dataIndex: 'model', key: 'model' },
  { title: '上游模型', dataIndex: 'upstream_model', key: 'upstream_model' },
  { title: '渠道', dataIndex: 'channel_name', key: 'channel_name' },
  { title: 'Token', key: 'tokens', width: 160 },
  { title: '点数', dataIndex: 'points', key: 'points', width: 80 },
  { title: '耗时', dataIndex: 'latency_ms', key: 'latency_ms', width: 90 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '参数分配', key: 'params', width: 200 },
]

const summary = computed(() => {
  const total = items.value.reduce((acc, i) => acc + i.total_tokens, 0)
  const points = items.value.reduce((acc, i) => acc + i.points, 0)
  return { total, points }
})

function buildParams() {
  const params: Record<string, unknown> = {
    page: pagination.current,
    page_size: pagination.pageSize,
  }
  if (filters.user_id) params.user_id = filters.user_id
  if (filters.model) params.model = filters.model
  if (filters.status) params.status = filters.status
  if (filters.range?.length === 2) {
    params.start = filters.range[0].toISOString()
    params.end = filters.range[1].toISOString()
  }
  return params
}

async function load() {
  loading.value = true
  try {
    const res = await adminApi.listLogs(buildParams())
    items.value = res.items
    pagination.total = res.total
  } catch {
    message.error('加载日志失败')
  } finally {
    loading.value = false
  }
}

function resetFilters() {
  filters.user_id = undefined
  filters.model = ''
  filters.status = undefined
  filters.range = null
  pagination.current = 1
  void load()
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

const exporting = ref(false)

/** 按当前筛选条件导出 CSV（最多 10000 条） */
async function exportCsv() {
  exporting.value = true
  try {
    const params = buildParams()
    delete params.page
    delete params.page_size
    const response = await http.get<Blob>('/admin/logs/export', {
      params,
      responseType: 'blob',
    })
    const disposition = String(response.headers['content-disposition'] ?? '')
    const match = /filename="?([^";]+)"?/.exec(disposition)
    const url = URL.createObjectURL(response.data)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = match?.[1] ?? 'call-logs.csv'
    anchor.click()
    URL.revokeObjectURL(url)
    message.success('已导出 CSV')
  } catch {
    message.error('导出失败')
  } finally {
    exporting.value = false
  }
}

function formatTime(value?: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '—'
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <div>
          <h2 style="margin: 0">全局调用日志</h2>
          <span class="wg-muted">当前页合计 {{ summary.total }} tokens / {{ summary.points }} 点</span>
        </div>
        <a-button :loading="exporting" @click="exportCsv">
          <DownloadOutlined />
          导出 CSV
        </a-button>
      </div>

      <a-form layout="inline" class="wg-filters">
        <a-form-item label="用户 ID">
          <a-input-number v-model:value="filters.user_id" :min="1" placeholder="不限" style="width: 110px" />
        </a-form-item>
        <a-form-item label="模型">
          <a-input v-model:value="filters.model" placeholder="不限" allow-clear style="width: 160px" />
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="filters.status" placeholder="不限" style="width: 110px" allow-clear>
            <a-select-option :value="1">成功</a-select-option>
            <a-select-option :value="2">失败</a-select-option>
          </a-select>
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
        :scroll="{ x: 1400 }"
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
            {{ formatTime((record as CallLog).created_at) }}
          </template>
          <template v-else-if="column.key === 'tokens'">
            <span class="wg-muted">P</span> {{ (record as CallLog).prompt_tokens }}
            <span class="wg-muted">/ C</span> {{ (record as CallLog).completion_tokens }}
            <a-tag v-if="(record as CallLog).reasoning_tokens" color="purple">
              思维链 {{ (record as CallLog).reasoning_tokens }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'latency_ms'">
            {{ (record as CallLog).latency_ms }} ms
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="(record as CallLog).status === 1 ? 'green' : 'red'">
              {{ (record as CallLog).status === 1 ? '成功' : '失败' }}
            </a-tag>
            <div v-if="(record as CallLog).error_message" class="wg-muted wg-error">
              {{ (record as CallLog).error_message }}
            </div>
          </template>
          <template v-else-if="column.key === 'params'">
            <div v-if="(record as CallLog).params_applied" class="wg-ok">
              已应用：{{ (record as CallLog).params_applied }}
            </div>
            <div v-if="(record as CallLog).params_dropped" class="wg-muted">
              已丢弃：{{ (record as CallLog).params_dropped }}
            </div>
            <a-tag v-if="(record as CallLog).usage_confidence === 'estimated'" color="orange">估算</a-tag>
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
}

.wg-ok {
  color: #389e0d;
}

.wg-error {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 576px) {
  .wg-filters :deep(.ant-form-item) {
    margin-bottom: 8px;
  }
}
</style>
