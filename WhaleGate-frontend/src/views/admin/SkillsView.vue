<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { DeleteOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { skillApi } from '@/api/skill'
import type { Skill } from '@/api/types'

const loading = ref(false)
const scanning = ref(false)
const items = ref<Skill[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })
const keyword = ref('')
const scanDir = ref('')

const detailOpen = ref(false)
const detail = ref<Skill | null>(null)

async function load() {
  loading.value = true
  try {
    const res = await skillApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      keyword: keyword.value.trim() || undefined,
    })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

async function scan() {
  scanning.value = true
  try {
    const res = await skillApi.scan(scanDir.value.trim() || undefined)
    message.success(`扫描完成，导入 ${res.imported} 个技能`)
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '扫描失败')
  } finally {
    scanning.value = false
  }
}

async function toggle(row: Skill, checked: boolean) {
  await skillApi.toggle(row.id, checked)
  message.success(checked ? `已启用「${row.name}」` : `已停用「${row.name}」`)
  await load()
}

function removeSkill(row: Skill) {
  Modal.confirm({
    title: '确认删除该技能？',
    content: `删除后「${row.name}」不再注入提示词。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await skillApi.remove(row.id)
      message.success('已删除')
      await load()
    },
  })
}

function openDetail(row: Skill) {
  detail.value = row
  detailOpen.value = true
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 20
  void load()
}

onMounted(load)
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 16px">
        <div>
          <h2 style="margin: 0">技能管理</h2>
          <div class="wg-muted" style="margin-top: 4px">共 {{ pagination.total }} 个技能</div>
        </div>
        <a-button type="primary" :loading="scanning" @click="scan">
          <ReloadOutlined />
          扫描导入
        </a-button>
      </div>

      <a-alert
        type="info"
        show-icon
        style="margin-bottom: 16px"
        message="SKILL.md 格式"
        description="每个技能一个目录，内含 SKILL.md；可用 frontmatter 声明 name / description / mode。mode=always 把正文全量注入 system 提示词，mode=index 只注入名称与描述索引。"
      />

      <a-space style="margin-bottom: 16px" wrap>
        <a-input-search v-model:value="keyword" placeholder="搜索技能" allow-clear style="width: 220px" @search="load" />
        <a-input v-model:value="scanDir" placeholder="扫描目录（留空用配置值 skills）" allow-clear style="width: 260px" />
      </a-space>

      <a-table
        :columns="[
          { title: '名称', dataIndex: 'name', key: 'name' },
          { title: '描述', dataIndex: 'description', key: 'description' },
          { title: '模式', dataIndex: 'mode', key: 'mode' },
          { title: '来源', dataIndex: 'source', key: 'source' },
          { title: '启用', key: 'enabled' },
          { title: '操作', key: 'action', fixed: 'right' },
        ]"
        :data-source="items"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 720 }"
        :pagination="{
          current: pagination.current,
          pageSize: pagination.pageSize,
          total: pagination.total,
          showSizeChanger: true,
          showTotal: (total: number) => `共 ${total} 条`,
        }"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'description'">
            <span class="wg-clamp">{{ (record as Skill).description || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'mode'">
            <a-tag :color="(record as Skill).mode === 'always' ? 'blue' : 'default'">
              {{ (record as Skill).mode === 'always' ? '全文注入' : '仅索引' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-switch
              size="small"
              :checked="(record as Skill).enabled"
              @change="(checked: boolean) => toggle(record as Skill, checked)"
            />
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button size="small" @click="openDetail(record as Skill)">查看</a-button>
              <a-button danger size="small" type="text" @click="removeSkill(record as Skill)">
                <DeleteOutlined />
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="detailOpen" :title="detail ? `技能：${detail.name}` : '技能详情'" :footer="null" :width="680">
      <a-descriptions :column="1" size="small" bordered>
        <a-descriptions-item label="描述">{{ detail?.description || '—' }}</a-descriptions-item>
        <a-descriptions-item label="模式">{{ detail?.mode }}</a-descriptions-item>
        <a-descriptions-item label="来源">{{ detail?.source }}</a-descriptions-item>
        <a-descriptions-item label="文件路径">{{ detail?.file_path || '—' }}</a-descriptions-item>
      </a-descriptions>
      <div style="margin-top: 12px">
        <div class="wg-muted" style="margin-bottom: 4px">正文</div>
        <pre class="wg-content">{{ detail?.content }}</pre>
      </div>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-clamp {
  display: inline-block;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.wg-content {
  margin: 0;
  padding: 12px;
  background: #fafafa;
  border-radius: 6px;
  font-size: 12px;
  white-space: pre-wrap;
  max-height: 360px;
  overflow: auto;
}
</style>
