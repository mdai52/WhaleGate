<script setup lang="ts">
/**
 * 倍率配置：以「渠道里出现的模型」为基准列出当前生效倍率。
 *
 * 未单独配置的模型回显兜底倍率并标记为「继承兜底」，
 * 管理员直接编辑即可落库为自定义配置，也可把自定义项恢复为默认。
 */
import { computed, reactive, ref } from 'vue'
import { ClearOutlined, DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { adminApi } from '@/api/admin'
import { channelApi } from '@/api/channel'
import { useIsMobile } from '@/composables/useIsMobile'
import type { RatioOverviewItem } from '@/api/types'

const isMobile = useIsMobile()
const loading = ref(false)
const saving = ref(false)
const items = ref<RatioOverviewItem[]>([])

/** 兜底倍率，来自列表里 `*` 那一项 */
const fallback = computed(() => items.value.find((i) => i.model === '*'))
const customCount = computed(() => items.value.filter((i) => i.custom).length)

/**
 * 失效配置：倍率表里有记录，但已没有任何启用渠道提供该模型。
 * 通常是渠道下线/改了模型后遗留的条目（含迁移预置的常见模型倍率），
 * 留在列表里会让人误以为还有渠道在按这个价计费。
 */
const orphans = computed(() =>
  items.value.filter((i) => i.model !== '*' && i.id > 0 && !i.channels.length),
)

/** 来源筛选：all 全部 / channel 渠道模型 / orphan 失效配置 */
const sourceFilter = ref<'all' | 'channel' | 'orphan'>('all')
const filteredItems = computed(() => {
  if (sourceFilter.value === 'channel') {
    return items.value.filter((i) => i.channels.length > 0)
  }
  if (sourceFilter.value === 'orphan') {
    return items.value.filter((i) => i.model !== '*' && !i.channels.length)
  }
  return items.value
})

// ---------------------------------------------------------------- 编辑

const modalOpen = ref(false)
/** 正在编辑的模型名；非空表示模型名锁定（编辑既有模型） */
const editingModel = ref('')
/** 该模型已单独配置时的主键；0 表示尚未单独配置 */
const editingId = ref(0)

const form = reactive({
  model: '',
  prompt_ratio: 15,
  completion_ratio: 60,
  image_ratio: 200,
  video_ratio: 100,
  enabled: true,
})

/** 模型候选：全局模型目录（避免手填错字） */
const modelOptions = ref<string[]>([])

async function loadCatalog() {
  if (modelOptions.value.length) {
    return
  }
  try {
    const res = await channelApi.catalog({ page_size: 500 })
    modelOptions.value = res.items.map((i) => i.model_id)
  } catch {
    modelOptions.value = []
  }
}

const columns = [
  { title: '模型', dataIndex: 'model', key: 'model' },
  { title: '输入倍率（点/1K）', key: 'prompt_ratio', width: 150 },
  { title: '输出倍率（点/1K）', key: 'completion_ratio', width: 150 },
  { title: '生图单价（点/张）', key: 'image_ratio', width: 150 },
  { title: '视频单价（点/秒）', key: 'video_ratio', width: 150 },
  { title: '来源', key: 'source', width: 110 },
  { title: '提供渠道', key: 'channels', width: 180 },
  { title: '操作', key: 'action', width: 200, fixed: 'right' as const },
]

async function load() {
  loading.value = true
  try {
    items.value = await adminApi.listRatios()
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingModel.value = ''
  editingId.value = 0
  Object.assign(form, {
    model: '',
    prompt_ratio: fallback.value?.prompt_ratio ?? 15,
    completion_ratio: fallback.value?.completion_ratio ?? 60,
    image_ratio: fallback.value?.image_ratio ?? 200,
    video_ratio: fallback.value?.video_ratio ?? 100,
    enabled: true,
  })
  modalOpen.value = true
  void loadCatalog()
}

function openEdit(row: RatioOverviewItem) {
  editingModel.value = row.model
  editingId.value = row.id
  Object.assign(form, {
    model: row.model,
    prompt_ratio: row.prompt_ratio,
    completion_ratio: row.completion_ratio,
    image_ratio: row.image_ratio,
    video_ratio: row.video_ratio,
    enabled: row.custom ? row.enabled : true,
  })
  modalOpen.value = true
  void loadCatalog()
}

async function submit() {
  const model = form.model.trim()
  if (!model) {
    message.warning('请填写模型名')
    return
  }
  saving.value = true
  try {
    await adminApi.upsertRatio({
      model,
      prompt_ratio: form.prompt_ratio,
      completion_ratio: form.completion_ratio,
      image_ratio: form.image_ratio,
      video_ratio: form.video_ratio,
      enabled: form.enabled,
    })
    message.success(editingId.value ? '倍率已更新' : '已保存为该模型的自定义倍率')
    modalOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

/** 把自定义倍率恢复为默认：删除配置后该模型重新按兜底计费。 */
function resetToDefault(row: RatioOverviewItem) {
  Modal.confirm({
    title: '恢复为默认倍率？',
    content: `删除「${row.model}」的自定义倍率后，该模型将重新按兜底倍率计费（当前 ${
      fallback.value ? `输入 ${fallback.value.prompt_ratio} / 输出 ${fallback.value.completion_ratio}` : '按系统默认值'
    }）。`,
    okText: '恢复默认',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await adminApi.deleteRatio(row.id)
      message.success('已恢复为默认倍率')
      await load()
    },
  })
}

/** 删除一条失效配置：模型已无渠道提供，这条倍率不会再命中任何调用。 */
function removeOrphan(row: RatioOverviewItem) {
  Modal.confirm({
    title: '删除该倍率配置？',
    content: `「${row.model}」当前没有任何启用渠道提供，这条配置不会命中任何调用；删除后将从列表移除。若之后重新接入该模型，将按兜底倍率计费。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await adminApi.deleteRatio(row.id)
      message.success('已删除')
      await load()
    },
  })
}

const cleaning = ref(false)

/** 一键清理所有失效配置：渠道已不提供这些模型，配置留着只会误导。 */
function cleanOrphans() {
  if (!orphans.value.length) {
    return
  }
  const names = orphans.value.map((i) => i.model).join('、')
  Modal.confirm({
    title: `清理 ${orphans.value.length} 条失效配置？`,
    content: `以下模型已没有任何启用渠道提供：${names}。删除后这些条目将从列表移除，重新接入时按兜底倍率计费。`,
    okText: '全部清理',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      cleaning.value = true
      try {
        // 逐条删除：接口按主键删除，批量走循环即可；失败的中断并提示剩余数量。
        for (const row of orphans.value) {
          await adminApi.deleteRatio(row.id)
        }
        message.success(`已清理 ${orphans.value.length} 条失效配置`)
        await load()
      } finally {
        cleaning.value = false
      }
    },
  })
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 8px">
        <h2 style="margin: 0">倍率配置</h2>
        <a-space>
          <a-button
            v-if="orphans.length"
            danger
            :loading="cleaning"
            @click="cleanOrphans"
          >
            <ClearOutlined />
            清理失效配置（{{ orphans.length }}）
          </a-button>
          <a-button type="primary" @click="openCreate">
            <PlusOutlined />
            新增倍率
          </a-button>
        </a-space>
      </div>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="渠道模型 + 手工配置的倍率合并在一张表里，未单独配置的按兜底倍率计费"
        description="「渠道模型」是启用渠道当前对外提供的模型，实时同步；「失效配置」是倍率表里已没有渠道提供的条目（渠道下线/改模型后遗留，或迁移预置的常见模型），不会命中任何调用，可单条删除或一键清理。倍率单位为「点 / 1K tokens」，1 点 = 0.001 元；模型名 * 表示兜底倍率。生图单价单位为「点 / 张」，视频单价单位为「点 / 秒」，填 0 均表示继承兜底单价；要全局免费请把 gateway.default_image_ratio / default_video_ratio 配成 0。"
      />

      <a-space style="margin-bottom: 12px" wrap>
        <a-radio-group v-model:value="sourceFilter" button-style="solid" size="small">
          <a-radio-button value="all">全部 {{ items.length }}</a-radio-button>
          <a-radio-button value="channel">
            渠道模型 {{ items.filter((i) => i.channels.length > 0).length }}
          </a-radio-button>
          <a-radio-button value="orphan">
            失效配置 {{ items.filter((i) => i.model !== '*' && !i.channels.length).length }}
          </a-radio-button>
        </a-radio-group>
        <a-tag color="green">自定义 {{ customCount }} 个</a-tag>
        <a-tag>继承兜底 {{ items.length - customCount }} 个</a-tag>
      </a-space>

      <a-table
        :columns="columns"
        :data-source="filteredItems"
        :loading="loading"
        row-key="model"
        :scroll="{ x: isMobile ? 900 : undefined }"
        :pagination="{
          pageSize: 20,
          showSizeChanger: true,
          showTotal: (total: number) => `共 ${total} 条`,
        }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'model'">
            <code>{{ (record as RatioOverviewItem).model }}</code>
            <a-tag v-if="(record as RatioOverviewItem).model === '*'" color="purple" style="margin-left: 6px">
              兜底
            </a-tag>
          </template>
          <template v-else-if="column.key === 'prompt_ratio'">
            {{ (record as RatioOverviewItem).prompt_ratio }}
          </template>
          <template v-else-if="column.key === 'completion_ratio'">
            {{ (record as RatioOverviewItem).completion_ratio }}
          </template>
          <template v-else-if="column.key === 'image_ratio'">
            {{ (record as RatioOverviewItem).image_ratio }}
            <!-- 整行继承兜底时来源列已说明，这里不再重复；
                 仅当 token 倍率已自定义而生图单价仍是继承时才值得提示 -->
            <a-tag
              v-if="(record as RatioOverviewItem).custom && !(record as RatioOverviewItem).image_custom"
              color="blue"
              style="margin-left: 4px"
            >
              继承兜底
            </a-tag>
          </template>
          <template v-else-if="column.key === 'video_ratio'">
            {{ (record as RatioOverviewItem).video_ratio }}
            <!-- 同样只在「有自定义但视频单价仍继承」时才提示，避免与来源列重复 -->
            <a-tag
              v-if="(record as RatioOverviewItem).custom && !(record as RatioOverviewItem).video_custom"
              color="blue"
              style="margin-left: 4px"
            >
              继承兜底
            </a-tag>
          </template>
          <template v-else-if="column.key === 'source'">
            <a-tag v-if="!(record as RatioOverviewItem).custom" color="blue">继承兜底</a-tag>
            <a-tag v-else-if="!(record as RatioOverviewItem).enabled" color="default">已停用</a-tag>
            <a-tag v-else color="green">自定义</a-tag>
          </template>
          <template v-else-if="column.key === 'channels'">
            <a-space v-if="(record as RatioOverviewItem).channels.length" wrap :size="[0, 4]">
              <a-tag
                v-for="name in (record as RatioOverviewItem).channels"
                :key="name"
                style="margin: 0"
              >
                {{ name }}
              </a-tag>
            </a-space>
            <a-tag v-else-if="(record as RatioOverviewItem).model === '*'" class="wg-muted">
              全局兜底
            </a-tag>
            <a-tag v-else color="red">渠道已下线</a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="openEdit(record as RatioOverviewItem)">编辑</a-button>
              <!-- 失效配置（模型已无渠道提供）直接删除；渠道模型的自定义倍率用「恢复默认」回退到兜底价 -->
              <a-button
                v-if="!(record as RatioOverviewItem).channels.length && (record as RatioOverviewItem).id > 0"
                size="small"
                danger
                type="text"
                @click="removeOrphan(record as RatioOverviewItem)"
              >
                <DeleteOutlined />
                删除
              </a-button>
              <a-tooltip v-else>
                <template #title>
                  {{
                    (record as RatioOverviewItem).custom
                      ? '清除该模型的自定义倍率，恢复按兜底倍率计费'
                      : '该模型由渠道提供且未单独配置倍率，没有可删除的自定义项；如需改价请点「编辑」'
                  }}
                </template>
                <!-- 禁用按钮不会触发原生事件，需要外层 span 让 Tooltip 生效 -->
                <span>
                  <a-button
                    size="small"
                    danger
                    type="text"
                    :disabled="!(record as RatioOverviewItem).custom"
                    @click="resetToDefault(record as RatioOverviewItem)"
                  >
                    <ReloadOutlined />
                    恢复默认
                  </a-button>
                </span>
              </a-tooltip>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="modalOpen"
      :title="editingModel ? (editingId ? '编辑倍率' : '配置倍率') : '新增倍率'"
      :footer="null"
      :width="440"
    >
      <!-- 必须带 :model：ant-design-vue 的 a-form 只在有 model 时才 emit('finish')，
           缺了它「保存」点了既没反应也没报错。 -->
      <a-form :model="form" layout="vertical" @finish="submit">
        <a-form-item label="模型名">
          <a-auto-complete
            v-model:value="form.model"
            :options="modelOptions.map((m) => ({ value: m }))"
            :disabled="!!editingModel"
            placeholder="输入或选择模型名，* 表示兜底"
            allow-clear
            filter-option
          />
          <div v-if="editingModel && !editingId" class="wg-hint">
            该模型当前继承兜底倍率，保存后会成为它自己的自定义倍率。
          </div>
        </a-form-item>
        <a-form-item label="输入倍率（点 / 1K tokens）">
          <a-input-number v-model:value="form.prompt_ratio" :min="0" :step="0.5" style="width: 100%" />
        </a-form-item>
        <a-form-item label="输出倍率（点 / 1K tokens）">
          <a-input-number v-model:value="form.completion_ratio" :min="0" :step="0.5" style="width: 100%" />
        </a-form-item>
        <a-form-item label="生图单价（点 / 张）">
          <a-input-number v-model:value="form.image_ratio" :min="0" :step="10" style="width: 100%" />
          <div class="wg-hint">
            图片生成按实际出图张数计费：点数 = 单价 × 张数，再乘分组倍率系数。填 0 表示继承兜底单价。
          </div>
        </a-form-item>
        <a-form-item label="视频单价（点 / 秒）">
          <a-input-number v-model:value="form.video_ratio" :min="0" :step="10" style="width: 100%" />
          <div class="wg-hint">
            视频生成按实际生成秒数计费：点数 = 单价 × 秒数，再乘分组倍率系数。填 0 表示继承兜底单价。
          </div>
        </a-form-item>
        <a-form-item label="启用">
          <a-switch v-model:checked="form.enabled" />
          <div class="wg-hint">停用后该模型回到兜底倍率计费。</div>
        </a-form-item>
        <a-button type="primary" html-type="submit" block :loading="saving">保存</a-button>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-hint {
  margin-top: 4px;
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
  line-height: 1.6;
}

.wg-muted {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
</style>
