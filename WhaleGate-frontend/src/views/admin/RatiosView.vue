<script setup lang="ts">
import { reactive, ref } from 'vue'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { adminApi } from '@/api/admin'
import { useIsMobile } from '@/composables/useIsMobile'
import type { ModelRatio } from '@/api/types'

const isMobile = useIsMobile()
const loading = ref(false)
const saving = ref(false)
const items = ref<ModelRatio[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })

const modalOpen = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({ model: '', prompt_ratio: 15, completion_ratio: 60, enabled: true })

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
  { title: '模型', dataIndex: 'model', key: 'model' },
  { title: '输入倍率（点/1K）', dataIndex: 'prompt_ratio', key: 'prompt_ratio' },
  { title: '输出倍率（点/1K）', dataIndex: 'completion_ratio', key: 'completion_ratio' },
  { title: '启用', dataIndex: 'enabled', key: 'enabled' },
  { title: '操作', key: 'action', fixed: 'right' as const },
]

async function load() {
  loading.value = true
  try {
    const res = await adminApi.listRatios({ page: pagination.current, page_size: pagination.pageSize })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { model: '', prompt_ratio: 15, completion_ratio: 60, enabled: true })
  modalOpen.value = true
}

function openEdit(row: ModelRatio) {
  editingId.value = row.id
  Object.assign(form, {
    model: row.model,
    prompt_ratio: row.prompt_ratio,
    completion_ratio: row.completion_ratio,
    enabled: row.enabled,
  })
  modalOpen.value = true
}

async function submit() {
  if (!form.model.trim()) {
    message.warning('请填写模型名')
    return
  }
  saving.value = true
  try {
    await adminApi.upsertRatio({
      model: form.model.trim(),
      prompt_ratio: form.prompt_ratio,
      completion_ratio: form.completion_ratio,
      enabled: form.enabled,
    })
    message.success(editingId.value ? '倍率已更新' : '倍率已创建')
    modalOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

function removeRatio(row: ModelRatio) {
  Modal.confirm({
    title: '确认删除该倍率？',
    content: `模型：${row.model}`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await adminApi.deleteRatio(row.id)
      message.success('已删除')
      await load()
    },
  })
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 8px">
        <h2 style="margin: 0">倍率配置</h2>
        <a-button type="primary" @click="openCreate">
          <PlusOutlined />
          新增倍率
        </a-button>
      </div>
      <a-alert
        type="info"
        show-icon
        message="倍率单位为「点 / 1K tokens」，1 点 = 0.001 元；模型名 * 表示兜底倍率。输出倍率按含思维链的 completion 计费。"
        style="margin-bottom: 16px"
      />

      <a-table
        :columns="columns"
        :data-source="items"
        :loading="loading"
        row-key="id"
        :scroll="{ x: isMobile ? 720 : undefined }"
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
          <template v-if="column.key === 'enabled'">
            <a-tag :color="(record as ModelRatio).enabled ? 'green' : 'default'">
              {{ (record as ModelRatio).enabled ? '启用' : '停用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="openEdit(record as ModelRatio)">编辑</a-button>
              <a-button size="small" danger type="text" @click="removeRatio(record as ModelRatio)">
                <DeleteOutlined />
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="modalOpen" :title="editingId ? '编辑倍率' : '新增倍率'" :footer="null" :width="440">
      <a-form layout="vertical" @finish="submit">
        <a-form-item label="模型名">
          <a-input v-model:value="form.model" placeholder="例如 gpt-4o，* 表示兜底" allow-clear />
        </a-form-item>
        <a-form-item label="输入倍率（点 / 1K tokens）">
          <a-input-number v-model:value="form.prompt_ratio" :min="0" :step="0.5" style="width: 100%" />
        </a-form-item>
        <a-form-item label="输出倍率（点 / 1K tokens）">
          <a-input-number v-model:value="form.completion_ratio" :min="0" :step="0.5" style="width: 100%" />
        </a-form-item>
        <a-form-item label="启用">
          <a-switch v-model:checked="form.enabled" />
        </a-form-item>
        <a-button type="primary" html-type="submit" block :loading="saving">保存</a-button>
      </a-form>
    </a-modal>
  </div>
</template>
