<script setup lang="ts">
/**
 * 高级配置项的「填写说明」：用途、格式、可直接套用的示例。
 *
 * 渠道高级配置涉及模型映射、参数注入、能力声明等概念，纯靠字段名很难理解，
 * 因此把说明与示例收在一个弹层里，示例可一键填入编辑器。
 */
import { QuestionCircleOutlined } from '@ant-design/icons-vue'

defineProps<{
  /** 说明标题 */
  title: string
  /** 用途说明，逐条陈述 */
  points: string[]
  /** 示例文本（JSON），有值时展示并提供一键填入 */
  example?: string
  /** 示例下方的补充说明 */
  exampleNote?: string
}>()

const emit = defineEmits<{ (e: 'apply', example: string): void }>()
</script>

<template>
  <a-popover placement="rightTop" trigger="click" :overlay-style="{ maxWidth: '420px' }">
    <template #title>{{ title }}</template>
    <template #content>
      <div class="wg-hint">
        <ul class="wg-hint-points">
          <li v-for="(point, index) in points" :key="index">{{ point }}</li>
        </ul>
        <div v-if="example" class="wg-hint-example">
          <div class="wg-hint-example-head">
            <span>示例</span>
            <a-button size="small" type="link" @click="emit('apply', example)">填入示例</a-button>
          </div>
          <pre>{{ example }}</pre>
          <div v-if="exampleNote" class="wg-hint-note">{{ exampleNote }}</div>
        </div>
      </div>
    </template>
    <QuestionCircleOutlined class="wg-hint-icon" />
  </a-popover>
</template>

<style scoped>
.wg-hint-icon {
  margin-left: 4px;
  color: rgba(0, 0, 0, 0.35);
  cursor: pointer;
}

.wg-hint-icon:hover {
  color: #023b82;
}

.wg-hint-points {
  margin: 0;
  padding-left: 18px;
  line-height: 1.8;
}

.wg-hint-example {
  margin-top: 10px;
}

.wg-hint-example-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
  color: rgba(0, 0, 0, 0.65);
  font-weight: 500;
}

.wg-hint-example pre {
  max-height: 220px;
  margin: 0;
  padding: 8px 10px;
  overflow: auto;
  background: #f5f7fa;
  border-radius: 6px;
  font-family: 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
}

.wg-hint-note {
  margin-top: 6px;
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
