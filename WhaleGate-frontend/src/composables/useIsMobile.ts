import { onBeforeUnmount, onMounted, ref } from 'vue'

/** 响应式断点：小于 768px 视为移动端。 */
export function useIsMobile(breakpoint = 768) {
  const isMobile = ref(window.innerWidth < breakpoint)

  const onResize = () => {
    isMobile.value = window.innerWidth < breakpoint
  }

  onMounted(() => window.addEventListener('resize', onResize))
  onBeforeUnmount(() => window.removeEventListener('resize', onResize))

  return isMobile
}
