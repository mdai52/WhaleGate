<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { parseJSON } from '@/utils/json'
import JsonValueEditor from './JsonValueEditor.vue'

const props = defineProps<{
  /** 键值对（值以字符串承载，序列化时按需解析类型） */
  modelValue: Record<string, string>
  keyPlaceholder?: string
  valuePlaceholder?: string
  /** 键的候选值，支持下拉选择，仍可自由输入 */
  keyOptions?: string[]
  /** 值的候选值，例如上游已登记的模型名 */
  valueOptions?: string[]
  /** 空状态说明 */
  emptyHint?: string
  /** 可一键填入的示例（JSON 对象），例如 {"temperature":0.7} */
  sample?: string
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

const keySelectOptions = computed(() => (props.keyOptions ?? []).map((v) => ({ value: v })))

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

/** 把示例 JSON 展开成多行，省去逐条手填。 */
function applySample() {
  const parsed = parseJSON<Record<string, unknown>>(props.sample ?? '', {})
  if (!parsed.ok || typeof parsed.value !== 'object' || parsed.value === null) {
    message.error('示例内容无法解析')
    return
  }
  rows.value = Object.entries(parsed.value).map(([key, value]) => ({
    key,
    value: typeof value === 'string' ? value : JSON.stringify(value),
  }))
  emitChange()
  message.success('已填入示例，可按需修改')
}
</script>

<template>
  <div class="wg-kv">
    <div v-for="(row, index) in rows" :key="index" class="wg-kv-row">
      <a-auto-complete
        v-model:value="row.key"
        :options="keySelectOptions"
        :placeholder="keyPlaceholder ?? '参数名'"
        class="wg-kv-key"
        @change="emitChange"
        @blur="emitChange"
      />
      <JsonValueEditor
        :value="row.value"
        :placeholder="valuePlaceholder"
        :options="valueOptions"
        @update:value="(v: string) => { row.value = v; emitChange() }"
      />
      <a-button size="small" type="text" danger @click="removeRow(index)">
        <DeleteOutlined />
      </a-button>
    </div>

    <div v-if="!rows.length && emptyHint" class="wg-kv-empty">
      <span class="wg-muted">{{ emptyHint }}</span>
    </div>

    <div class="wg-kv-actions">
      <a-button size="small" type="dashed" block @click="addRow">
        <PlusOutlined />
        添加一项
      </a-button>
      <a-button v-if="sample" size="small" type="text" @click="applySample">填入示例</a-button>
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
  min-width: 0;
}

.wg-kv-empty {
  padding: 4px 0 8px;
}

.wg-kv-actions {
  display: flex;
  gap: 8px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
