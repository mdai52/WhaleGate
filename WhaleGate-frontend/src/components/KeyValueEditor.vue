<script setup lang="ts">
import { ref, watch } from 'vue'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'

const props = defineProps<{
  /** 键值对（值以字符串承载，序列化时按需解析类型） */
  modelValue: Record<string, string>
  keyPlaceholder?: string
  valuePlaceholder?: string
  /** 值输入尝试解析为数字 / 布尔 / JSON，失败则按字符串处理 */
  valueJson?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, string>): void }>()

interface Row {
  key: string
  value: string
}

const rows = ref<Row[]>([])

function toRows(obj: Record<string, string>): Row[] {
  return Object.entries(obj ?? {}).map(([key, value]) => ({ key, value: value == null ? '' : String(value) }))
}

watch(
  () => props.modelValue,
  (next) => {
    const incoming = toRows(next)
    if (JSON.stringify(incoming) !== JSON.stringify(rows.value)) {
      rows.value = incoming
    }
  },
  { immediate: true, deep: true },
)

function emitChange() {
  const out: Record<string, string> = {}
  for (const row of rows.value) {
    const key = row.key.trim()
    if (key) {
      out[key] = row.value
    }
  }
  emit('update:modelValue', out)
}

function addRow() {
  rows.value.push({ key: '', value: '' })
}

function removeRow(index: number) {
  rows.value.splice(index, 1)
  emitChange()
}

function pasteJson() {
  message.info('粘贴 {"键":"值"} 形式的 JSON 到值输入框即可，会自动识别类型')
}
</script>

<template>
  <div class="wg-kv">
    <div v-for="(row, index) in rows" :key="index" class="wg-kv-row">
      <a-input
        v-model:value="row.key"
        :placeholder="keyPlaceholder ?? '参数名'"
        class="wg-kv-key"
        @change="emitChange"
      />
      <a-input
        v-model:value="row.value"
        :placeholder="valuePlaceholder ?? '值'"
        class="wg-kv-value"
        @change="emitChange"
      />
      <a-button size="small" type="text" danger @click="removeRow(index)">
        <DeleteOutlined />
      </a-button>
    </div>
    <div class="wg-kv-actions">
      <a-button size="small" type="dashed" block @click="addRow">
        <PlusOutlined />
        添加一项
      </a-button>
      <a-button v-if="rows.length === 0" size="small" type="text" class="wg-muted" @click="pasteJson">
        填写说明
      </a-button>
    </div>
  </div>
</template>

<style scoped>
.wg-kv-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.wg-kv-key {
  flex: 2;
}

.wg-kv-value {
  flex: 3;
}

.wg-kv-actions {
  display: flex;
  gap: 8px;
}
</style>
