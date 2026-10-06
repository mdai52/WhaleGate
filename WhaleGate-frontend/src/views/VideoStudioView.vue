<script setup lang="ts">
/**
 * 视频工作台：脚本创作 → 视频生成 → 作品库。
 *
 * 与绘图工作台的两点差异：
 *   1. 视频生成是异步任务，提交后只拿到「已排队」，需要按状态轮询进度；
 *   2. 脚本由大模型按结构化 JSON 生成，成品可直接拆成镜头提示词送进视频模型。
 */
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { DeleteOutlined, SendOutlined, ThunderboltOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { videoApi } from '@/api/video'
import { useIsMobile } from '@/composables/useIsMobile'
import type {
  PromptLibrary,
  VideoModel,
  VideoScene,
  VideoScript,
  VideoWork,
} from '@/api/types'

const isMobile = useIsMobile()

// ---------------------------------------------------------------- 模型与词库

const models = ref<VideoModel[]>([])
const modelsLoading = ref(false)

/** 视频生成模型与脚本创作模型分开下拉，避免把视频模型拿去写脚本 */
const videoModels = computed(() => models.value.filter((m) => m.kind === 'video'))
const textModels = computed(() => models.value.filter((m) => m.kind === 'text'))

async function loadModels() {
  modelsLoading.value = true
  try {
    models.value = await videoApi.models()
  } finally {
    modelsLoading.value = false
  }
}

const library = ref<PromptLibrary | null>(null)
const category = ref('')
const keyword = ref('')

async function loadLibrary(cat = '') {
  try {
    library.value = await videoApi.prompts({ category: cat, keyword: keyword.value.trim() })
  } catch {
    library.value = null
  }
}

// ---------------------------------------------------------------- 脚本创作

const scriptForm = reactive({
  model: '',
  topic: '',
  style: '',
  audience: '',
  duration: 30,
  scenes: 5,
  notes: '',
  templateId: 0,
})

const scripting = ref(false)
const currentScript = ref<VideoScript | null>(null)
const scripts = ref<VideoScript[]>([])
const scriptsLoading = ref(false)

async function generateScript() {
  if (!scriptForm.model) {
    message.warning('请选择用于创作脚本的模型')
    return
  }
  if (!scriptForm.topic.trim()) {
    message.warning('请填写视频主题')
    return
  }
  scripting.value = true
  try {
    const script = await videoApi.createScript({
      model: scriptForm.model,
      topic: scriptForm.topic.trim(),
      style: scriptForm.style.trim() || undefined,
      audience: scriptForm.audience.trim() || undefined,
      duration: scriptForm.duration,
      scenes: scriptForm.scenes,
      notes: scriptForm.notes.trim() || undefined,
      template_id: scriptForm.templateId || undefined,
    })
    currentScript.value = script
    await loadScripts()
    message.success(`已生成 ${script.scene_count} 个镜头，消耗 ${script.points} 点`)
  } finally {
    scripting.value = false
  }
}

async function loadScripts() {
  scriptsLoading.value = true
  try {
    const res = await videoApi.listScripts({ page: 1, page_size: 20 })
    scripts.value = res.items
  } finally {
    scriptsLoading.value = false
  }
}

function useTemplate(item: { id: number; title: string; prompt: string }) {
  scriptForm.notes = item.prompt
  scriptForm.topic = scriptForm.topic.trim() || item.title
  scriptForm.templateId = item.id
  activeTab.value = 'script'
  message.success(`已套用「${item.title}」的创作要求`)
}

function removeScript(script: VideoScript) {
  Modal.confirm({
    title: '删除这个脚本？',
    content: `「${script.title}」删除后不可恢复。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await videoApi.removeScript(script.id)
      message.success('已删除')
      if (currentScript.value?.id === script.id) currentScript.value = null
      await loadScripts()
    },
  })
}

// ---------------------------------------------------------------- 视频生成

const videoForm = reactive({
  model: '',
  prompt: '',
  negativePrompt: '',
  seconds: 5,
  size: '',
  aspectRatio: '16:9',
  fps: 0,
  scriptId: 0,
  sceneIndex: 0,
})

const submitting = ref(false)

/** 把脚本镜头送进生成面板：提示词与时长一并带入 */
function useScene(scene: VideoScene, scriptId: number) {
  videoForm.prompt = scene.prompt || scene.visual
  videoForm.seconds = clampSeconds(scene.seconds)
  videoForm.scriptId = scriptId
  videoForm.sceneIndex = scene.index
  activeTab.value = 'generate'
  message.success(`已带入第 ${scene.index} 镜的画面提示词`)
}

function clampSeconds(value: number) {
  if (!value || value < 1) return 5
  if (value > 20) return 20
  return value
}

async function submitVideo() {
  if (!videoForm.model) {
    message.warning('请选择视频生成模型')
    return
  }
  if (!videoForm.prompt.trim()) {
    message.warning('请填写画面提示词')
    return
  }
  submitting.value = true
  try {
    const res = await videoApi.generate({
      model: videoForm.model,
      prompt: videoForm.prompt.trim(),
      negative_prompt: videoForm.negativePrompt.trim() || undefined,
      seconds: videoForm.seconds,
      size: videoForm.size.trim() || undefined,
      aspect_ratio: videoForm.aspectRatio.trim() || undefined,
      fps: videoForm.fps || undefined,
      script_id: videoForm.scriptId || undefined,
      scene_index: videoForm.sceneIndex || undefined,
    })
    message.success(res.message)
    activeTab.value = 'works'
    await loadWorks()
    startPolling()
  } finally {
    submitting.value = false
  }
}

// ---------------------------------------------------------------- 作品库

const works = ref<VideoWork[]>([])
const worksLoading = ref(false)
const worksPagination = reactive({ current: 1, pageSize: 12, total: 0 })

/** 已完成与失败之外的任务需要轮询 */
const hasPending = computed(() =>
  works.value.some((w) => w.status === 0 || w.status === 1),
)

let pollTimer: ReturnType<typeof setInterval> | null = null

function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    if (!hasPending.value) {
      stopPolling()
      return
    }
    await loadWorks()
  }, 4000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function loadWorks() {
  worksLoading.value = true
  try {
    const res = await videoApi.listWorks({
      page: worksPagination.current,
      page_size: worksPagination.pageSize,
    })
    works.value = res.items
    worksPagination.total = res.total
  } finally {
    worksLoading.value = false
  }
}

// 有任务在跑才开始轮询，页面离开立即停掉，避免后台一直打接口
watch(hasPending, (pending) => {
  if (pending) {
    startPolling()
  } else {
    stopPolling()
  }
})

onBeforeUnmount(() => {
  stopPolling()
  for (const url of videoURLs.values()) {
    URL.revokeObjectURL(url)
  }
  videoURLs.clear()
})

/** 需要鉴权的视频取回为 blob 临时地址后缓存 */
const videoURLs = new Map<string, string>()

async function hydrateVideos(list: VideoWork[]) {
  await Promise.all(
    list.flatMap((work) =>
      work.videos.map(async (video, index) => {
        if (!video.need_auth) return
        const key = `${work.id}:${index}`
        const cached = videoURLs.get(key)
        if (cached) {
          video.url = cached
          return
        }
        try {
          const url = await videoApi.fetchWorkVideo(work.id, index)
          videoURLs.set(key, url)
          video.url = url
        } catch {
          // 单段失败不影响其余作品
        }
      }),
    ),
  )
}

watch(works, (list) => {
  void hydrateVideos(list)
})

function removeWork(work: VideoWork) {
  Modal.confirm({
    title: '删除这件作品？',
    content: '托管在服务端的视频会一并清理，删除后不可恢复。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await videoApi.removeWork(work.id)
      message.success('已删除')
      await loadWorks()
    },
  })
}

function statusColor(status: number) {
  switch (status) {
    case 2:
      return 'green'
    case 3:
      return 'red'
    case 1:
      return 'blue'
    default:
      return 'gold'
  }
}

function formatTime(value: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

// ---------------------------------------------------------------- 初始化

const activeTab = ref('script')

loadModels()
void loadLibrary()
void loadScripts()
void loadWorks()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 8px">
        <h2 style="margin: 0">视频工作台</h2>
        <span class="wg-muted">
          脚本创作按 token 计费，视频生成按秒计费；视频为异步任务，提交后在作品库查看进度
        </span>
      </div>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="视频生成需要渠道提供 OpenAI 兼容的 /v1/videos 端点"
        description="左侧词库提供脚本创作模板；先由对话模型产出分镜，再把每个镜头的画面提示词送进视频模型。若模型下拉里没有 video 类型，说明当前渠道未提供视频模型（关键词含 sora / veo / kling / wan 等，或模型目录声明了 video 能力）。"
      />

      <a-tabs v-model:activeKey="activeTab">
        <!-- ------------------------------------------------ 脚本创作 -->
        <a-tab-pane key="script" tab="脚本创作">
          <a-row :gutter="16">
            <a-col v-if="!isMobile" :span="8">
              <div class="wg-lib">
                <a-input
                  v-model:value="keyword"
                  placeholder="搜索脚本模板"
                  allow-clear
                  style="margin-bottom: 8px"
                  @press-enter="loadLibrary(category)"
                />
                <a-space wrap :size="[0, 6]" style="margin-bottom: 8px">
                  <a-tag
                    :color="category === '' ? 'blue' : undefined"
                    style="cursor: pointer"
                    @click="((category = ''), loadLibrary(''))"
                  >
                    全部
                  </a-tag>
                  <a-tag
                    v-for="c in library?.categories ?? []"
                    :key="c.key"
                    :color="category === c.key ? 'blue' : undefined"
                    style="cursor: pointer"
                    @click="((category = c.key), loadLibrary(c.key))"
                  >
                    {{ c.label }}（{{ c.count }}）
                  </a-tag>
                </a-space>
                <div class="wg-lib-list">
                  <div
                    v-for="item in library?.items ?? []"
                    :key="item.id"
                    class="wg-lib-item"
                    @click="useTemplate(item)"
                  >
                    <div class="wg-lib-title">{{ item.title }}</div>
                    <div class="wg-lib-summary">{{ item.summary }}</div>
                  </div>
                  <a-empty v-if="!library?.items?.length" description="暂无脚本模板" />
                </div>
              </div>
            </a-col>

            <a-col :span="isMobile ? 24 : 16">
              <a-form layout="vertical">
                <a-form-item label="创作模型">
                  <a-select
                    v-model:value="scriptForm.model"
                    :loading="modelsLoading"
                    :placeholder="textModels.length ? '选择对话模型' : '暂无可用的对话模型'"
                    show-search
                    :options="textModels.map((m) => ({ value: m.model, label: m.model }))"
                  />
                </a-form-item>
                <a-form-item label="视频主题">
                  <a-textarea
                    v-model:value="scriptForm.topic"
                    :rows="2"
                    show-count
                    :maxlength="500"
                    placeholder="例如：一款面向设计师的云协作工具，30 秒讲清核心价值"
                  />
                </a-form-item>
                <a-row :gutter="12">
                  <a-col :span="12">
                    <a-form-item label="风格">
                      <a-input v-model:value="scriptForm.style" placeholder="如 科技感 / 电影感" allow-clear />
                    </a-form-item>
                  </a-col>
                  <a-col :span="12">
                    <a-form-item label="目标受众">
                      <a-input v-model:value="scriptForm.audience" placeholder="如 设计师 / 学生" allow-clear />
                    </a-form-item>
                  </a-col>
                </a-row>
                <a-row :gutter="12">
                  <a-col :span="12">
                    <a-form-item label="总时长（秒）">
                      <a-input-number v-model:value="scriptForm.duration" :min="5" :max="300" style="width: 100%" />
                    </a-form-item>
                  </a-col>
                  <a-col :span="12">
                    <a-form-item label="镜头数">
                      <a-input-number v-model:value="scriptForm.scenes" :min="1" :max="20" style="width: 100%" />
                    </a-form-item>
                  </a-col>
                </a-row>
                <a-form-item label="补充要求">
                  <a-textarea
                    v-model:value="scriptForm.notes"
                    :rows="3"
                    placeholder="点左侧模板可套用现成的创作要求；也可自行补充"
                  />
                </a-form-item>
                <a-button type="primary" block :loading="scripting" @click="generateScript">
                  <ThunderboltOutlined />
                  生成分镜脚本
                </a-button>
              </a-form>

              <!-- 脚本结果 -->
              <template v-if="currentScript">
                <a-divider orientation="left">《{{ currentScript.title }}》</a-divider>
                <a-space style="margin-bottom: 8px" wrap>
                  <a-tag>{{ currentScript.scene_count }} 镜</a-tag>
                  <a-tag>建议 {{ currentScript.total_seconds }} 秒</a-tag>
                  <a-tag color="volcano">
                    {{ currentScript.self_use ? '应扣' : '已扣' }} {{ currentScript.points }} 点
                  </a-tag>
                </a-space>
                <a-list size="small" :data-source="currentScript.script" bordered>
                  <template #renderItem="{ item }">
                    <a-list-item>
                      <div style="width: 100%">
                        <div class="wg-scene-head">
                          <a-tag>第 {{ (item as VideoScene).index }} 镜</a-tag>
                          <span class="wg-scene-title">
                            {{ (item as VideoScene).title || (item as VideoScene).visual }}
                          </span>
                          <a-tag color="cyan">{{ (item as VideoScene).seconds }}s</a-tag>
                          <a-button
                            size="small"
                            type="link"
                            @click="useScene(item as VideoScene, currentScript!.id)"
                          >
                            用于生成视频
                          </a-button>
                        </div>
                        <div class="wg-muted">{{ (item as VideoScene).visual }}</div>
                        <div v-if="(item as VideoScene).narration" class="wg-muted">
                          旁白：{{ (item as VideoScene).narration }}
                        </div>
                        <div v-if="(item as VideoScene).camera" class="wg-muted">
                          运镜：{{ (item as VideoScene).camera }}
                        </div>
                      </div>
                    </a-list-item>
                  </template>
                </a-list>
              </template>

              <!-- 我的脚本 -->
              <a-divider orientation="left">我的脚本</a-divider>
              <a-spin :spinning="scriptsLoading">
                <a-empty v-if="!scripts.length" description="还没有脚本" />
                <a-list v-else size="small" :data-source="scripts" bordered>
                  <template #renderItem="{ item }">
                    <a-list-item>
                      <div style="width: 100%">
                        <div class="wg-scene-head">
                          <span class="wg-scene-title">{{ (item as VideoScript).title }}</span>
                          <a-tag>{{ (item as VideoScript).scene_count }} 镜</a-tag>
                          <span class="wg-muted">{{ formatTime((item as VideoScript).created_at) }}</span>
                          <a-space>
                            <a-button
                              size="small"
                              type="link"
                              @click="currentScript = item as VideoScript"
                            >
                              查看
                            </a-button>
                            <a-button
                              size="small"
                              type="text"
                              danger
                              @click="removeScript(item as VideoScript)"
                            >
                              <DeleteOutlined />
                            </a-button>
                          </a-space>
                        </div>
                        <div class="wg-muted">{{ (item as VideoScript).brief }}</div>
                      </div>
                    </a-list-item>
                  </template>
                </a-list>
              </a-spin>
            </a-col>
          </a-row>
        </a-tab-pane>

        <!-- ------------------------------------------------ 视频生成 -->
        <a-tab-pane key="generate" tab="视频生成">
          <a-row :gutter="16">
            <a-col :span="isMobile ? 24 : 14">
              <a-form layout="vertical">
                <a-form-item label="视频模型">
                  <a-select
                    v-model:value="videoForm.model"
                    :loading="modelsLoading"
                    :placeholder="videoModels.length ? '选择视频模型' : '暂无可用的视频模型'"
                    show-search
                    :options="videoModels.map((m) => ({ value: m.model, label: m.model }))"
                  />
                </a-form-item>
                <a-form-item label="画面提示词">
                  <a-textarea
                    v-model:value="videoForm.prompt"
                    :rows="4"
                    show-count
                    :maxlength="4000"
                    placeholder="描述一个具体画面：主体、动作、环境、光线、镜头语言、风格"
                  />
                </a-form-item>
                <a-form-item label="不希望出现的元素">
                  <a-input v-model:value="videoForm.negativePrompt" placeholder="部分上游支持" allow-clear />
                </a-form-item>
                <a-row :gutter="12">
                  <a-col :span="8">
                    <a-form-item label="时长（秒）">
                      <a-input-number v-model:value="videoForm.seconds" :min="1" :max="20" style="width: 100%" />
                    </a-form-item>
                  </a-col>
                  <a-col :span="8">
                    <a-form-item label="画幅">
                      <a-select v-model:value="videoForm.aspectRatio" :options="[
                        { value: '16:9', label: '16:9 横屏' },
                        { value: '9:16', label: '9:16 竖屏' },
                        { value: '1:1', label: '1:1 方形' },
                      ]" />
                    </a-form-item>
                  </a-col>
                  <a-col :span="8">
                    <a-form-item label="分辨率">
                      <a-input v-model:value="videoForm.size" placeholder="如 1280x720" allow-clear />
                    </a-form-item>
                  </a-col>
                </a-row>
                <a-button type="primary" block :loading="submitting" @click="submitVideo">
                  <SendOutlined />
                  提交生成
                </a-button>
              </a-form>
            </a-col>
            <a-col :span="isMobile ? 24 : 10">
              <a-alert
                type="warning"
                show-icon
                message="按秒计费，提交即扣"
                description="点数 = 单价（点/秒）× 时长，再乘分组倍率系数。失败会全额回补。生成耗时通常 1-5 分钟，提交后可切到「作品库」查看进度，页面会自动刷新。"
              />
            </a-col>
          </a-row>
        </a-tab-pane>

        <!-- ------------------------------------------------ 作品库 -->
        <a-tab-pane key="works" tab="作品库">
          <a-space style="margin-bottom: 12px" wrap>
            <a-tag v-if="hasPending" color="blue">有任务进行中，自动刷新</a-tag>
            <a-button size="small" :loading="worksLoading" @click="loadWorks">刷新</a-button>
          </a-space>

          <a-spin :spinning="worksLoading">
            <a-empty v-if="!works.length" description="还没有作品，先创作脚本再生成视频吧" />
            <div v-else class="wg-work-grid">
              <article v-for="work in works" :key="work.id" class="wg-work-card">
                <div class="wg-video-box">
                  <video
                    v-if="work.videos.length"
                    :src="work.videos[0].url"
                    controls
                    preload="metadata"
                    class="wg-video"
                  />
                  <div v-else class="wg-video-placeholder">
                    <a-tag :color="statusColor(work.status)">{{ work.status_text }}</a-tag>
                    <a-progress
                      v-if="work.status === 0 || work.status === 1"
                      :percent="work.progress"
                      size="small"
                      style="width: 80%"
                    />
                    <span v-if="work.error_message" class="wg-muted">{{ work.error_message }}</span>
                  </div>
                </div>
                <div class="wg-work-body">
                  <p class="wg-work-prompt" :title="work.prompt">{{ work.prompt }}</p>
                  <div class="wg-work-meta">
                    <a-tag>{{ work.model }}</a-tag>
                    <a-tag :color="statusColor(work.status)">{{ work.status_text }}</a-tag>
                    <a-tag v-if="work.duration_sec">{{ work.duration_sec }}s</a-tag>
                    <a-tag v-if="work.points" color="volcano">
                      {{ work.self_use ? '应扣' : '已扣' }} {{ work.points }} 点
                    </a-tag>
                    <span class="wg-muted">{{ formatTime(work.created_at) }}</span>
                  </div>
                  <div v-if="work.error_message" class="wg-muted">{{ work.error_message }}</div>
                </div>
                <div class="wg-work-foot">
                  <a-button type="text" danger size="small" @click="removeWork(work)">
                    <DeleteOutlined />
                    删除
                  </a-button>
                </div>
              </article>
            </div>
          </a-spin>

          <div v-if="worksPagination.total > worksPagination.pageSize" class="wg-pager">
            <a-pagination
              v-model:current="worksPagination.current"
              v-model:pageSize="worksPagination.pageSize"
              :total="worksPagination.total"
              show-size-changer
              @change="loadWorks"
            />
          </div>
        </a-tab-pane>
      </a-tabs>
    </a-card>
  </div>
</template>

<style scoped>
.wg-lib {
  border: 1px solid #f0f0f0;
  border-radius: 10px;
  padding: 12px;
}

.wg-lib-list {
  max-height: 520px;
  overflow-y: auto;
}

.wg-lib-item {
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.2s;
}

.wg-lib-item:hover {
  background: #fafafa;
}

.wg-lib-title {
  font-weight: 600;
}

.wg-lib-summary {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
  line-height: 1.6;
}

.wg-scene-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 4px;
}

.wg-scene-title {
  font-weight: 600;
}

.wg-work-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.wg-work-card {
  border: 1px solid #f0f0f0;
  border-radius: 10px;
  overflow: hidden;
}

.wg-video-box {
  background: #000;
  aspect-ratio: 16 / 9;
  display: flex;
  align-items: center;
  justify-content: center;
}

.wg-video {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.wg-video-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  color: rgba(255, 255, 255, 0.65);
  font-size: 12px;
  text-align: center;
  padding: 12px;
}

.wg-work-body {
  padding: 12px;
}

.wg-work-prompt {
  margin: 0 0 8px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.wg-work-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.wg-work-foot {
  display: flex;
  justify-content: flex-end;
  padding: 4px 8px;
  border-top: 1px solid #f0f0f0;
}

.wg-pager {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}
</style>
