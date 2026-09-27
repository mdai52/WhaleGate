<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import KeyValueEditor from './KeyValueEditor.vue'

/** 常见统一参数预置项 */
const COMMON_PARAMS = [
  'temperature',
  'top_p',
  'max_tokens',
  'stop',
  'frequency_penalty',
  'presence_penalty',
  'seed',
  'response_format',
  'tools',
  'tool_choice',
  'parallel_tool_calls',
  'reasoning_effort',
  'image_size',
  'image_quality',
]

export interface ParamSchemaValue {
  supported?: string[]
  exclude?: string[]
  defaults?: Record<string, unknown>
}

const props = defineProps<{
  modelValue: ParamSchemaValue
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: ParamSchemaValue): void }>()

const supported = ref<string[]>([])
const exclude = ref<string[]>([])
const defaults = ref<Record<string, string>>({})

/** 对象值转字符串以便编辑 */
function serializeDefaults(defaults?: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(defaults ?? {})) {
    out[k] = typeof v === 'string' ? v : JSON.stringify(v)
  }
  return out
}

watch(
  () => props.modelValue,
  (next) => {
    supported.value = [...(next?.supported ?? [])]
    exclude.value = [...(next?.exclude ?? [])]
    const incoming = serializeDefaults(next?.defaults)
    if (JSON.stringify(incoming) !== JSON.stringify(defaults.value)) {
      defaults.value = incoming
    }
  },
  { immediate: true, deep: true },
)

const supportedOptions = computed(() =>
  Array.from(new Set([...COMMON_PARAMS, ...supported.value])).map((v) => ({ value: v })),
)
const excludeOptions = computed(() =>
  Array.from(new Set([...COMMON_PARAMS, ...exclude.value])).map((v) => ({ value: v })),
)

function emitChange() {
  const out: ParamSchemaValue = {}
  if (supported.value.length) {
    out.supported = supported.value
  }
  if (exclude.value.length) {
    out.exclude = exclude.value
  }
  const defaults: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(defaults.value ?? {})) {
    if (k.trim()) {
      defaults[k.trim()] = parseScalar(v)
    }
  }
  if (Object.keys(defaults).length) {
    out.defaults = defaults
  }
  emit('update:modelValue', out)
}

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
</script>

<template>
  <div class="wg-schema">
    <div class="wg-schema-field">
      <div class="wg-schema-label">
        支持的参数
        <span class="wg-muted">留空 = 使用协议默认支持集</span>
      </div>
      <a-select
        v-model:value="supported"
        mode="tags"
        :options="supportedOptions"
        placeholder="选择或输入统一参数名，如 temperature"
        style="width: 100%"
        @change="emitChange"
      />
    </div>

    <div class="wg-schema-field">
      <div class="wg-schema-label">
        排除的参数
        <span class="wg-muted">明确不支持，优先级高于上方</span>
      </div>
      <a-select
        v-model:value="exclude"
        mode="tags"
        :options="excludeOptions"
        placeholder="选择或输入要禁用的参数"
        style="width: 100%"
        @change="emitChange"
      />
    </div>

    <div class="wg-schema-field">
      <div class="wg-schema-label">
        默认值
        <span class="wg-muted">用户未传参时使用</span>
      </div>
      <KeyValueEditor
        v-model="defaults"
        key-placeholder="参数名"
        value-placeholder="默认值，支持数字 / 布尔 / JSON"
        @update:model-value="emitChange"
      />
    </div>
  </div>
</template>

<style scoped>
.wg-schema-field {
  margin-bottom: 12px;
}

.wg-schema-label {
  margin-bottom: 4px;
  font-weight: 500;
}
</style>
