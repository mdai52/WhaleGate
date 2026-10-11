<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CopyOutlined, DeleteOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons-vue'
import { message, Modal } from 'ant-design-vue'
import { skillApi } from '@/api/skill'
import type { Skill } from '@/api/types'

const loading = ref(false)
const scanning = ref(false)
const uploading = ref(false)
const items = ref<Skill[]>([])
const pagination = reactive({ current: 1, pageSize: 20, total: 0 })
const keyword = ref('')
const scanDir = ref('')

const detailOpen = ref(false)
const detail = ref<Skill | null>(null)

/** 本页统计：列表是分页的，标签里写明「本页」避免被当成全量 */
const enabledOnPage = computed(() => items.value.filter((s) => s.enabled).length)
const alwaysOnPage = computed(() => items.value.filter((s) => s.mode === 'always').length)

/** 空状态下的接入步骤 */
const guideSteps = [
  { title: '建技能目录', desc: '每个技能一个子目录，目录内含 SKILL.md' },
  { title: '用 frontmatter 声明', desc: '写 name / description / mode，可不写走默认值' },
  { title: '扫描导入并选择模式', desc: '点「扫描导入」，再按长度决定全文注入或仅索引' },
]

const skillExample = `---
name: code-review
description: 按项目规范做代码评审，输出问题清单与修改建议
mode: index
---

# 代码评审

评审时按以下顺序检查，输出按严重程度排序：
1. 边界条件与错误处理是否完整
2. 是否存在并发或资源泄漏风险
3. 命名与项目既有约定是否一致
`

async function copyExample() {
  try {
    await navigator.clipboard.writeText(skillExample)
    message.success('示例已复制，保存为 SKILL.md 即可扫描导入')
  } catch {
    message.error('复制失败，请手动选中复制')
  }
}

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
    // 扫到 0 个多半是目录没写对，给出可操作的提示而不是只报数字
    if (res.imported > 0) {
      message.success(`扫描完成，导入 ${res.imported} 个技能`)
    } else {
      message.warning(
        '未扫描到技能：请确认目录存在且每个技能子目录下有 SKILL.md（可在上方「扫描目录」显式指定，或检查配置文件的 skills.dir）',
      )
    }
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '扫描失败')
  } finally {
    scanning.value = false
  }
}

/** 压缩包导入：交给后端按管理员配置的上限校验，前端只在成功后刷新列表 */
async function upload(file: File) {
  uploading.value = true
  try {
    const res = await skillApi.upload(file)
    message.success(`导入成功，新增或更新 ${res.imported} 个技能`)
    await load()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '导入失败')
  } finally {
    uploading.value = false
  }
  return false
}

function beforeUpload(file: File) {
  void upload(file)
  // 返回 false 阻止 a-upload 的默认提交：这里的请求已由 skillApi.upload 发出。
  return false
}

async function toggle(row: Skill, checked: boolean) {
  await skillApi.toggle(row.id, checked)
  message.success(checked ? `已启用「${row.name}」` : `已停用「${row.name}」`)
  await load()
}

/** 切换注入模式：全文注入省一次往返，但会占上下文；长技能适合改成仅索引 */
async function setMode(row: Skill, mode: 'always' | 'index') {
  await skillApi.setMode(row.id, mode)
  message.success(`「${row.name}」已切换为${mode === 'always' ? '全文注入' : '仅索引'}`)
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
          <div class="wg-stats">
            <div class="wg-stat"><b>{{ pagination.total }}</b><span>技能</span></div>
            <template v-if="items.length">
              <div class="wg-stat"><b>{{ enabledOnPage }}</b><span>本页启用</span></div>
              <div class="wg-stat"><b>{{ alwaysOnPage }}</b><span>本页全文注入</span></div>
            </template>
          </div>
        </div>
        <div style="display: flex; gap: 8px; flex-wrap: wrap">
          <a-upload
            :show-upload-list="false"
            accept=".zip,.tar.gz,.tgz"
            :before-upload="beforeUpload"
          >
            <a-button :loading="uploading">
              <UploadOutlined />
              压缩包导入
            </a-button>
          </a-upload>
          <a-button type="primary" :loading="scanning" @click="scan">
            <ReloadOutlined />
            扫描导入
          </a-button>
        </div>
      </div>

      <!-- 目录结构与 frontmatter 用并列卡片，比一段说明更容易照着做 -->
      <div class="wg-modes">
        <div class="wg-mode">
          <div class="wg-mode-head">目录结构</div>
          <pre class="wg-tree">skills/
├─ code-review/
│  └─ SKILL.md
└─ data-analysis/
   └─ SKILL.md</pre>
          <div class="wg-mode-foot">每个技能一个子目录，目录内含 SKILL.md</div>
        </div>
        <div class="wg-mode">
          <div class="wg-mode-head">SKILL.md frontmatter</div>
          <ul class="wg-fields">
            <li><code>name</code> 技能名，不写则取目录名</li>
            <li><code>description</code> 一句话说明，仅索引模式下注入</li>
            <li><code>mode</code> <b>always</b> 全文注入 / <b>index</b> 仅索引</li>
          </ul>
          <div class="wg-mode-foot">长技能用 index，省上下文；短指令用 always</div>
        </div>
      </div>

      <a-space style="margin-bottom: 16px" wrap>
        <a-input-search v-model:value="keyword" placeholder="搜索技能" allow-clear style="width: 220px" @search="load" />
        <a-input v-model:value="scanDir" placeholder="扫描目录（留空用配置值 skills）" allow-clear style="width: 260px" />
      </a-space>

      <!-- 完全没有技能时给一条可照着走的路径；搜索无结果时不占用这里 -->
      <div v-if="!keyword.trim() && pagination.total === 0 && !loading" class="wg-guide">
        <div class="wg-guide-steps">
          <div v-for="(s, i) in guideSteps" :key="i" class="wg-guide-step">
            <div class="wg-guide-no">{{ i + 1 }}</div>
            <div>
              <div class="wg-guide-title">{{ s.title }}</div>
              <div class="wg-muted">{{ s.desc }}</div>
            </div>
          </div>
        </div>

        <div class="wg-example">
          <div class="wg-example-head">
            <span>SKILL.md 示例</span>
            <a-button size="small" @click="copyExample">
              <CopyOutlined />
              复制示例
            </a-button>
          </div>
          <pre class="wg-example-body">{{ skillExample }}</pre>
        </div>

        <div class="wg-guide-foot">
          <span class="wg-muted">目录可在上方「扫描目录」指定，留空用配置里的 skills.dir</span>
        </div>
      </div>

      <a-table
        v-else
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
          <template v-if="column.key === 'name'">
            <div>{{ (record as Skill).name }}</div>
            <!-- 展示名直观，且常与目录名不同，此前没有渲染 -->
            <div v-if="(record as Skill).display_name && (record as Skill).display_name !== (record as Skill).name" class="wg-muted">
              {{ (record as Skill).display_name }}
            </div>
          </template>
          <template v-else-if="column.key === 'description'">
            <span class="wg-clamp">{{ (record as Skill).description || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'mode'">
            <a-space :size="4">
              <a-tag :color="(record as Skill).mode === 'always' ? 'blue' : 'default'">
                {{ (record as Skill).mode === 'always' ? '全文注入' : '仅索引' }}
              </a-tag>
              <!-- 模式此前只能在扫描时由 SKILL.md 决定，现在可直接切换 -->
              <a-button
                size="small"
                type="link"
                @click="setMode(record as Skill, (record as Skill).mode === 'always' ? 'index' : 'always')"
              >
                切换
              </a-button>
            </a-space>
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
.wg-stats {
  display: flex;
  gap: 20px;
  margin-top: 8px;
}
.wg-stat {
  display: flex;
  align-items: baseline;
  gap: 5px;
}
.wg-stat b {
  font-size: 18px;
  font-weight: 600;
  color: var(--wg-primary, #1677ff);
}
.wg-stat span {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
}

.wg-modes {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.wg-mode {
  padding: 12px 16px;
  border: 1px solid #f0f0f0;
  border-radius: 8px;
  background: #fafafa;
}
.wg-mode-head {
  font-weight: 600;
  margin-bottom: 8px;
}
.wg-mode-foot {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
}
.wg-tree {
  margin: 0 0 8px;
  font-size: 12px;
  line-height: 1.6;
}
.wg-fields {
  margin: 0 0 8px;
  padding-left: 18px;
  font-size: 13px;
}
.wg-fields li {
  margin-bottom: 4px;
}

.wg-guide {
  max-width: 760px;
  margin: 0 auto;
  padding: 24px 0 8px;
}
.wg-guide-steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
}
.wg-guide-step {
  display: flex;
  gap: 10px;
}
.wg-guide-no {
  flex: none;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--wg-primary, #1677ff);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.wg-guide-title {
  font-weight: 600;
  margin-bottom: 2px;
}
.wg-example {
  margin-top: 20px;
  border: 1px solid #f0f0f0;
  border-radius: 8px;
  overflow: hidden;
}
.wg-example-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: #fafafa;
  border-bottom: 1px solid #f0f0f0;
  font-size: 13px;
  font-weight: 600;
}
.wg-example-body {
  margin: 0;
  padding: 12px;
  background: #fff;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
}
.wg-guide-foot {
  margin-top: 16px;
  text-align: center;
}

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
