<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ApiOutlined,
  DashboardOutlined,
  FileSearchOutlined,
  HistoryOutlined,
  IdcardOutlined,
  KeyOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  PercentageOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import { Modal } from 'ant-design-vue'
import { useUserStore } from '@/stores/user'
import logoMark from '@/assets/logo-mark.png'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const collapsed = ref(false)
const isMobile = ref(window.innerWidth < 768)
const drawerOpen = ref(false)

window.addEventListener('resize', () => {
  isMobile.value = window.innerWidth < 768
})
watch(isMobile, (mobile) => {
  if (!mobile) drawerOpen.value = false
})

interface MenuItem {
  key: string
  label: string
  icon: string
  group?: string
}

const menus = computed<MenuItem[]>(() => {
  const list: MenuItem[] = [
    { key: '/dashboard', label: '概览', icon: 'DashboardOutlined', group: '用户门户' },
    { key: '/keys', label: '密钥管理', icon: 'KeyOutlined', group: '用户门户' },
    { key: '/usage', label: '调用历史', icon: 'HistoryOutlined', group: '用户门户' },
    { key: '/account', label: '账号与安全', icon: 'UserOutlined', group: '用户门户' },
  ]
  if (userStore.isAdmin) {
    list.push(
      { key: '/admin/users', label: '用户管理', icon: 'TeamOutlined', group: '管理后台' },
      { key: '/admin/channels', label: '渠道管理', icon: 'ApiOutlined', group: '管理后台' },
      { key: '/admin/ratios', label: '倍率配置', icon: 'PercentageOutlined', group: '管理后台' },
      { key: '/admin/logs', label: '全局日志', icon: 'FileSearchOutlined', group: '管理后台' },
      { key: '/admin/oauth', label: 'OAuth 登录', icon: 'SafetyCertificateOutlined', group: '凭证' },
      { key: '/admin/credentials', label: '认证文件', icon: 'IdcardOutlined', group: '凭证' },
    )
  }
  return list
})

/** 按 group 分组，便于侧边栏分段展示 */
const menuGroups = computed(() => {
  const groups: { name: string; items: MenuItem[] }[] = []
  for (const item of menus.value) {
    const name = item.group ?? ''
    let group = groups.find((g) => g.name === name)
    if (!group) {
      group = { name, items: [] }
      groups.push(group)
    }
    group.items.push(item)
  }
  return groups
})

/** 图标名 -> 组件，供 <component :is> 动态渲染 */
const iconMap: Record<string, unknown> = {
  ApiOutlined,
  DashboardOutlined,
  FileSearchOutlined,
  HistoryOutlined,
  IdcardOutlined,
  KeyOutlined,
  PercentageOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
}

const selectedKeys = computed(() => [route.path])

function go(key: string) {
  router.push(key)
  drawerOpen.value = false
}

function onMenuClick(info: { key: string | number }) {
  go(String(info.key))
}

function confirmLogout() {
  Modal.confirm({
    title: '确认退出登录？',
    content: '退出后需要重新输入账号密码。',
    okText: '退出',
    cancelText: '取消',
    onOk: () => {
      userStore.logout()
      router.push({ name: 'login' })
    },
  })
}
</script>

<template>
  <a-layout class="wg-layout">
    <a-drawer
      v-if="isMobile"
      v-model:open="drawerOpen"
      placement="left"
      :width="220"
      :body-style="{ padding: 0 }"
    >
      <div class="wg-drawer-brand">
        <img :src="logoMark" class="wg-logo-mark" alt="WhaleGate" />
        <span>鲸闸 WhaleGate</span>
      </div>
      <a-menu mode="inline" :selected-keys="selectedKeys" @click="onMenuClick">
        <a-menu-item-group v-for="g in menuGroups" :key="g.name" :title="g.name">
          <a-menu-item v-for="m in g.items" :key="m.key">
            <component :is="iconMap[m.icon]" />
            <span>{{ m.label }}</span>
          </a-menu-item>
        </a-menu-item-group>
      </a-menu>
    </a-drawer>

    <a-layout-sider
      v-else
      v-model:collapsed="collapsed"
      collapsible
      :trigger="null"
      theme="light"
      width="220"
    >
      <div class="wg-logo">
        <img :src="logoMark" class="wg-logo-mark" alt="WhaleGate" />
        <span v-if="!collapsed">鲸闸 WhaleGate</span>
      </div>
      <a-menu mode="inline" :selected-keys="selectedKeys" @click="onMenuClick">
        <a-menu-item-group v-for="g in menuGroups" :key="g.name" :title="g.name">
          <a-menu-item v-for="m in g.items" :key="m.key">
            <component :is="iconMap[m.icon]" />
            <span>{{ m.label }}</span>
          </a-menu-item>
        </a-menu-item-group>
      </a-menu>
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="wg-header">
        <div class="wg-header-left">
          <component
            :is="isMobile ? MenuUnfoldOutlined : collapsed ? MenuUnfoldOutlined : MenuFoldOutlined"
            class="wg-trigger"
            @click="isMobile ? (drawerOpen = true) : (collapsed = !collapsed)"
          />
          <span class="wg-title">{{ route.meta.title }}</span>
        </div>
        <a-space size="middle">
          <a-tag v-if="userStore.isAdmin" color="blue">管理员</a-tag>
          <a-dropdown>
            <a-space class="wg-user">
              <a-avatar :size="28"><UserOutlined /></a-avatar>
              <span class="wg-user-name">{{ userStore.displayName }}</span>
            </a-space>
            <template #overlay>
              <a-menu>
                <a-menu-item key="logout" @click="confirmLogout">
                  <LogoutOutlined />
                  退出登录
                </a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
        </a-space>
      </a-layout-header>

      <a-layout-content class="wg-content">
        <router-view />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<style scoped>
.wg-layout {
  min-height: 100vh;
}

.wg-logo {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-weight: 600;
  font-size: 16px;
  color: var(--wg-primary);
  border-bottom: 1px solid #f0f0f0;
}

.wg-logo-mark {
  width: 28px;
  height: 28px;
  object-fit: contain;
  flex: none;
}

.wg-drawer-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 56px;
  padding: 0 16px;
  font-weight: 600;
  font-size: 16px;
  color: var(--wg-primary);
  border-bottom: 1px solid #f0f0f0;
}

.wg-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  padding: 0 16px;
  border-bottom: 1px solid #f0f0f0;
}

.wg-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.wg-trigger {
  font-size: 18px;
  cursor: pointer;
}

.wg-title {
  font-size: 16px;
  font-weight: 600;
}

.wg-user {
  cursor: pointer;
}

.wg-content {
  padding: 0;
}

@media (max-width: 576px) {
  .wg-user-name {
    display: none;
  }
}
</style>
