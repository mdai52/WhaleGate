<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ThunderboltOutlined } from '@ant-design/icons-vue'
import { apiKeyApi } from '@/api/apikey'
import { usageApi } from '@/api/usage'
import { useUserStore } from '@/stores/user'
import UsageChart from '@/components/UsageChart.vue'
import type { APIKeyItem, UsagePoint } from '@/api/types'

const userStore = useUserStore()

const loading = ref(false)
const keys = ref<APIKeyItem[]>([])
const points = ref<UsagePoint[]>([])
const days = ref(7)

/** 1 点 = 0.001 元，与后端 constant.PointsPerYuan 保持一致。 */
const POINTS_PER_YUAN = 1000

const remainingYuan = computed(() => ((userStore.profile?.quota ?? 0) / POINTS_PER_YUAN).toFixed(3))
const usedYuan = computed(() => ((userStore.profile?.used_quota ?? 0) / POINTS_PER_YUAN).toFixed(3))
const activeKeyCount = computed(() => keys.value.filter((k) => k.status === 1 && !k.revoked_at).length)

const totalTokens = computed(() =>
  points.value.reduce((acc, p) => acc + p.prompt_tokens + p.completion_tokens, 0),
)
const totalRequests = computed(() => points.value.reduce((acc, p) => acc + p.requests, 0))

async function loadUsage() {
  loading.value = true
  try {
    points.value = await usageApi.summary(days.value)
  } finally {
    loading.value = false
  }
}

async function loadKeys() {
  try {
    const res = await apiKeyApi.list({ page: 1, page_size: 20 })
    keys.value = res.items
  } catch {
    keys.value = []
  }
}

onMounted(async () => {
  if (!userStore.profile) {
    await userStore.fetchProfile().catch(() => undefined)
  }
  await Promise.all([loadUsage(), loadKeys()])
})

watch(days, loadUsage)
</script>

<template>
  <div class="wg-page">
    <a-row :gutter="[16, 16]">
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card :loading="!userStore.profile">
          <a-statistic title="剩余额度（元）" :value="remainingYuan" :precision="3">
            <template #prefix><ThunderboltOutlined /></template>
          </a-statistic>
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card :loading="!userStore.profile">
          <a-statistic title="累计消耗（元）" :value="usedYuan" :precision="3" />
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card :loading="loading">
          <a-statistic :title="`近 ${days} 天请求`" :value="totalRequests" />
        </a-card>
      </a-col>
      <a-col :xs="12" :sm="12" :lg="6">
        <a-card :loading="loading">
          <a-statistic :title="`近 ${days} 天 Token`" :value="totalTokens" />
        </a-card>
      </a-col>
    </a-row>

    <a-card style="margin-top: 16px">
      <div class="wg-card-title" style="margin-bottom: 12px">
        <h3 style="margin: 0">用量趋势</h3>
        <a-segmented
          v-model:value="days"
          :options="[
            { label: '近 7 天', value: 7 },
            { label: '近 30 天', value: 30 },
          ]"
        />
      </div>
      <a-empty v-if="!loading && points.length === 0" description="暂无用量数据" />
      <UsageChart v-else :points="points" :height="320" />
    </a-card>

    <a-card style="margin-top: 16px" title="快速接入">
      <a-alert
        type="info"
        show-icon
        message="使用 OpenAI 兼容协议调用"
        description="将 base_url 指向本网关的 /v1，并在 Authorization 中传入 sk- 开头的密钥；所有接入的模型都会以 OpenAI 格式返回。"
        style="margin-bottom: 12px"
      />
      <pre class="wg-code">curl http://&lt;gateway&gt;/v1/chat/completions \
  -H "Authorization: Bearer sk-你的密钥" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"你好"}]}'</pre>
      <a-descriptions :column="1" size="small" bordered style="margin-top: 12px">
        <a-descriptions-item label="有效密钥数">{{ activeKeyCount }}</a-descriptions-item>
        <a-descriptions-item label="Gemini 原生入口">
          /v1beta/models/{model}:generateContent
        </a-descriptions-item>
        <a-descriptions-item label="自定义参数">
          可直接传顶层字段或使用 extra_body，网关按渠道声明的能力自动分配
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<style scoped>
.wg-code {
  background: #f5f5f5;
  padding: 12px;
  border-radius: 8px;
  overflow-x: auto;
  font-size: 12px;
  margin: 0;
}
</style>
