import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useUserStore } from '@/stores/user'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { title: '登录', public: true },
  },
  {
    path: '/',
    component: () => import('@/layouts/BasicLayout.vue'),
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('@/views/DashboardView.vue'),
        meta: { title: '概览', icon: 'DashboardOutlined' },
      },
      {
        path: 'keys',
        name: 'keys',
        component: () => import('@/views/KeysView.vue'),
        meta: { title: '密钥管理', icon: 'KeyOutlined' },
      },
      {
        path: 'usage',
        name: 'usage',
        component: () => import('@/views/UsageView.vue'),
        meta: { title: '调用历史', icon: 'HistoryOutlined' },
      },
      {
        path: 'account',
        name: 'account',
        component: () => import('@/views/AccountView.vue'),
        meta: { title: '账号与安全', icon: 'UserOutlined' },
      },
      {
        path: 'admin/users',
        name: 'admin-users',
        component: () => import('@/views/admin/UsersView.vue'),
        meta: { title: '用户管理', icon: 'TeamOutlined', admin: true, group: '管理后台' },
      },
      {
        path: 'admin/channels',
        name: 'admin-channels',
        component: () => import('@/views/admin/ChannelsView.vue'),
        meta: { title: '渠道管理', icon: 'ApiOutlined', admin: true, group: '管理后台' },
      },
      {
        path: 'admin/settings',
        name: 'admin-settings',
        component: () => import('@/views/admin/SettingsView.vue'),
        meta: { title: '系统设置', icon: 'SettingOutlined', admin: true, group: '管理后台' },
      },
      {
        path: 'admin/ratios',
        name: 'admin-ratios',
        component: () => import('@/views/admin/RatiosView.vue'),
        meta: { title: '倍率配置', icon: 'PercentageOutlined', admin: true, group: '管理后台' },
      },
      {
        path: 'admin/logs',
        name: 'admin-logs',
        component: () => import('@/views/admin/LogsView.vue'),
        meta: { title: '全局日志', icon: 'FileSearchOutlined', admin: true, group: '管理后台' },
      },
      {
        path: 'admin/oauth',
        name: 'admin-oauth',
        component: () => import('@/views/admin/OAuthView.vue'),
        meta: { title: 'OAuth 登录', icon: 'SafetyCertificateOutlined', admin: true, group: '凭证' },
      },
      {
        path: 'admin/credentials',
        name: 'admin-credentials',
        component: () => import('@/views/admin/CredentialsView.vue'),
        meta: { title: '认证文件', icon: 'IdcardOutlined', admin: true, group: '凭证' },
      },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

router.beforeEach(async (to) => {
  document.title = to.meta.title ? `鲸闸 · ${to.meta.title}` : '鲸闸 WhaleGate'
  if (to.meta.public) {
    return true
  }
  const store = useUserStore()
  if (!store.isLogin) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (!store.profile) {
    try {
      await store.fetchProfile()
    } catch {
      store.logout()
      return { name: 'login' }
    }
  }
  if (to.meta.admin && !store.isAdmin) {
    return { name: 'dashboard' }
  }
  return true
})

export default router
