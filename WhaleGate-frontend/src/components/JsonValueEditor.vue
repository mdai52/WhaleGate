<script setup lang="ts">
/**
 * JSON 值编辑弹窗：把挤在单行输入框里的结构化值展开成多行编辑。
 *
 * 高级配置里的注入值经常是对象或数组（如 tools、imageConfig），
 * 一行输入框既看不清也没法校验，因此提供格式化与语法检查。
 */
import { computed, ref, watch } from 'vue'
import { AlignLeftOutlined, ClearOutlined } from '@ant-design/icons-vue'
import { parseJSON } from '@/utils/json'

const props = defineProps<{
  /** 当前值（字符串形态） */
  value: string
  /** 弹窗标题 */
  title?: string
  placeholder?: string
  /** 值的候选，例如上游已登记的模型名；有值时输入框变为可搜索下拉 */
  options?: string[]
}>()

const emit = defineEmits<{ (e: 'update:value', v: string): void }>()

const open = ref(false)
const draft = ref('')
const error = ref('')

const selectOptions = computed(() => (props.options ?? []).map((v) => ({ value: v })))

/** 缩进美化；非法 JSON 原样返回，避免把用户输入改坏。 */
function prettify(text: string): string {
  const trimmed = (text ?? '').trim()
  if (!trimmed) {
    return ''
  }
  const result = parseJSON(trimmed, null)
  return result.ok ? JSON.stringify(result.value, null, 2) : text
}

/** 校验草稿并回写错误提示。 */
function validate() {
  const trimmed = draft.value.trim()
  if (!trimmed) {
    error.value = ''
    return
  }
  const result = parseJSON(trimmed, null)
  error.value = result.ok ? '' : result.error ?? 'JSON 格式错误'
}

function format() {
  validate()
  if (!error.value) {
    draft.value = prettify(draft.value)
  }
}

function clear() {
  draft.value = ''
  error.value = ''
}

function confirm() {
  validate()
  if (error.value) {
    return
  }
  emit('update:value', prettify(draft.value))
  open.value = false
}

// 每次打开都从外部值重新载入，避免中途改过外部值后不同步。
watch(open, (next) => {
  if (next) {
    draft.value = prettify(props.value)
    error.value = ''
  }
})

/** 值看起来是对象或数组时，提示用户展开编辑更易读。 */
const looksStructured = ref(false)
watch(
  () => props.value,
  (next) => {
    const trimmed = (next ?? '').trim()
    looksStructured.value = trimmed.length > 24 && (trimmed.startsWith('{') || trimmed.startsWith('['))
  },
  { immediate: true },
)
</script>

<template>
  <div class="wg-json-value">
    <a-auto-complete
      v-if="selectOptions.length"
      :value="value"
      :options="selectOptions"
      :placeholder="placeholder ?? '值'"
      class="wg-json-input"
      @update:value="(v: string) => emit('update:value', v)"
    />
    <a-input
      v-else
      :value="value"
      :placeholder="placeholder ?? '值'"
      class="wg-json-input"
      @update:value="(v: string) => emit('update:value', v)"
    />
    <a-tooltip title="展开编辑 JSON（自动格式化并校验）">
      <a-button size="small" type="text" class="wg-json-btn" @click="open = true">
        <AlignLeftOutlined />
      </a-button>
    </a-tooltip>
    <a-tag v-if="looksStructured" class="wg-json-tag">结构化</a-tag>

    <a-modal
      v-model:open="open"
      :title="title ?? '编辑 JSON 值'"
      :width="640"
      ok-text="应用"
      cancel-text="取消"
      @ok="confirm"
    >
      <a-textarea
        v-model:value="draft"
        :rows="14"
        class="wg-json-textarea"
        placeholder='支持对象 / 数组，例如 {"type":"image_generation","size":"1024x1024"}'
        @blur="validate"
      />
      <div class="wg-json-toolbar">
        <a-space>
          <a-button size="small" @click="format">
            <AlignLeftOutlined />
            格式化
          </a-button>
          <a-button size="small" @click="clear">
            <ClearOutlined />
            清空
          </a-button>
        </a-space>
        <span v-if="error" class="wg-json-error">格式错误：{{ error }}</span>
        <span v-else-if="draft.trim()" class="wg-json-ok">格式正确</span>
        <span v-else class="wg-muted">留空表示不设置该值</span>
      </div>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-json-value {
  display: flex;
  flex: 1;
  gap: 4px;
  align-items: center;
  min-width: 0;
}

.wg-json-input {
  flex: 1;
  min-width: 0;
}

.wg-json-btn {
  flex: none;
  color: rgba(0, 0, 0, 0.45);
}

.wg-json-tag {
  flex: none;
  margin: 0;
  background: #e8f0fb;
  border-color: transparent;
  color: #023b82;
}

.wg-json-textarea {
  font-family: 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
}

.wg-json-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
}

.wg-json-error {
  color: #cf1322;
  font-size: 12px;
}

.wg-json-ok {
  color: #389e0d;
  font-size: 12px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
