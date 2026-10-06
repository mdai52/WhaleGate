<script setup lang="ts">
/** 接入指南：把对接所需的信息、示例与注意事项集中在一处，避免散落在多个页面。 */
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CopyOutlined, KeyOutlined, PlayCircleOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import ClientSetup from '@/components/ClientSetup.vue'

const router = useRouter()

const activeTab = ref('start')
const activeLang = ref('python')
const activeParamLang = ref<'python' | 'curl' | 'raw'>('python')
const apiKey = ref('sk-你的密钥')
const model = ref('gpt-4o')

const origin = computed(() => window.location.origin)
// 出口双轨：/api/v1 为统一出口（与 /api Anthropic base 配套），/v1 为兼容旧接入保留
const baseURL = computed(() => `${origin.value}/api/v1`)
const legacyBase = computed(() => `${origin.value}/v1`)
const anthropicBase = computed(() => origin.value)
const geminiBase = computed(() => `${origin.value}/api/v1beta`)

async function copy(text: string, tip = '已复制到剪贴板') {
  try {
    await navigator.clipboard.writeText(text)
    message.success(tip)
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

const codeExamples = computed<Record<string, string>>(() => ({
  python: `from openai import OpenAI

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
    print(chunk.choices[0].delta.content or "", end="")`,

  node: `import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: '${apiKey.value}',
  baseURL: '${baseURL.value}',
})

const resp = await client.chat.completions.create({
  model: '${model.value}',
  messages: [{ role: 'user', content: '你好' }],
})
console.log(resp.choices[0].message.content)`,

  curl: `curl ${baseURL.value}/chat/completions \\
  -H "Authorization: Bearer ${apiKey.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model.value}",
    "messages": [{"role": "user", "content": "你好"}]
  }'`,

  gemini: `curl -X POST "${geminiBase.value}/models/${model.value}:generateContent" \\
  -H "Authorization: Bearer ${apiKey.value}" \\
  -H "Content-Type: application/json" \\
  -d '{"contents":[{"role":"user","parts":[{"text":"你好"}]}]}'

# 流式：把 generateContent 换成 streamGenerateContent 并加 ?alt=sse`,
}))

/** 自定义参数的两种传法对比。 */
const paramExamples = computed<Record<string, string>>(() => ({
  python: `from openai import OpenAI

client = OpenAI(api_key="${apiKey.value}", base_url="${baseURL.value}")

# 方式一：协议已标准化的参数，直接作为具名参数传入
resp = client.chat.completions.create(
    model="${model.value}",
    messages=[{"role": "user", "content": "你好"}],
    temperature=0.7,
    top_p=0.9,
    max_tokens=1024,
)

# 方式二：协议未标准化的参数，用 extra_body 传入
# （直接放在请求体顶层也可以，网关会收进自定义参数统一处理）
resp = client.chat.completions.create(
    model="${model.value}",
    messages=[{"role": "user", "content": "你好"}],
    extra_body={
        "top_k": 40,
        "repetition_penalty": 1.1,
        "seed": 42,
    },
)

# 排查：网关会把分配结果回显在响应头里
print("已生效:", resp.response.headers.get("X-WG-Params-Applied"))
print("被丢弃:", resp.response.headers.get("X-WG-Params-Dropped"))`,

  curl: `# 自定义参数与标准参数一样放在请求体里即可
curl ${baseURL.value}/chat/completions \\
  -H "Authorization: Bearer ${apiKey.value}" \\
  -H "Content-Type: application/json" \\
  -D /dev/stderr \\
  -d '{
    "model": "${model.value}",
    "messages": [{"role": "user", "content": "你好"}],
    "temperature": 0.7,
    "top_k": 40,
    "seed": 42
  }'

# -D /dev/stderr 用于打印响应头，其中包含：
#   X-WG-Params-Applied: temperature,seed
#   X-WG-Params-Dropped: top_k`,

  raw: `{
  "model": "${model.value}",
  "messages": [{"role": "user", "content": "你好"}],
  "temperature": 0.7,
  "top_k": 40,
  "seed": 42,
  "modalities": ["text"],
  "logit_bias": {"1234": -100}
}`,
}))
</script>

<template>
  <div class="wg-page">
    <a-card>
      <template #extra>
        <a-space>
          <a-button @click="router.push('/playground')">
            <PlayCircleOutlined />
            参数调试
          </a-button>
          <a-button @click="router.push('/keys')">
            <KeyOutlined />
            去创建密钥
          </a-button>
        </a-space>
      </template>

      <a-tabs v-model:activeKey="activeTab">
        <!-- ---------------------------------------------------------------- 快速开始 -->
        <a-tab-pane key="start" tab="快速开始">
          <a-alert
            type="info"
            show-icon
            style="margin-bottom: 16px"
            message="三步完成对接"
            description="1. 在「密钥管理」创建 sk- 开头的密钥；2. 确认管理员已在「渠道管理」配置好渠道与模型；3. 把下面的 base_url 与 api_key 换成你的地址与密钥。"
          />

          <a-descriptions bordered :column="1" size="small" style="margin-bottom: 16px">
            <a-descriptions-item label="OpenAI 兼容地址">
              <code>{{ baseURL }}</code>
              <a-button type="link" size="small" @click="copy(baseURL)">
                <CopyOutlined />
              </a-button>
              <div class="wg-hint">
                旧地址 <code>{{ legacyBase }}</code> 同样可用（双轨兼容，无需改动存量接入）。
              </div>
            </a-descriptions-item>
            <a-descriptions-item label="Anthropic SDK 地址（anthropic_base_url）">
              <code>{{ anthropicBase }}</code>
              <a-button type="link" size="small" @click="copy(anthropicBase)">
                <CopyOutlined />
              </a-button>
              <div class="wg-hint">SDK 会自动追加 <code>/v1/messages</code>。</div>
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
            <a-descriptions-item label="模型列表">
              <code>GET {{ baseURL }}/models</code>
              <a-button type="link" size="small" @click="copy(`${baseURL}/models`)">
                <CopyOutlined />
              </a-button>
            </a-descriptions-item>
          </a-descriptions>

          <a-space style="margin-bottom: 12px" wrap>
            <a-input v-model:value="apiKey" addon-before="API Key" style="width: 320px" />
            <a-input v-model:value="model" addon-before="模型" style="width: 240px" />
          </a-space>

          <a-tabs v-model:activeKey="activeLang" size="small">
            <a-tab-pane key="python" tab="Python" />
            <a-tab-pane key="node" tab="Node.js" />
            <a-tab-pane key="curl" tab="cURL" />
            <a-tab-pane key="gemini" tab="Gemini 原生" />
          </a-tabs>
          <pre class="wg-code">{{ codeExamples[activeLang] }}</pre>
          <a-button style="margin-top: 8px" @click="copy(codeExamples[activeLang])">
            <CopyOutlined />
            复制当前示例
          </a-button>
        </a-tab-pane>

        <!-- ---------------------------------------------------------------- 自定义参数 -->
        <a-tab-pane key="params" tab="自定义参数">
          <a-alert
            type="info"
            show-icon
            style="margin-bottom: 16px"
            message="自定义参数会自动识别并按上游能力分配"
            description="不同上游支持的参数并不一致。网关会统一参数名、按协议映射到上游对应字段，能力表之外的参数默认原样透传，由上游决定是否接受；只有管理员显式排除的参数才会被拦下。"
          />

          <h3>网关的处理流程</h3>
          <a-steps direction="vertical" size="small" style="margin: 12px 0 20px">
            <a-step
              title="参数名归一"
              description="topK / top-k / max_output_tokens 等写法统一为 top_k、max_tokens 等标准名。"
            />
            <a-step
              title="能力协商"
              description="按渠道声明的 supported 过滤；未声明则使用该协议的默认支持集，再扣除 exclude。"
            />
            <a-step
              title="协议映射"
              description="写入上游对应字段：OpenAI 渠道写 temperature，Gemini 渠道写 generationConfig.temperature。"
            />
            <a-step
              title="结果回显"
              description="分配结果通过 X-WG-Params-Applied 与 X-WG-Params-Dropped 两个响应头返回，并写入调用日志。"
            />
          </a-steps>

          <h3>传参方式</h3>
          <ul class="wg-list">
            <li><b>标准参数</b>：直接作为具名参数传入，如 <code>temperature</code>、<code>top_p</code>、<code>max_tokens</code>。</li>
            <li>
              <b>自定义参数</b>：放在请求体顶层即可；使用 OpenAI SDK 时也可以放进
              <code>extra_body</code>，效果相同。
            </li>
            <li><b>值类型</b>：数字、布尔、字符串、数组、对象都支持，网关按原样传递（必要时按目标协议转换）。</li>
          </ul>

          <a-segmented
            v-model:value="activeParamLang"
            :options="[
              { label: 'Python', value: 'python' },
              { label: 'cURL', value: 'curl' },
              { label: '原始请求体', value: 'raw' },
            ]"
            style="margin: 12px 0"
          />
          <pre class="wg-code">{{ paramExamples[activeParamLang ?? 'python'] }}</pre>
          <a-button style="margin-top: 8px" @click="copy(paramExamples[activeParamLang ?? 'python'])">
            <CopyOutlined />
            复制当前示例
          </a-button>

          <a-alert
            style="margin-top: 20px"
            type="success"
            show-icon
            message="想先试再写代码？"
            description="打开「参数调试」，选择模型、勾选参数即可预览哪些会生效、哪些会被丢弃，以及最终发往上游的请求体长什么样。预览不消耗额度。"
          >
            <template #action>
              <a-button size="small" type="primary" @click="router.push('/playground')">去调试</a-button>
            </template>
          </a-alert>
        </a-tab-pane>

        <!-- ---------------------------------------------------------------- 客户端接入 -->
        <a-tab-pane key="clients" tab="客户端接入">
          <ClientSetup :base-u-r-l="origin" :api-key="apiKey" :model="model" />
        </a-tab-pane>

        <!-- ---------------------------------------------------------------- 常见问题 -->
        <a-tab-pane key="faq" tab="常见问题">
          <a-collapse>
            <a-collapse-panel key="1" header="支持哪些客户端？">
              兼容 OpenAI 协议的客户端与 SDK 接入时，替换 <code>base_url</code> 与
              <code>api_key</code> 即可；Gemini 原生客户端可用
              <code>/v1beta/models/{model}:generateContent</code>；
              Claude Code、CCSwitch 这类只认 Anthropic Messages API 的工具，
              可直接把地址指向本网关的 <code>/v1/messages</code>。具体填法见「客户端接入」。
            </a-collapse-panel>
            <a-collapse-panel key="2" header="传了参数但上游没生效？">
              网关默认<b>透传</b>自定义参数：能力表之外的参数会原样发给上游，由上游决定是否接受，
              不支持时上游的报错会原样返回给你。只有管理员在「渠道管理 → 参数能力声明」里显式
              <code>exclude</code> 的参数才会被网关拦下（响应头 <code>X-WG-Params-Dropped</code> 可见）。
              想恢复"先过滤再转发"的旧行为，可在渠道的参数能力声明里加
              <code>{"passthrough": false}</code>，或把 <code>gateway.param_passthrough</code> 配成 false。
            </a-collapse-panel>
            <a-collapse-panel key="3" header="如何查看可用模型？">
              调用 <code>GET {{ baseURL }}/models</code>；该列表汇总了所有启用渠道暴露的模型与别名。
            </a-collapse-panel>
            <a-collapse-panel key="4" header="流式调用怎么用？">
              在请求中加 <code>"stream": true</code>，网关以 SSE 逐块返回，格式与 OpenAI 完全一致；
              Gemini 原生入口使用 <code>:streamGenerateContent?alt=sse</code>。
            </a-collapse-panel>
            <a-collapse-panel key="5" header="返回了 402 / 429 怎么办？">
              <code>402</code> 表示余额不足，请在概览页查看额度或联系管理员充值；
              <code>429</code> 表示触发限流，可能是密钥的 QPM / 并发上限，也可能是上游自身限流，
              可在「调用历史」按状态筛选查看具体原因。
            </a-collapse-panel>
            <a-collapse-panel key="6" header="密钥泄露了怎么办？">
              立即到「密钥管理」吊销该密钥，吊销后立刻失效。建议为每个应用分配独立密钥，
              便于单独限流、统计与吊销。
            </a-collapse-panel>
            <a-collapse-panel key="7" header="如何知道自己的调用记录？">
              在「调用历史」可按模型、状态与时间范围筛选，查看每次调用的 token、点数、耗时与
              参数应用明细；管理员还可以在「全局日志」看到全部账号的调用。
            </a-collapse-panel>
            <a-collapse-panel key="8" header="返回 401「凭证无效」，是不是模型选错了？">
              不是。401 表示这串密钥在网关里查不到，与所选模型无关——请求在鉴权阶段就被拦下，
              还没走到渠道选择。最常见的原因是复制了「密钥管理」列表里打码后的展示值
              （形如 <code>sk-Ab3x...****</code>），那串只用于辨认是哪一个密钥，不能用于调用；
              其次可能是密钥已被删除，或这串密钥来自其它网关。
              完整明文只在创建成功那一刻展示一次，丢失后无法找回，重新创建即可。
            </a-collapse-panel>
          </a-collapse>
        </a-tab-pane>
      </a-tabs>
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

.wg-list {
  margin: 8px 0;
  padding-left: 20px;
  line-height: 2;
}

.wg-hint {
  margin-top: 2px;
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}

code {
  font-size: 12px;
}

h3 {
  margin: 20px 0 4px;
  font-size: 15px;
}
</style>
