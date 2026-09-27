<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CopyOutlined, KeyOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'

const router = useRouter()

const activeTab = ref('python')
const apiKey = ref('sk-你的密钥')
const model = ref('gpt-4o')

const baseURL = computed(() => `${window.location.origin}/v1`)
const geminiBase = computed(() => `${window.location.origin}/v1beta`)

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success('已复制到剪贴板')
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

const curlCode = computed(() => `curl ${baseURL.value}/chat/completions \\
  -H "Authorization: Bearer ${apiKey.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model.value}",
    "messages": [{"role": "user", "content": "你好"}]
  }'`)

const pythonCode = computed(() => `from openai import OpenAI

client = OpenAI(
    api_key="${apiKey.value}",
    base_url="${baseURL.value}",
)

resp = client.chat.completions.create(
    model="${model.value}",
    messages=[{"role": "user", "content": "你好"}],
)
print(resp.choices[0].message.content)

# 流式
stream = client.chat.completions.create(
    model="${model.value}",
    messages=[{"role": "user", "content": "你好"}],
    stream=True,
)
for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")`)

const nodeCode = computed(() => `import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: '${apiKey.value}',
  baseURL: '${baseURL.value}',
})

const resp = await client.chat.completions.create({
  model: '${model.value}',
  messages: [{ role: 'user', content: '你好' }],
})
console.log(resp.choices[0].message.content)`)

const geminiCode = computed(() => `curl -X POST "${geminiBase.value}/models/${model.value}:generateContent" \\
  -H "Authorization: Bearer ${apiKey.value}" \\
  -H "Content-Type: application/json" \\
  -d '{"contents":[{"role":"user","parts":[{"text":"你好"}]}]}'

# 流式：把 generateContent 换成 streamGenerateContent 并加 ?alt=sse`)

const codeMap = computed<Record<string, string>>(() => ({
  curl: curlCode.value,
  python: pythonCode.value,
  node: nodeCode.value,
  gemini: geminiCode.value,
}))
</script>

<template>
  <div class="wg-page">
    <a-card title="接入指南">
      <template #extra>
        <a-button @click="router.push('/keys')">
          <KeyOutlined />
          去创建密钥
        </a-button>
      </template>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="三步完成对接"
        description="1. 在「密钥管理」生成 sk- 开头的 API Key；2. 确认管理员已在「渠道管理」配置好渠道与模型；3. 把下面示例中的 base_url 与 api_key 换成你的地址与密钥即可调用。"
      />

      <a-descriptions bordered :column="1" size="small" style="margin-bottom: 16px">
        <a-descriptions-item label="OpenAI 兼容地址">
          <code>{{ baseURL }}</code>
          <a-button type="link" size="small" @click="copy(baseURL)">
            <CopyOutlined />
          </a-button>
        </a-descriptions-item>
        <a-descriptions-item label="Gemini 原生地址">
          <code>{{ geminiBase }}</code>
          <a-button type="link" size="small" @click="copy(geminiBase)">
            <CopyOutlined />
          </a-button>
        </a-descriptions-item>
        <a-descriptions-item label="鉴权方式">
          <code>Authorization: Bearer sk-xxx</code>
        </a-descriptions-item>
      </a-descriptions>

      <a-space style="margin-bottom: 12px" wrap>
        <a-input v-model:value="apiKey" addon-before="API Key" style="width: 320px" />
        <a-input v-model:value="model" addon-before="模型" style="width: 240px" />
      </a-space>

      <a-tabs v-model:activeKey="activeTab">
        <a-tab-pane key="python" tab="Python">
          <pre class="wg-code">{{ codeMap.python }}</pre>
        </a-tab-pane>
        <a-tab-pane key="node" tab="Node.js">
          <pre class="wg-code">{{ codeMap.node }}</pre>
        </a-tab-pane>
        <a-tab-pane key="curl" tab="cURL">
          <pre class="wg-code">{{ codeMap.curl }}</pre>
        </a-tab-pane>
        <a-tab-pane key="gemini" tab="Gemini 原生">
          <pre class="wg-code">{{ codeMap.gemini }}</pre>
        </a-tab-pane>
      </a-tabs>

      <a-button style="margin-top: 8px" @click="copy(codeMap[activeTab])">
        <CopyOutlined />
        复制当前示例
      </a-button>
    </a-card>

    <a-card title="常见问题" style="margin-top: 16px">
      <a-collapse>
        <a-collapse-panel key="1" header="支持哪些客户端？">
          任何兼容 OpenAI 协议（<code>/v1/chat/completions</code>）的 SDK 都能直接接入，只需替换
          <code>base_url</code> 与 <code>api_key</code>；Gemini 原生客户端可用
          <code>/v1beta/models/{model}:generateContent</code>。
        </a-collapse-panel>
        <a-collapse-panel key="2" header="如何查看可用模型？">
          调用 <code>GET {{ baseURL }}/models</code> 即可列出当前账号可用的模型，或在管理端「渠道管理」查看已配置的模型。
        </a-collapse-panel>
        <a-collapse-panel key="3" header="流式调用怎么用？">
          在请求中加 <code>"stream": true</code>，网关会以 SSE 逐块返回，格式与 OpenAI 完全一致；Gemini 原生入口使用
          <code>:streamGenerateContent?alt=sse</code>。
        </a-collapse-panel>
        <a-collapse-panel key="4" header="余额不足或被限流怎么办？">
          余额不足返回 <code>402</code>，请在用户中心查看额度或联系管理员充值；超过 QPM 限制返回 <code>429</code>，可在「密钥管理」查看每个密钥的限流配置。
        </a-collapse-panel>
        <a-collapse-panel key="5" header="密钥泄露了怎么办？">
          立即到「密钥管理」吊销该密钥，吊销后立即失效；建议为不同应用分配不同密钥，便于单独限流与统计。
        </a-collapse-panel>
      </a-collapse>
    </a-card>
  </div>
</template>

<style scoped>
.wg-code {
  margin: 0;
  padding: 16px;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
  white-space: pre;
}

code {
  font-size: 12px;
}
</style>
