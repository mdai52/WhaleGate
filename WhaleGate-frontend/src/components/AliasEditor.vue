<script setup lang="ts">
import { ref, watch } from 'vue'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import KeyValueEditor from './KeyValueEditor.vue'

export interface AliasEntry {
  alias: string
  model: string
  override: Record<string, string>
}

interface AliasValue {
  model?: string
  override?: Record<string, unknown>
}

const props = defineProps<{
  /** 别名配置：别名 -> { model, override } */
  modelValue: Record<string, AliasValue>
  /** 上游模型候选（来自渠道登记或自动探测） */
  modelOptions?: string[]
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, AliasValue>): void }>()

const rows = ref<AliasEntry[]>([])

function toRows(obj: Record<string, AliasValue>): AliasEntry[] {
  return Object.entries(obj ?? {}).map(([alias, cfg]) => ({
    alias,
    model: cfg?.model ?? '',
    override: serializeOverride(cfg?.override),
  }))
}

/** 把对象值转成字符串以便在 KV 编辑器中编辑 */
function serializeOverride(override?: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(override ?? {})) {
    out[k] = typeof v === 'string' ? v : JSON.stringify(v)
  }
  return out
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
  const out: Record<string, { model?: string; override?: Record<string, unknown> }> = {}
  for (const row of rows.value) {
    const alias = row.alias.trim()
    if (!alias) {
      continue
    }
    const entry: { model?: string; override?: Record<string, unknown> } = {}
    if (row.model.trim() && row.model.trim() !== alias) {
      entry.model = row.model.trim()
    }
    const override: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(row.override ?? {})) {
      if (k.trim()) {
        override[k.trim()] = parseScalar(v)
      }
    }
    if (Object.keys(override).length) {
      entry.override = override
    }
    out[alias] = entry
  }
  emit('update:modelValue', out)
}

/** 简单类型推断：数字 / 布尔 / JSON，其余按字符串 */
function parseScalar(v: string): unknown {
  const t = v.trim()
  if (t === 'true') return true
  if (t === 'false') return false
  if (t !== '' && !Number.isNaN(Number(t))) return Number(t)
  if (t.startsWith('{') || t.startsWith('[')) {
    try {
      return JSON.parse(t)
    } catch {
      return v
    }
  }
  return v
}

function addRow() {
  rows.value.push({ alias: '', model: '', override: {} })
}

function removeRow(index: number) {
  rows.value.splice(index, 1)
  emitChange()
}
</script>

<template>
  <div class="wg-alias">
    <div v-for="(row, index) in rows" :key="index" class="wg-alias-item">
      <div class="wg-alias-head">
        <a-input
          v-model:value="row.alias"
          placeholder="对外别名，例如 img-2k"
          class="wg-alias-name"
          @change="emitChange"
        />
        <span class="wg-muted">→</span>
        <a-auto-complete
          v-model:value="row.model"
          :options="(modelOptions ?? []).map((m) => ({ value: m }))"
          placeholder="上游模型（留空 = 同名）"
          class="wg-alias-model"
          @change="emitChange"
        />
        <a-button size="small" type="text" danger @click="removeRow(index)">
          <DeleteOutlined />
        </a-button>
      </div>
      <div class="wg-alias-override">
        <div class="wg-muted" style="font-size: 12px; margin-bottom: 4px">
          该别名专属参数注入（选填），例如强制注入 tools 或修改分辨率：
        </div>
        <KeyValueEditor
          v-model="row.override"
          key-placeholder="参数名，如 image_size"
          value-placeholder="值，支持数字 / 布尔 / JSON"
          @update:model-value="emitChange"
        />
      </div>
    </div>
    <a-button size="small" type="dashed" block @click="addRow">
      <PlusOutlined />
      添加别名
    </a-button>
  </div>
</template>

<style scoped>
.wg-alias-item {
  border: 1px solid #f0f0f0;
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 12px;
}

.wg-alias-head {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.wg-alias-name {
  flex: 2;
}

.wg-alias-model {
  flex: 3;
}
</style>
