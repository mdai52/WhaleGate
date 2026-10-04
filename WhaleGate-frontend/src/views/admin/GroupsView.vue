<script setup lang="ts">
/**
 * 分组管理：把倍率与限流策略打包成预设，供密钥创建时选择。
 *
 * 分组是"策略预设"而非"权限域"：密钥记录分组标识，
 * 调用时按键级 > 分组级 > 全局默认的优先级取生效上限。
 */
import { onMounted, reactive, ref } from 'vue'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { groupApi } from '@/api/group'
import type { GroupPayload, KeyGroup } from '@/api/group'

/** 预置的默认分组标识（后端不允许删除） */
const DEFAULT_GROUP = 'default'

const loading = ref(false)
const groups = ref<KeyGroup[]>([])

const modalOpen = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)

const form = reactive({
  name: '',
  display_name: '',
  description: '',
  ratio_multiplier: 1,
  qpm: 0,
  concurrency: 0,
  enabled: true,
})

const columns = [
  { title: '标识', dataIndex: 'name', key: 'name' },
  { title: '名称', dataIndex: 'display_name', key: 'display_name' },
  { title: '倍率系数', key: 'ratio_multiplier', width: 110 },
  { title: 'QPM', key: 'qpm', width: 90 },
  { title: '并发', key: 'concurrency', width: 90 },
  { title: '状态', key: 'status', width: 90 },
  { title: '说明', dataIndex: 'description', key: 'description' },
  { title: '操作', key: 'action', width: 150, fixed: 'right' as const },
]

async function load() {
  loading.value = true
  try {
    groups.value = await groupApi.list()
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '',
    display_name: '',
    description: '',
    ratio_multiplier: 1,
    qpm: 0,
    concurrency: 0,
    enabled: true,
  })
  modalOpen.value = true
}

function openEdit(record: KeyGroup) {
  editingId.value = record.id
  Object.assign(form, {
    name: record.name,
    display_name: record.display_name,
    description: record.description,
    ratio_multiplier: record.ratio_multiplier || 1,
    qpm: record.qpm || 0,
    concurrency: record.concurrency || 0,
    enabled: record.enabled,
  })
  modalOpen.value = true
}

async function submit() {
  if (!editingId.value && !/^[A-Za-z0-9_-]{2,64}$/.test(form.name.trim())) {
    message.error('标识仅支持字母、数字、下划线与中划线，长度 2-64')
    return
  }
  if (form.ratio_multiplier <= 0) {
    message.error('倍率系数需大于 0')
    return
  }
  const payload: GroupPayload = {
    name: form.name.trim(),
    display_name: form.display_name.trim(),
    description: form.description.trim(),
    ratio_multiplier: form.ratio_multiplier,
    qpm: form.qpm || 0,
    concurrency: form.concurrency || 0,
    enabled: form.enabled,
  }
  saving.value = true
  try {
    if (editingId.value) {
      await groupApi.update(editingId.value, payload)
      message.success('分组已更新')
    } else {
      await groupApi.create(payload)
      message.success('分组已创建')
    }
    modalOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

function remove(record: KeyGroup) {
  Modal.confirm({
    title: '确认删除该分组？',
    content: `删除后，归属「${record.display_name || record.name}」的密钥会在下次调用时回落为默认策略（不加价、不额外限流），调用不会中断。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await groupApi.remove(record.id)
      message.success('已删除')
      await load()
    },
  })
}

/** 限制摘要：值为 0 表示该层不设限，回落下一级 */
function limitText(value: number) {
  return value > 0 ? String(value) : '继承'
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <h2 style="margin: 0">分组管理</h2>
        <a-button type="primary" @click="openCreate">
          <PlusOutlined />
          新建分组
        </a-button>
      </div>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="分组的倍率与限流如何生效"
        description="倍率系数会与「按模型的倍率」相乘，用于区分不同分组的计费；QPM 与并发按「密钥级 > 分组级 > 全局默认」逐层回落，留 0 表示该层不设限。分组被删除或禁用后，其下密钥自动回落为默认策略，调用不会中断。"
      />

      <a-table
        :columns="columns"
        :data-source="groups"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 1080 }"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <code>{{ (record as KeyGroup).name }}</code>
          </template>
          <template v-else-if="column.key === 'ratio_multiplier'">
            <a-tag :color="(record as KeyGroup).ratio_multiplier > 1 ? 'orange' : 'green'">
              ×{{ (record as KeyGroup).ratio_multiplier }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'qpm'">
            <span :class="{ 'wg-muted': !(record as KeyGroup).qpm }">
              {{ limitText((record as KeyGroup).qpm) }}
            </span>
          </template>
          <template v-else-if="column.key === 'concurrency'">
            <span :class="{ 'wg-muted': !(record as KeyGroup).concurrency }">
              {{ limitText((record as KeyGroup).concurrency) }}
            </span>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="(record as KeyGroup).enabled ? 'green' : 'default'">
              {{ (record as KeyGroup).enabled ? '启用' : '已禁用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'description'">
            <span class="wg-muted">{{ (record as KeyGroup).description || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="openEdit(record as KeyGroup)">
                <EditOutlined />
                配置
              </a-button>
              <a-button
                danger
                size="small"
                :disabled="(record as KeyGroup).name === DEFAULT_GROUP"
                @click="remove(record as KeyGroup)"
              >
                <DeleteOutlined />
                删除
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="modalOpen"
      :title="editingId ? '编辑分组' : '新建分组'"
      :footer="null"
      :width="520"
    >
      <a-form :model="form" layout="vertical" @finish="submit">
        <a-form-item label="标识" required>
          <a-input
            v-model:value="form.name"
            :disabled="!!editingId"
            placeholder="字母 / 数字 / _ / -，如 vip、internal"
            allow-clear
          />
          <div class="wg-muted wg-hint">
            密钥上记录的就是这个标识。{{ editingId ? '标识是密钥的引用键，创建后不可修改。' : '' }}
          </div>
        </a-form-item>

        <a-form-item label="名称">
          <a-input v-model:value="form.display_name" placeholder="留空则与标识相同" allow-clear />
        </a-form-item>

        <a-form-item label="说明">
          <a-input
            v-model:value="form.description"
            placeholder="选填，例如：内部服务专用，不参与计费"
            allow-clear
          />
        </a-form-item>

        <a-form-item label="倍率系数">
          <a-input-number
            v-model:value="form.ratio_multiplier"
            :min="0.01"
            :max="1000"
            :step="0.1"
            :precision="2"
            style="width: 100%"
            addon-after="倍"
          />
          <div class="wg-muted wg-hint">
            在「按模型倍率」算出的点数上相乘：1 = 不加价，2 = 加倍计费，0.5 = 五折。
          </div>
        </a-form-item>

        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item label="QPM 上限">
              <a-input-number
                v-model:value="form.qpm"
                :min="0"
                style="width: 100%"
                placeholder="0 = 继承全局默认"
              />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="并发上限">
              <a-input-number
                v-model:value="form.concurrency"
                :min="0"
                style="width: 100%"
                placeholder="0 = 继承全局默认"
              />
            </a-form-item>
          </a-col>
        </a-row>
        <div class="wg-muted wg-hint" style="margin-top: -8px">
          密钥上单独配置的限流优先于分组；分组为 0 时继续回落到全局默认。
        </div>

        <a-form-item label="状态">
          <a-switch v-model:checked="form.enabled" checked-children="启用" un-checked-children="禁用" />
          <div class="wg-muted wg-hint">禁用后，归属该分组的密钥会回落为默认策略。</div>
        </a-form-item>

        <a-button type="primary" html-type="submit" block :loading="saving">
          {{ editingId ? '保存配置' : '创建分组' }}
        </a-button>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-hint {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.6;
}
</style>
