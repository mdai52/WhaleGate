<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import dayjs from 'dayjs'
import { message, Modal } from 'ant-design-vue'
import { DeleteOutlined } from '@ant-design/icons-vue'
import { credentialApi } from '@/api/credential'
import { parseJSON } from '@/utils/json'
import type { Credential } from '@/api/types'

const loading = ref(false)
const items = ref<Credential[]>([])
const pagination = reactive({ current: 1, pageSize: 12, total: 0 })

// 筛选：供应商 / 关键字 / 状态
const providerFilter = ref('')
const keyword = ref('')
const statusFilter = ref<number | undefined>(undefined)

const enabledCount = computed(() => items.value.filter((c) => c.status === 1).length)

const statusMeta: Record<number, { text: string; color: string }> = {
  1: { text: '启用', color: 'green' },
  2: { text: '停用', color: 'default' },
  3: { text: '失效', color: 'red' },
}

async function load() {
  loading.value = true
  try {
    const res = await credentialApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      provider: providerFilter.value || undefined,
      keyword: keyword.value.trim() || undefined,
      status: statusFilter.value,
    })
    items.value = res.items
    pagination.total = res.total
  } finally {
    loading.value = false
  }
}

function resetFilters() {
  providerFilter.value = ''
  keyword.value = ''
  statusFilter.value = undefined
  pagination.current = 1
  void load()
}

/** 上传认证文件 */
async function handleUpload(options: { file: File }) {
  try {
    await credentialApi.import(options.file)
    message.success('认证文件导入成功')
    await load()
  } catch {
    message.error('认证文件导入失败，请确认文件格式')
  }
}

function toggleStatus(cred: Credential) {
  const next = cred.status === 1 ? 2 : 1
  credentialApi
    .setStatus(cred.id, next)
    .then(() => {
      message.success(next === 1 ? '已启用' : '已停用')
      void load()
    })
    .catch(() => message.error('操作失败'))
}

function refreshCredential(cred: Credential) {
  credentialApi
    .refresh(cred.id)
    .then(() => {
      message.success('凭证已续期')
      void load()
    })
    .catch(() => message.error('续期失败：该凭证可能不支持自动续期'))
}

/** 重命名凭证 */
const renameOpen = ref(false)
const renameTarget = ref<Credential | null>(null)
const renameName = ref('')

function openRename(cred: Credential) {
  renameTarget.value = cred
  renameName.value = cred.name
  renameOpen.value = true
}

async function submitRename() {
  const target = renameTarget.value
  if (!target || !renameName.value.trim()) {
    message.warning('请输入名称')
    return
  }
  try {
    await credentialApi.rename(target.id, renameName.value.trim())
    message.success('已重命名')
    renameOpen.value = false
    await load()
  } catch {
    message.error('重命名失败')
  }
}

/** 下载认证文件 */
async function downloadCredential(cred: Credential) {
  try {
    const { name, content } = await credentialApi.export(cred.id)
    const blob = new Blob([content], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = name
    anchor.click()
    URL.revokeObjectURL(url)
    message.success('已导出认证文件')
  } catch {
    message.error('导出失败')
  }
}

/** 解析配额快照（有数据时展示进度），形如 {"gemini_5h": {"used": 12, "limit": 100}} */
function quotaEntries(cred: Credential): { label: string; used: number; limit: number }[] {
  const quota = parseJSON<Record<string, unknown>>(cred.quota ?? '', {})
  if (!quota.ok) {
    return []
  }
  const out: { label: string; used: number; limit: number }[] = []
  for (const [key, value] of Object.entries(quota.value)) {
    const used = Number((value as Record<string, unknown>)?.used ?? 0)
    const limit = Number((value as Record<string, unknown>)?.limit ?? 0)
    if (Number.isFinite(used) && Number.isFinite(limit) && limit > 0) {
      out.push({ label: key, used, limit })
    }
  }
  return out
}

function removeCredential(cred: Credential) {
  Modal.confirm({
    title: '确认删除该凭证？',
    content: `删除后绑定此凭证的渠道将无法鉴权。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      await credentialApi.remove(cred.id)
      message.success('已删除')
      await load()
    },
  })
}

function formatTime(value?: string | null) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—'
}

function onTableChange(pager: { current?: number; pageSize?: number }) {
  pagination.current = pager.current ?? 1
  pagination.pageSize = pager.pageSize ?? 12
  void load()
}

load()
</script>

<template>
  <div class="wg-page">
    <a-card>
      <div class="wg-card-title" style="margin-bottom: 12px">
        <div>
          <h2 style="margin: 0">认证文件管理</h2>
          <div class="wg-muted" style="margin-top: 4px">
            共 {{ pagination.total }} 个凭证，{{ enabledCount }} 个启用
          </div>
        </div>
        <a-space>
          <a-upload :before-upload="() => false" :show-upload-list="false" :custom-request="handleUpload as never">
            <a-button type="primary">
              上传文件
            </a-button>
          </a-upload>
        </a-space>
      </div>

      <a-form layout="inline" class="wg-filters">
        <a-form-item label="供应商">
          <a-select
            v-model:value="providerFilter"
            placeholder="全部"
            allow-clear
            style="width: 140px"
            @change="load"
          />
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="statusFilter" placeholder="全部" allow-clear style="width: 120px" @change="load">
            <a-select-option :value="1">启用</a-select-option>
            <a-select-option :value="2">停用</a-select-option>
            <a-select-option :value="3">问题</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="关键字">
          <a-input-search
            v-model:value="keyword"
            placeholder="名称 / 账号 / 类型 / 文件名"
            allow-clear
            style="width: 240px"
            @search="load"
          />
        </a-form-item>
        <a-form-item>
          <a-button @click="resetFilters">重置</a-button>
        </a-form-item>
      </a-form>

      <a-empty
        v-if="!loading && items.length === 0"
        description="暂无凭证，可通过 OAuth 登录或上传认证文件创建"
        style="padding: 48px 0"
      />

      <a-row :gutter="[16, 16]">
        <a-col v-for="cred in items" :key="cred.id" :xs="24" :md="12" :xl="8">
          <a-card size="small" hoverable>
            <div class="wg-cred-head">
              <a-tag :color="cred.provider === 'anthropic' ? 'orange' : cred.provider === 'codex' ? 'green' : 'blue'">
                {{ cred.provider }}
              </a-tag>
              <a-tag :color="statusMeta[cred.status]?.color">
                {{ statusMeta[cred.status]?.text ?? '未知' }}
              </a-tag>
              <span class="wg-cred-name">{{ cred.name }}</span>
              <a-switch
                size="small"
                :checked="cred.status === 1"
                @change="() => toggleStatus(cred)"
              />
            </div>

            <div class="wg-cred-row">
              <span class="wg-muted">账号</span>
              <span>{{ cred.account || '—' }}</span>
            </div>
            <div class="wg-cred-row">
              <span class="wg-muted">来源</span>
              <span>{{ cred.auth_type === 'file' ? `文件 ${cred.file_name || ''}` : 'OAuth 授权' }}</span>
            </div>
            <div class="wg-cred-row">
              <span class="wg-muted">过期时间</span>
              <span>{{ formatTime(cred.expires_at) }}</span>
            </div>
            <div class="wg-cred-row">
              <span class="wg-muted">最近续期</span>
              <span>{{ formatTime(cred.last_refresh_at) }}</span>
            </div>

            <template v-if="quotaEntries(cred).length">
              <a-divider style="margin: 12px 0" />
              <div v-for="q in quotaEntries(cred)" :key="q.label" class="wg-quota-row">
                <div class="wg-quota-label">
                  <span>{{ q.label }}</span>
                  <span class="wg-muted">{{ ((q.used / q.limit) * 100).toFixed(1) }}%</span>
                </div>
                <a-progress
                  :percent="Math.min(100, (q.used / q.limit) * 100)"
                  size="small"
                  :show-info="false"
                  :stroke-color="q.used / q.limit > 0.8 ? '#fa541c' : '#389e0d'"
                />
                <div class="wg-muted" style="font-size: 12px">
                  已用 {{ q.used }} / {{ q.limit }}
                </div>
              </div>
            </template>

            <a-alert
              v-if="cred.last_error"
              type="error"
              :message="cred.last_error"
              style="margin-top: 8px"
              show-icon
            />

            <div class="wg-cred-actions">
              <a-button size="small" @click="openRename(cred)">编辑</a-button>
              <a-button size="small" @click="downloadCredential(cred)">下载</a-button>
              <a-button size="small" :disabled="!cred.provider" @click="refreshCredential(cred)">续期</a-button>
              <a-button size="small" danger type="text" @click="removeCredential(cred)">
                <DeleteOutlined />
              </a-button>
            </div>
          </a-card>
        </a-col>
      </a-row>

      <div style="margin-top: 16px; text-align: right" v-if="pagination.total > pagination.pageSize">
        <a-pagination
          v-model:current="pagination.current"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          show-size-changer
          @change="onTableChange"
        />
      </div>
    </a-card>

    <a-modal
      v-model:open="renameOpen"
      title="重命名凭证"
      ok-text="保存"
      cancel-text="取消"
      :width="400"
      @ok="submitRename"
    >
      <a-form layout="vertical">
        <a-form-item label="名称">
          <a-input v-model:value="renameName" placeholder="例如：Claude 主账号" allow-clear />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.wg-filters {
  margin-bottom: 16px;
  row-gap: 8px;
}

.wg-quota-row {
  margin-top: 6px;
}

.wg-quota-label {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  margin-bottom: 2px;
}

<style scoped>
.wg-muted {
  color: rgba(0, 0, 0, 0.45);
}

.wg-cred-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.wg-cred-name {
  font-weight: 600;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.wg-cred-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
  padding: 2px 0;
}

.wg-cred-actions {
  display: flex;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 12px;
}
</style>
