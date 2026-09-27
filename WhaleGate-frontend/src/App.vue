<script setup lang="ts">
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'

import ForceChangePasswordModal from '@/components/ForceChangePasswordModal.vue'

dayjs.locale('zh-cn')
</script>

<template>
  <a-config-provider
    :locale="zhCN"
    :theme="{ token: { colorPrimary: '#023b82', borderRadius: 8 } }"
  >
    <router-view />
    <!-- 初始密码登录后强制改密 -->
    <ForceChangePasswordModal />
  </a-config-provider>
</template>
