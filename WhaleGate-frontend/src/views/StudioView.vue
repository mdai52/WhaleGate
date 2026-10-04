<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { DeleteOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import dayjs from 'dayjs'
import { studioApi } from '@/api/studio'
import type { ImageWork, PromptCategory, PromptTemplate, StudioImage, StudioModel } from '@/api/types'

// ---------------------------------------------------------------- 常量

/** 与后端 studio.max_images_per_request 默认值保持一致。 */
const MAX_IMAGES = 4

const SIZE_OPTIONS = [
  { value: '1024x1024' },
  { value: '1024x1536' },
  { value: '1536x1024' },
  { value: '768x1024' },
  { value: '1024x768' },
  { value: '512x512' },
]

const QUALITY_OPTIONS = [
  { value: 'high' },
  { value: 'medium' },
  { value: 'low' },
  { value: 'standard' },
  { value: 'hd' },
]

/** 分类强调色，按分类顺序取用，保证同一分类颜色稳定。 */
const CATEGORY_COLORS = [
  '#023b82',
  '#13c2c2',
  '#722ed1',
  '#fa8c16',
  '#eb2f96',
  '#2f54eb',
  '#52c41a',
  '#08979c',
  '#faad14',
  '#8c8c8c',
]

// ---------------------------------------------------------------- 状态

const activeTab = ref<'create' | 'works'>('create')

const modelsLoading = ref(false)
const models = ref<StudioModel[]>([])
const modelOptions = computed(() =>
  models.value.map((m) => ({ value: m.model, label: `${m.model}（${m.channel}）` })),
)

const libraryLoading = ref(false)
const categories = ref<PromptCategory[]>([])
const templates = ref<PromptTemplate[]>([])
const activeCategory = ref('')
const keyword = ref('')

const form = reactive({
  model: '',
  prompt: '',
  negativePrompt: '',
  n: 1,
  size: '1024x1024',
  quality: 'high',
  templateId: 0,
})

const generating = ref(false)
const latest = ref<ImageWork | null>(null)

const worksLoading = ref(false)
const works = ref<ImageWork[]>([])
const pagination = reactive({ current: 1, pageSize: 12, total: 0 })

const preview = reactive({ open: false, src: '' })

/**
 * 作品图片托管在服务端且需要登录态，<img> 无法直接携带 Authorization，
 * 因此统一取回为 blob 临时地址并按 key 缓存，卸载时回收。
 */
const imageURLs = new Map<string, string>()

// ---------------------------------------------------------------- 数据加载

async function loadModels() {
  modelsLoading.value = true
  try {
    models.value = await studioApi.models()
    if (!form.model && models.value.length) {
      form.model = models.value[0].model
    }
  } catch {
    models.value = []
  } finally {
    modelsLoading.value = false
  }
}

async function loadLibrary() {
  libraryLoading.value = true
  try {
    const library = await studioApi.prompts({
      category: activeCategory.value || undefined,
      keyword: keyword.value.trim() || undefined,
    })
    categories.value = library.categories
    templates.value = library.items
  } finally {
    libraryLoading.value = false
  }
}

async function loadWorks() {
  worksLoading.value = true
  try {
    const res = await studioApi.works({ page: pagination.current, page_size: pagination.pageSize })
    works.value = res.items
    pagination.total = res.total
    await hydrateImages(res.items)
  } finally {
    worksLoading.value = false
  }
}

/** 把需要鉴权的图片替换为可直接渲染的临时地址。 */
async function hydrateImages(list: ImageWork[]) {
  await Promise.all(
    list.flatMap((work) =>
      work.images.map(async (img, index) => {
        if (!img.need_auth) return
        const key = `${work.id}:${index}`
        const cached = imageURLs.get(key)
        if (cached) {
          img.url = cached
          return
        }
        try {
          const url = await studioApi.fetchWorkImage(work.id, index)
          imageURLs.set(key, url)
          img.url = url
        } catch {
          // 单张失败不影响其余图片与整个页面
        }
      }),
    ),
  )
}

onMounted(() => {
  loadModels()
  loadLibrary()
})

onBeforeUnmount(() => {
  imageURLs.forEach((url) => URL.revokeObjectURL(url))
  imageURLs.clear()
})

watch(activeTab, (tab) => {
  if (tab === 'works') loadWorks()
})

// ---------------------------------------------------------------- 词库交互

const totalTemplates = computed(() => categories.value.reduce((sum, c) => sum + c.count, 0))

function categoryColor(key: string) {
  const index = categories.value.findIndex((c) => c.key === key)
  return CATEGORY_COLORS[(index < 0 ? 0 : index) % CATEGORY_COLORS.length]
}

function selectCategory(key: string) {
  activeCategory.value = activeCategory.value === key ? '' : key
  loadLibrary()
}

function applyTemplate(template: PromptTemplate) {
  form.prompt = template.prompt
  form.templateId = template.id
  if (template.model_hint && models.value.some((m) => m.model === template.model_hint)) {
    form.model = template.model_hint
  }
  if (activeTab.value !== 'create') activeTab.value = 'create'
  message.success(`已套用「${template.title}」`)
}

// ---------------------------------------------------------------- 生成

async function generate() {
  if (!form.model) {
    message.warning('请先选择生图模型')
    return
  }
  if (!form.prompt.trim()) {
    message.warning('请填写提示词')
    return
  }
  generating.value = true
  try {
    const work = await studioApi.generate({
      model: form.model,
      prompt: form.prompt.trim(),
      negative_prompt: form.negativePrompt.trim() || undefined,
      n: form.n,
      size: form.size.trim() || undefined,
      quality: form.quality.trim() || undefined,
      template_id: form.templateId || undefined,
    })
    await hydrateImages([work])
    latest.value = work
    message.success(`生成完成，用时 ${(work.latency_ms / 1000).toFixed(1)} 秒`)
  } finally {
    generating.value = false
  }
}

function downloadImage(img: StudioImage, index: number) {
  const link = document.createElement('a')
  link.href = img.url
  link.download = `whalegate-${Date.now()}-${index + 1}.png`
  link.click()
}

// ---------------------------------------------------------------- 作品库

function onWorksPageChange(page: number, pageSize: number) {
  pagination.current = page
  pagination.pageSize = pageSize
  loadWorks()
}

function openPreview(url: string) {
  preview.src = url
  preview.open = true
}

function reuse(work: ImageWork) {
  form.prompt = work.prompt
  if (models.value.some((m) => m.model === work.model)) form.model = work.model
  if (typeof work.params?.size === 'string') form.size = work.params.size
  activeTab.value = 'create'
  message.success('已复用到创作面板')
}

function removeWork(work: ImageWork) {
  Modal.confirm({
    title: '删除这件作品？',
    content: '托管在服务端的图片会一并清理，删除后不可恢复。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await studioApi.removeWork(work.id)
      message.success('已删除')
      if (latest.value?.id === work.id) latest.value = null
      await loadWorks()
    },
  })
}

function formatTime(value: string) {
  return dayjs(value).format('MM-DD HH:mm')
}
</script>

<template>
  <div class="wg-page wg-studio">
    <a-tabs v-model:activeKey="activeTab">
      <a-tab-pane key="create" tab="创作">
        <a-row :gutter="[16, 16]">
          <a-col :xs="24" :lg="16">
            <a-card :bordered="false" class="wg-panel">
              <template #title>
                <div class="wg-panel-title">
                  <span>词库</span>
                  <span class="wg-muted">{{ totalTemplates }} 条提示词</span>
                </div>
              </template>
              <template #extra>
                <a-input-search
                  v-model:value="keyword"
                  placeholder="搜索提示词"
                  allow-clear
                  style="width: 200px"
                  @search="loadLibrary"
                />
              </template>

              <div class="wg-chip-row">
                <a-checkable-tag :checked="!activeCategory" @change="() => selectCategory('')">
                  全部 {{ totalTemplates }}
                </a-checkable-tag>
                <a-checkable-tag
                  v-for="c in categories"
                  :key="c.key"
                  :checked="activeCategory === c.key"
                  @change="() => selectCategory(c.key)"
                >
                  {{ c.label }} <span class="wg-muted">{{ c.count }}</span>
                </a-checkable-tag>
              </div>

              <a-spin :spinning="libraryLoading">
                <a-empty v-if="!templates.length" description="没有匹配的提示词" />
                <div v-else class="wg-prompt-grid">
                  <article v-for="t in templates" :key="t.id" class="wg-prompt-card">
                    <div class="wg-prompt-accent" :style="{ background: categoryColor(t.category) }" />
                    <div class="wg-prompt-body">
                      <div class="wg-prompt-head">
                        <a-tag :color="categoryColor(t.category)">{{ t.category_label }}</a-tag>
                        <a-tag v-if="t.aspect_ratio">{{ t.aspect_ratio }}</a-tag>
                      </div>
                      <h4 class="wg-prompt-title">{{ t.title }}</h4>
                      <p class="wg-prompt-summary">{{ t.summary }}</p>
                      <div class="wg-prompt-tags">
                        <a-tag v-for="tag in t.tags" :key="tag" class="wg-plain-tag">{{ tag }}</a-tag>
                      </div>
                    </div>
                    <div class="wg-prompt-foot">
                      <a-button type="primary" ghost block @click="applyTemplate(t)">使用</a-button>
                    </div>
                  </article>
                </div>
              </a-spin>
            </a-card>
          </a-col>

          <a-col :xs="24" :lg="8">
            <a-card :bordered="false" class="wg-panel" title="创作">
              <a-form layout="vertical">
                <a-form-item label="模型" required>
                  <a-select
                    v-model:value="form.model"
                    :options="modelOptions"
                    :loading="modelsLoading"
                    :placeholder="models.length ? '选择生图模型' : '暂无可用生图模型'"
                    show-search
                    allow-clear
                  />
                  <div v-if="!modelsLoading && !models.length" class="wg-hint">
                    暂无生图模型。请在「渠道管理」中配置 OpenAI 兼容或 Gemini 原生渠道，
                    并登记生图模型（如 gpt-image-2、gemini-2.5-flash-image）。
                  </div>
                </a-form-item>

                <a-form-item label="提示词" required>
                  <a-textarea
                    v-model:value="form.prompt"
                    :rows="7"
                    :maxlength="8000"
                    show-count
                    placeholder="描述画面：主体、场景、光线、材质、构图、风格…"
                  />
                </a-form-item>

                <a-form-item label="负向提示词">
                  <a-input
                    v-model:value="form.negativePrompt"
                    placeholder="不希望出现的元素，部分上游支持"
                    allow-clear
                  />
                </a-form-item>

                <a-row :gutter="12">
                  <a-col :span="8">
                    <a-form-item label="数量">
                      <a-input-number
                        v-model:value="form.n"
                        :min="1"
                        :max="MAX_IMAGES"
                        style="width: 100%"
                      />
                    </a-form-item>
                  </a-col>
                  <a-col :span="16">
                    <a-form-item label="尺寸">
                      <a-auto-complete
                        v-model:value="form.size"
                        :options="SIZE_OPTIONS"
                        placeholder="如 1024x1024"
                      />
                    </a-form-item>
                  </a-col>
                </a-row>

                <a-form-item label="质量">
                  <a-auto-complete
                    v-model:value="form.quality"
                    :options="QUALITY_OPTIONS"
                    placeholder="如 high / medium / low"
                    allow-clear
                  />
                </a-form-item>

                <a-button
                  type="primary"
                  block
                  size="large"
                  :loading="generating"
                  :disabled="!models.length"
                  @click="generate"
                >
                  <template #icon><ThunderboltOutlined /></template>
                  生成图片
                </a-button>
              </a-form>
            </a-card>

            <a-card v-if="latest" :bordered="false" class="wg-panel wg-latest" title="本次结果">
              <div class="wg-result-grid">
                <figure v-for="(img, i) in latest.images" :key="i" class="wg-result-item">
                  <img :src="img.url" :alt="latest.prompt" loading="lazy" @click="openPreview(img.url)" />
                  <figcaption>
                    <a-button type="link" size="small" @click="downloadImage(img, i)">下载</a-button>
                  </figcaption>
                </figure>
              </div>
            </a-card>
          </a-col>
        </a-row>
      </a-tab-pane>

      <a-tab-pane key="works" tab="作品库">
        <div class="wg-works-head">
          <span class="wg-muted">共 {{ pagination.total }} 件作品</span>
          <a-button size="small" @click="loadWorks">
            <template #icon><ReloadOutlined /></template>
            刷新
          </a-button>
        </div>

        <a-spin :spinning="worksLoading">
          <a-empty v-if="!works.length" description="还没有作品，去「创作」生成第一张吧" />
          <div v-else class="wg-work-grid">
            <article v-for="work in works" :key="work.id" class="wg-work-card">
              <div class="wg-work-images" :class="`wg-n-${Math.min(work.images.length, 4)}`">
                <img
                  v-for="(img, i) in work.images"
                  :key="i"
                  :src="img.url"
                  :alt="work.prompt"
                  loading="lazy"
                  @click="openPreview(img.url)"
                />
              </div>
              <div class="wg-work-body">
                <p class="wg-work-prompt" :title="work.prompt">{{ work.prompt }}</p>
                <div class="wg-work-meta">
                  <a-tag>{{ work.model }}</a-tag>
                  <span class="wg-muted">{{ formatTime(work.created_at) }}</span>
                </div>
              </div>
              <div class="wg-work-foot">
                <a-button type="text" size="small" @click="reuse(work)">复用到创作</a-button>
                <a-button type="text" danger size="small" @click="removeWork(work)">
                  <DeleteOutlined />
                </a-button>
              </div>
            </article>
          </div>
        </a-spin>

        <div v-if="pagination.total > pagination.pageSize" class="wg-works-page">
          <a-pagination
            :current="pagination.current"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            :show-size-changer="false"
            @change="onWorksPageChange"
          />
        </div>
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:open="preview.open" :footer="null" :width="900" centered>
      <img :src="preview.src" alt="预览" style="width: 100%; display: block; border-radius: 8px" />
    </a-modal>
  </div>
</template>

<style scoped>
.wg-panel {
  border-radius: 8px;
}

.wg-panel + .wg-panel {
  margin-top: 16px;
}

.wg-panel-title {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
  font-weight: 400;
}

.wg-hint {
  margin-top: 6px;
  line-height: 1.6;
}

/* ---------------- 词库 */

.wg-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}

.wg-prompt-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
}

.wg-prompt-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fff;
  border: 1px solid #eef0f4;
  border-radius: 8px;
  transition: box-shadow 0.2s, transform 0.2s;
}

.wg-prompt-card:hover {
  box-shadow: 0 6px 20px rgba(2, 59, 130, 0.1);
  transform: translateY(-2px);
}

.wg-prompt-accent {
  height: 4px;
}

.wg-prompt-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  padding: 14px 14px 0;
}

.wg-prompt-head {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.wg-prompt-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
}

.wg-prompt-summary {
  flex: 1;
  margin: 0;
  color: rgba(0, 0, 0, 0.6);
  font-size: 12px;
  line-height: 1.6;
}

.wg-prompt-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.wg-plain-tag {
  margin: 0;
  background: #f5f7fa;
  border-color: transparent;
  color: rgba(0, 0, 0, 0.55);
}

.wg-prompt-foot {
  padding: 12px 14px;
}

/* ---------------- 生成结果 */

.wg-result-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
}

.wg-result-item {
  margin: 0;
}

.wg-result-item img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 8px;
  cursor: zoom-in;
}

.wg-result-item figcaption {
  text-align: right;
}

/* ---------------- 作品库 */

.wg-works-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.wg-work-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 16px;
}

.wg-work-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fff;
  border: 1px solid #eef0f4;
  border-radius: 8px;
}

.wg-work-images {
  display: grid;
  gap: 2px;
  background: #f5f7fa;
}

.wg-work-images img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  cursor: zoom-in;
}

.wg-n-1 {
  grid-template-columns: 1fr;
}

.wg-n-2,
.wg-n-3,
.wg-n-4 {
  grid-template-columns: 1fr 1fr;
}

.wg-n-1 img {
  aspect-ratio: 4 / 3;
}

.wg-n-3 img:first-child {
  grid-row: span 2;
}

.wg-work-body {
  flex: 1;
  padding: 12px 14px 0;
}

.wg-work-prompt {
  display: -webkit-box;
  overflow: hidden;
  margin: 0 0 8px;
  font-size: 13px;
  line-height: 1.5;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.wg-work-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.wg-work-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px 8px;
}

.wg-works-page {
  display: flex;
  justify-content: center;
  margin-top: 20px;
}

@media (max-width: 576px) {
  .wg-prompt-grid,
  .wg-work-grid {
    grid-template-columns: 1fr;
  }
}
</style>
