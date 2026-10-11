<template>
  <div class="cv-page">
    <!-- 左：画布列表 -->
    <aside class="cv-side">
      <div class="cv-side-head">
        <span class="cv-side-title">我的画布</span>
        <a-button size="small" type="primary" @click="createCanvas">
          <PlusOutlined />
          新建
        </a-button>
      </div>
      <a-spin :spinning="loadingList">
        <div v-if="canvases.length === 0" class="cv-empty-list">还没有画布</div>
        <div
          v-for="item in canvases"
          :key="item.id"
          class="cv-item"
          :class="{ active: current?.id === item.id }"
          @click="openCanvas(item.id)"
        >
          <div class="cv-item-main">
            <div class="cv-item-name">{{ item.name }}</div>
            <div class="cv-item-meta">{{ item.width }} × {{ item.height }}</div>
          </div>
          <a-popconfirm title="删除这张画布？导出记录会一并清理" @confirm="removeCanvas(item.id)">
            <a-button size="small" type="text" danger @click.stop>
              <DeleteOutlined />
            </a-button>
          </a-popconfirm>
        </div>
      </a-spin>
    </aside>

    <!-- 中：画布 -->
    <main class="cv-main">
      <div class="cv-toolbar">
        <a-segmented v-model:value="tool" :options="toolOptions" size="small" />
        <span class="cv-divider" />
        <a-upload :show-upload-list="false" accept="image/*" :before-upload="beforeImage">
          <a-button size="small">
            <PictureOutlined />
            插入图片
          </a-button>
        </a-upload>
        <a-button size="small" type="primary" ghost :loading="generating" @click="aiOpen = true">
          <RobotOutlined />
          AI 生成
        </a-button>
        <span class="cv-divider" />
        <a-button size="small" danger :disabled="!selected" @click="removeSelected">
          <DeleteOutlined />
          删除元素
        </a-button>
        <span class="cv-flex" />
        <a-input v-model:value="name" size="small" class="cv-name" placeholder="画布名称" @change="dirty = true" />
        <a-button size="small" type="primary" :loading="saving" :disabled="!dirty" @click="save">
          <SaveOutlined />
          保存
        </a-button>
      </div>

      <div class="cv-stage">
        <canvas
          ref="canvasRef"
          class="cv-canvas"
          :style="{ aspectRatio: `${width} / ${height}` }"
          @mousedown="onDown"
          @mousemove="onMove"
          @mouseup="onUp"
          @mouseleave="onUp"
        />
        <div v-if="nodes.length === 0" class="cv-hint">
          <PictureOutlined style="font-size: 28px" />
          <p>从上方选择工具开始绘制，或用「AI 生成」插入图片</p>
        </div>
      </div>
    </main>

    <!-- 右：属性与导出 -->
    <aside class="cv-side cv-right">
      <div class="cv-section">
        <div class="cv-section-title">画布</div>
        <div class="cv-grid2">
          <label class="cv-field">
            <span>宽</span>
            <a-input-number v-model:value="width" :min="16" :max="4096" size="small" @change="onEdit" />
          </label>
          <label class="cv-field">
            <span>高</span>
            <a-input-number v-model:value="height" :min="16" :max="4096" size="small" @change="onEdit" />
          </label>
        </div>
        <label class="cv-field">
          <span>背景</span>
          <div class="cv-color">
            <input v-model="background" type="color" @change="onEdit" />
            <a-input v-model:value="background" size="small" @change="onEdit" />
          </div>
        </label>
      </div>

      <div class="cv-section">
        <div class="cv-section-title">元素属性</div>
        <template v-if="selected">
          <div class="cv-grid2">
            <label class="cv-field"><span>X</span>
              <a-input-number v-model:value="selected.x" size="small" @change="onEdit" />
            </label>
            <label class="cv-field"><span>Y</span>
              <a-input-number v-model:value="selected.y" size="small" @change="onEdit" />
            </label>
            <label v-if="selected.type !== 'text'" class="cv-field"><span>宽</span>
              <a-input-number v-model:value="selected.w" :min="1" size="small" @change="onEdit" />
            </label>
            <label v-if="selected.type !== 'text'" class="cv-field"><span>高</span>
              <a-input-number v-model:value="selected.h" :min="1" size="small" @change="onEdit" />
            </label>
          </div>
          <label class="cv-field">
            <span>填充</span>
            <div class="cv-color">
              <input v-model="selected.fill" type="color" @change="onEdit" />
              <a-input v-model:value="selected.fill" size="small" @change="onEdit" />
            </div>
          </label>
          <label v-if="selected.type !== 'text'" class="cv-field">
            <span>描边</span>
            <div class="cv-color">
              <input v-model="selected.stroke" type="color" @change="onEdit" />
              <a-input v-model:value="selected.stroke" size="small" @change="onEdit" />
            </div>
          </label>
          <div class="cv-grid2">
            <label class="cv-field"><span>线宽</span>
              <a-input-number v-model:value="selected.stroke_width" :min="0.5" :step="0.5" size="small" @change="onEdit" />
            </label>
            <label v-if="selected.type === 'rect'" class="cv-field"><span>圆角</span>
              <a-input-number v-model:value="selected.radius" :min="0" size="small" @change="onEdit" />
            </label>
            <label v-if="selected.type === 'text'" class="cv-field"><span>字号</span>
              <a-input-number v-model:value="selected.font_size" :min="6" size="small" @change="onEdit" />
            </label>
            <label v-if="selected.type !== 'image'" class="cv-field"><span>不透明度</span>
              <a-input-number v-model:value="selected.opacity" :min="0.05" :max="1" :step="0.05" size="small" @change="onEdit" />
            </label>
          </div>
          <label v-if="selected.type === 'text'" class="cv-field">
            <span>文字内容</span>
            <a-textarea v-model:value="selected.text" :rows="3" @change="onEdit" />
          </label>
        </template>
        <div v-else class="cv-empty-hint">选中画布上的元素后可调整属性</div>
      </div>

      <div class="cv-section">
        <div class="cv-section-title">导出</div>
        <div class="cv-grid2">
          <label class="cv-field">
            <span>格式</span>
            <a-select v-model:value="exportFormat" size="small">
              <a-select-option value="png">PNG</a-select-option>
              <a-select-option value="jpeg">JPEG</a-select-option>
            </a-select>
          </label>
          <label class="cv-field">
            <span>倍数</span>
            <a-select v-model:value="exportScale" size="small">
              <a-select-option :value="1">1x</a-select-option>
              <a-select-option :value="2">2x</a-select-option>
              <a-select-option :value="3">3x</a-select-option>
            </a-select>
          </label>
        </div>
        <a-button
          block
          type="primary"
          style="margin-top: 10px"
          :loading="exporting"
          :disabled="!current"
          @click="submitExport"
        >
          <ExportOutlined />
          导出（服务端渲染）
        </a-button>

        <div v-if="exports.length" class="cv-exports">
          <div v-for="item in exports" :key="item.id" class="cv-export">
            <div class="cv-export-main">
              <span>{{ item.format.toUpperCase() }} {{ item.scale }}x · {{ exportStatusText(item.status) }}</span>
              <a-progress v-if="item.status === 0 || item.status === 1" :percent="item.progress" size="small" />
              <div v-if="item.error_message" class="cv-error">{{ item.error_message }}</div>
            </div>
            <a-button v-if="item.status === 2" size="small" type="link" @click="download(item.id)">下载</a-button>
          </div>
        </div>
      </div>
    </aside>

    <!-- AI 生图 -->
    <a-modal v-model:open="aiOpen" title="AI 生成并插入画布" :footer="null" width="460px">
      <a-form layout="vertical" size="small">
        <a-form-item label="模型" required>
          <a-select
            v-model:value="ai.model"
            :options="aiModels.map((m) => ({ label: m.model, value: m.model }))"
            placeholder="选择生图模型"
            show-search
          />
        </a-form-item>
        <a-form-item required>
          <template #label>
            <span>提示词</span>
            <a-tooltip title="写几个词也行，AI 会帮你补全成完整的画面描述（走对话渠道按 token 计费）">
              <a-button
                size="small"
                type="link"
                :loading="enhancing"
                :disabled="!ai.prompt.trim()"
                style="margin-left: 8px; padding: 0"
                @click="enhancePrompt"
              >
                <ThunderboltOutlined />
                AI 优化
              </a-button>
            </a-tooltip>
          </template>
          <a-textarea
            v-model:value="ai.prompt"
            :rows="3"
            placeholder="描述你想生成的画面；只写几个词也可以，点「AI 优化」自动补全"
          />
        </a-form-item>
        <a-form-item label="尺寸">
          <a-select v-model:value="ai.size" :options="sizeOptions" />
        </a-form-item>
        <a-alert
          v-if="ai.error"
          type="error"
          :message="ai.error"
          show-icon
          style="margin-bottom: 12px"
        />
        <a-button block type="primary" :loading="generating" @click="generateAndInsert">
          <RobotOutlined />
          生成并插入
        </a-button>
        <div class="wg-muted" style="margin-top: 8px">
          生成走绘图工作台的渠道并按张计费，结果作为图片元素插入画布。
        </div>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import {
  DeleteOutlined,
  ExportOutlined,
  PictureOutlined,
  PlusOutlined,
  RobotOutlined,
  SaveOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons-vue'
import { canvasApi, exportStatusText } from '@/api/canvas'
import { studioApi } from '@/api/studio'
import { videoApi } from '@/api/video'
import type { CanvasExport, CanvasNode, CanvasNodeType, CanvasRecord, StudioModel, VideoModel } from '@/api/types'

type Tool = 'select' | CanvasNodeType

const toolOptions = [
  { label: '选择', value: 'select' },
  { label: '矩形', value: 'rect' },
  { label: '椭圆', value: 'ellipse' },
  { label: '折线', value: 'line' },
  { label: '文字', value: 'text' },
]

const sizeOptions = ['1024x1024', '1024x1792', '1792x1024', '512x512'].map((s) => ({ label: s, value: s }))

// ---------------------------------------------------------------- 状态

const canvases = ref<CanvasRecord[]>([])
const current = ref<CanvasRecord | null>(null)
const loadingList = ref(false)
const saving = ref(false)
const dirty = ref(false)
const name = ref('')
const background = ref('#FFFFFF')
const width = ref(1280)
const height = ref(720)

const nodes = ref<CanvasNode[]>([])
const selectedId = ref('')
const tool = ref<Tool>('select')
const canvasRef = ref<HTMLCanvasElement | null>(null)

const exportFormat = ref<'png' | 'jpeg'>('png')
const exportScale = ref(1)
const exporting = ref(false)
const exports = ref<CanvasExport[]>([])
let pollTimer: number | undefined

const aiOpen = ref(false)
const generating = ref(false)
const aiModels = ref<StudioModel[]>([])
const ai = reactive({ model: '', prompt: '', size: '1024x1024', error: '' })

const selected = computed(() => nodes.value.find((n) => n.id === selectedId.value) ?? null)

// ---------------------------------------------------------------- 列表与画布

async function loadList() {
  loadingList.value = true
  try {
    const res = await canvasApi.list({ page: 1, page_size: 50 })
    canvases.value = res.items ?? []
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载画布失败')
  } finally {
    loadingList.value = false
  }
}

async function createCanvas() {
  try {
    const created = await canvasApi.create({ name: '未命名画布', width: 1280, height: 720 })
    await loadList()
    await openCanvas(created.id)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '新建失败')
  }
}

async function openCanvas(id: number) {
  try {
    const detail = await canvasApi.get(id)
    applyRecord(detail)
    await nextTick()
    draw()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '打开画布失败')
  }
}

function applyRecord(detail: CanvasRecord) {
  current.value = detail
  name.value = detail.name
  width.value = detail.width
  height.value = detail.height
  background.value = detail.background || '#FFFFFF'
  nodes.value = detail.payload?.nodes ?? []
  selectedId.value = ''
  dirty.value = false
}

async function removeCanvas(id: number) {
  try {
    await canvasApi.remove(id)
    if (current.value?.id === id) {
      current.value = null
      nodes.value = []
    }
    await loadList()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '删除失败')
  }
}

async function save() {
  if (!current.value) return
  saving.value = true
  try {
    const saved = await canvasApi.update(current.value.id, {
      name: name.value,
      width: width.value,
      height: height.value,
      background: background.value,
      payload: { nodes: nodes.value, width: width.value, height: height.value },
    })
    current.value = saved
    dirty.value = false
    message.success('已保存')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    saving.value = false
  }
}

// ---------------------------------------------------------------- 绘制

/** 前端预览的绘制顺序与服务端渲染保持一致：按下标从下到上 */
function draw() {
  const el = canvasRef.value
  if (!el) return
  el.width = width.value
  el.height = height.value
  const ctx = el.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, el.width, el.height)
  ctx.fillStyle = background.value
  ctx.fillRect(0, 0, el.width, el.height)

  for (const n of nodes.value) {
    if (n.hidden) continue
    ctx.save()
    ctx.globalAlpha = n.opacity && n.opacity > 0 && n.opacity < 1 ? n.opacity : 1
    if (n.rotation) {
      ctx.translate(n.x + (n.w ?? 0) / 2, n.y + (n.h ?? 0) / 2)
      ctx.rotate((n.rotation * Math.PI) / 180)
      ctx.translate(-(n.w ?? 0) / 2, -(n.h ?? 0) / 2)
    }
    switch (n.type) {
      case 'rect':
        drawRect(ctx, n)
        break
      case 'ellipse':
        ctx.beginPath()
        ctx.ellipse(n.x + (n.w ?? 0) / 2, n.y + (n.h ?? 0) / 2, (n.w ?? 0) / 2, (n.h ?? 0) / 2, 0, 0, Math.PI * 2)
        paint(ctx, n)
        break
      case 'line': {
        const pts = n.points?.length ? n.points : [{ x: n.x, y: n.y }, { x: n.x + (n.w ?? 0), y: n.y + (n.h ?? 0) }]
        ctx.beginPath()
        ctx.moveTo(pts[0].x, pts[0].y)
        pts.slice(1).forEach((p) => ctx.lineTo(p.x, p.y))
        if (n.fill) {
          ctx.closePath()
          ctx.fillStyle = n.fill
          ctx.fill()
        }
        if (n.stroke) {
          ctx.strokeStyle = n.stroke
          ctx.lineWidth = n.stroke_width ?? 1
          ctx.stroke()
        }
        break
      }
      case 'text':
        ctx.fillStyle = n.color || n.fill || '#000000'
        ctx.font = `${n.font_size ?? 16}px sans-serif`
        ctx.textBaseline = 'top'
        ctx.fillText(n.text ?? '', n.x, n.y)
        break
      case 'image': {
        const img = n.src ? imageCache.get(n.src) : undefined
        if (img) ctx.drawImage(img, n.x, n.y, n.w || img.width, n.h || img.height)
        break
      }
    }
    ctx.restore()
  }
  // 选中框画在最上层，不参与导出
  const sel = selected.value
  if (sel) {
    ctx.save()
    ctx.strokeStyle = '#1677ff'
    ctx.lineWidth = 1.5
    ctx.setLineDash([5, 3])
    ctx.strokeRect(sel.x - 3, sel.y - 3, (sel.w ?? 0) + 6, (sel.h ?? 0) + 6)
    ctx.restore()
  }
}

function drawRect(ctx: CanvasRenderingContext2D, n: CanvasNode) {
  const r = Math.min(n.radius ?? 0, Math.min(n.w ?? 0, n.h ?? 0) / 2)
  ctx.beginPath()
  if (r > 0 && ctx.roundRect) {
    ctx.roundRect(n.x, n.y, n.w ?? 0, n.h ?? 0, r)
  } else {
    ctx.rect(n.x, n.y, n.w ?? 0, n.h ?? 0)
  }
  paint(ctx, n)
}

function paint(ctx: CanvasRenderingContext2D, n: CanvasNode) {
  if (n.fill) {
    ctx.fillStyle = n.fill
    ctx.fill()
  }
  if (n.stroke) {
    ctx.strokeStyle = n.stroke
    ctx.lineWidth = n.stroke_width ?? 1
    ctx.stroke()
  }
}

// 内嵌图片解码后缓存，避免每次重绘都重新解码 data URI
const imageCache = new Map<string, HTMLImageElement>()

function cacheImage(src: string, onReady?: (img: HTMLImageElement) => void) {
  if (!src) return
  const existing = imageCache.get(src)
  if (existing) {
    onReady?.(existing)
    return
  }
  const img = new Image()
  img.onload = () => {
    draw()
    onReady?.(img)
  }
  img.src = src
  imageCache.set(src, img)
}

// ---------------------------------------------------------------- AI 生图

async function loadAIModels() {
  try {
    aiModels.value = await studioApi.models()
    if (!ai.model && aiModels.value.length > 0) {
      ai.model = aiModels.value[0].model
    }
  } catch {
    // 没配生图渠道时下拉为空，点生成时会提示
  }
}

// 「AI 优化」用的对话模型：复用视频工作台的模型列表（kind = text）
const textModels = ref<VideoModel[]>([])
const enhancing = ref(false)

async function loadTextModels() {
  try {
    textModels.value = (await videoApi.models()).filter((m) => m.kind === 'text')
  } catch {
    // 没有对话模型时按钮点了会提示
  }
}

/** 把简短想法扩写成完整生图提示词，结果直接填回输入框可继续手改 */
async function enhancePrompt() {
  const idea = ai.prompt.trim()
  if (!idea) {
    message.warning('先写几个词，哪怕是「一只猫」')
    return
  }
  if (textModels.value.length === 0) {
    message.warning('没有可用的对话模型（需要在渠道里配置非生图/非视频的模型）')
    return
  }
  enhancing.value = true
  ai.error = ''
  try {
    const { prompt } = await studioApi.enhancePrompt({
      model: textModels.value[0].model,
      prompt: idea,
    })
    ai.prompt = prompt
    message.success('已优化提示词，可继续手动修改')
  } catch (error) {
    ai.error = error instanceof Error ? error.message : '优化失败'
  } finally {
    enhancing.value = false
  }
}

/** 生成后以图片节点插入画布中央 */
async function generateAndInsert() {
  if (!ai.model) {
    ai.error = '请先选择生图模型（需要在渠道里配置支持生图的模型）'
    return
  }
  if (!ai.prompt.trim()) {
    ai.error = '请填写提示词'
    return
  }
  generating.value = true
  ai.error = ''
  try {
    const [w, h] = ai.size.split('x').map(Number)
    const work = await studioApi.generate({
      model: ai.model,
      prompt: ai.prompt,
      n: 1,
      size: ai.size,
    })
    const src = await studioApi.fetchWorkImageDataUrl(work.id, 0)
    cacheImage(src, (img) => {
      // 按生成尺寸等比缩放到画布内，避免一张 1024 的图铺满整个画布
      const maxW = Math.min(w, width.value * 0.6)
      const scale = maxW / (img.naturalWidth || w)
      pushNode({
        id: `n${Date.now()}`,
        type: 'image',
        x: Math.max(8, (width.value - maxW) / 2),
        y: Math.max(8, (height.value - (img.naturalHeight || h) * scale) / 2),
        w: maxW,
        h: (img.naturalHeight || h) * scale,
        src,
      })
    })
    aiOpen.value = false
    message.success('已插入画布')
  } catch (error) {
    ai.error = error instanceof Error ? error.message : '生成失败'
  } finally {
    generating.value = false
  }
}

function pushNode(node: CanvasNode) {
  nodes.value.push(node)
  selectedId.value = node.id
  dirty.value = true
  draw()
}

// ---------------------------------------------------------------- 交互

let dragging: { id: string; dx: number; dy: number } | null = null
let drawing: CanvasNode | null = null

function toLocal(e: MouseEvent) {
  const el = canvasRef.value!
  const rect = el.getBoundingClientRect()
  return {
    x: ((e.clientX - rect.left) * el.width) / rect.width,
    y: ((e.clientY - rect.top) * el.height) / rect.height,
  }
}

function hitTest(x: number, y: number): CanvasNode | null {
  // 从上层往下找，与视觉叠放顺序一致
  for (let i = nodes.value.length - 1; i >= 0; i--) {
    const n = nodes.value[i]
    if (n.hidden) continue
    if (x >= n.x && x <= n.x + (n.w ?? 0) && y >= n.y && y <= n.y + (n.h ?? 0)) return n
  }
  return null
}

function onDown(e: MouseEvent) {
  const p = toLocal(e)
  if (tool.value === 'select') {
    const hit = hitTest(p.x, p.y)
    selectedId.value = hit?.id ?? ''
    if (hit) dragging = { id: hit.id, dx: p.x - hit.x, dy: p.y - hit.y }
    draw()
    return
  }
  if (tool.value === 'text') {
    pushNode({
      id: `n${Date.now()}`,
      type: 'text',
      x: p.x,
      y: p.y,
      w: 200,
      h: 24,
      text: '双击右侧属性修改文字',
      font_size: 20,
      color: '#000000',
    })
    return
  }
  drawing = {
    id: `n${Date.now()}`,
    type: tool.value as CanvasNodeType,
    x: p.x,
    y: p.y,
    w: 1,
    h: 1,
    fill: tool.value === 'line' ? '' : '#1677ff',
    stroke: '#1677ff',
    stroke_width: 2,
    radius: 0,
  }
  nodes.value.push(drawing)
}

function onMove(e: MouseEvent) {
  const p = toLocal(e)
  if (dragging) {
    const node = nodes.value.find((n) => n.id === dragging!.id)
    if (node) {
      node.x = Math.round(p.x - dragging.dx)
      node.y = Math.round(p.y - dragging.dy)
      dirty.value = true
      draw()
    }
    return
  }
  if (drawing) {
    drawing.w = Math.max(1, Math.round(p.x - drawing.x))
    drawing.h = Math.max(1, Math.round(p.y - drawing.y))
    dirty.value = true
    draw()
  }
}

function onUp() {
  if (drawing) {
    // 误触产生的极小元素直接丢弃，避免画布里堆满看不见的点
    if ((drawing.w ?? 0) < 3 && (drawing.h ?? 0) < 3) {
      nodes.value = nodes.value.filter((n) => n.id !== drawing!.id)
    } else {
      selectedId.value = drawing.id
    }
    drawing = null
    draw()
  }
  dragging = null
}

function onEdit() {
  dirty.value = true
  draw()
}

function removeSelected() {
  if (!selectedId.value) return
  nodes.value = nodes.value.filter((n) => n.id !== selectedId.value)
  selectedId.value = ''
  dirty.value = true
  draw()
}

/** 图片转成 data URI：服务端渲染只接受内嵌图片（防 SSRF），blob 地址它读不到 */
function beforeImage(file: File) {
  const reader = new FileReader()
  reader.onload = () => {
    const src = String(reader.result)
    cacheImage(src, (img) => {
      // 必须等解码完成才能拿到真实尺寸：提前读 width 会得到 0，元素就会不可见
      const maxW = Math.min(img.naturalWidth || 400, width.value * 0.6)
      const scale = maxW / (img.naturalWidth || 400)
      pushNode({
        id: `n${Date.now()}`,
        type: 'image',
        x: Math.max(8, (width.value - maxW) / 2),
        y: Math.max(8, (height.value - (img.naturalHeight || 300) * scale) / 2),
        w: maxW,
        h: (img.naturalHeight || 300) * scale,
        src,
      })
    })
  }
  reader.readAsDataURL(file)
  // 返回 false 阻止 a-upload 的默认上传行为
  return false
}

// ---------------------------------------------------------------- 导出

async function loadExports() {
  try {
    const res = await canvasApi.exports({ page: 1, page_size: 10 })
    exports.value = res.items ?? []
  } catch {
    // 导出列表是辅助信息，失败不打扰主流程
  }
}

async function submitExport() {
  if (!current.value) return
  if (dirty.value) await save()
  exporting.value = true
  try {
    const created = await canvasApi.submitExport(current.value.id, exportFormat.value, exportScale.value)
    exports.value = [created, ...exports.value]
    startPolling()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '提交导出失败')
  } finally {
    exporting.value = false
  }
}

/** 轮询未完成的导出任务；队列由服务端调度，前端只按状态刷新 */
function startPolling() {
  if (pollTimer) return
  pollTimer = window.setInterval(async () => {
    const pending = exports.value.filter((e) => e.status === 0 || e.status === 1)
    if (pending.length === 0) {
      stopPolling()
      return
    }
    try {
      const res = await canvasApi.exports({ page: 1, page_size: 10 })
      exports.value = res.items ?? []
    } catch {
      // 忽略单次失败，下一轮继续
    }
  }, 1500)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

async function download(id: number) {
  try {
    const url = await canvasApi.fetchExport(id)
    const a = document.createElement('a')
    a.href = url
    a.download = `canvas-${id}${exportFormat.value === 'jpeg' ? '.jpg' : '.png'}`
    a.click()
    // 立即 revoke 会让部分浏览器来不及读取，留一拍再释放
    setTimeout(() => URL.revokeObjectURL(url), 5000)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '下载失败')
  }
}

// ---------------------------------------------------------------- 生命周期

// 画布内容里的图片要先解码，否则导出与服务端渲染都拿不到尺寸
watch(
  () => nodes.value.map((n) => n.src).filter(Boolean) as string[],
  (srcs) => srcs.forEach((s) => cacheImage(s)),
  { immediate: true },
)

// Delete / Backspace 删除选中元素
function onKeydown(e: KeyboardEvent) {
  const target = e.target as HTMLElement
  if (target && ['INPUT', 'TEXTAREA'].includes(target.tagName)) return
  if ((e.key === 'Delete' || e.key === 'Backspace') && selectedId.value) {
    e.preventDefault()
    removeSelected()
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  await loadList()
  await loadExports()
  await Promise.all([loadAIModels(), loadTextModels()])
  if (canvases.value.length > 0) {
    await openCanvas(canvases.value[0].id)
  } else {
    await nextTick()
    draw()
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  stopPolling()
})
</script>

<style scoped>
.cv-page {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr) 300px;
  gap: 14px;
  align-items: start;
}

@media (max-width: 1200px) {
  .cv-page {
    grid-template-columns: 1fr;
  }
}

/* ---- 左右栏 ---- */
.cv-side {
  background: #fff;
  border: 1px solid #eef0f3;
  border-radius: 10px;
  padding: 14px;
}

.cv-side-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.cv-side-title {
  font-weight: 600;
  font-size: 13px;
}

.cv-empty-list {
  color: #98a2b3;
  font-size: 12px;
  padding: 12px 0;
  text-align: center;
}

.cv-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.15s;
}

.cv-item:hover {
  background: #f5f7fa;
}

.cv-item.active {
  background: rgba(22, 119, 255, 0.08);
}

.cv-item-main {
  flex: 1;
  min-width: 0;
}

.cv-item-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.cv-item-meta {
  color: #98a2b3;
  font-size: 12px;
}

/* ---- 中间 ---- */
.cv-main {
  background: #fff;
  border: 1px solid #eef0f3;
  border-radius: 10px;
  padding: 14px;
}

.cv-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.cv-divider {
  width: 1px;
  height: 18px;
  background: #eef0f3;
}

.cv-flex {
  flex: 1;
}

.cv-name {
  width: 160px;
}

.cv-stage {
  position: relative;
  border-radius: 8px;
  background:
    linear-gradient(45deg, #f7f8fa 25%, transparent 25%, transparent 75%, #f7f8fa 75%),
    linear-gradient(45deg, #f7f8fa 25%, transparent 25%, transparent 75%, #f7f8fa 75%);
  background-size: 16px 16px;
  background-position: 0 0, 8px 8px;
  padding: 12px;
  overflow: auto;
}

.cv-canvas {
  display: block;
  width: 100%;
  height: auto;
  border-radius: 4px;
  box-shadow: 0 1px 4px rgba(16, 24, 40, 0.08);
  cursor: crosshair;
}

.cv-hint {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: #98a2b3;
  font-size: 13px;
  pointer-events: none;
}

/* ---- 右栏 ---- */
.cv-section {
  padding-bottom: 14px;
  margin-bottom: 14px;
  border-bottom: 1px solid #f2f4f7;
}

.cv-section:last-child {
  border-bottom: none;
  margin-bottom: 0;
  padding-bottom: 0;
}

.cv-section-title {
  font-weight: 600;
  font-size: 13px;
  margin-bottom: 10px;
}

.cv-grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.cv-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
}

.cv-field > span {
  color: #667085;
  font-size: 12px;
}

.cv-color {
  display: flex;
  align-items: center;
  gap: 6px;
}

.cv-color input[type='color'] {
  width: 30px;
  height: 26px;
  padding: 0;
  border: 1px solid #eef0f3;
  border-radius: 6px;
  background: none;
  cursor: pointer;
  flex-shrink: 0;
}

.cv-empty-hint {
  color: #98a2b3;
  font-size: 12px;
  padding: 8px 0;
}

/* ---- 导出列表 ---- */
.cv-exports {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cv-export {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  font-size: 12px;
}

.cv-export-main {
  flex: 1;
  min-width: 0;
}

.cv-error {
  color: #d4380d;
  margin-top: 2px;
  word-break: break-all;
}
</style>
