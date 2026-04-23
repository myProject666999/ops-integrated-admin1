import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

export function useViewport() {
  const viewportWidth = ref(typeof window === 'undefined' ? 1280 : window.innerWidth)
  const isMobile = computed(() => viewportWidth.value <= 900)
  const credentialGridCols = computed(() => {
    if (viewportWidth.value <= 760) return 1
    if (viewportWidth.value <= 1260) return 2
    return 3
  })

  function syncViewportState() {
    viewportWidth.value = window.innerWidth
  }

  onMounted(() => {
    window.addEventListener('resize', syncViewportState)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('resize', syncViewportState)
  })

  return {
    viewportWidth,
    isMobile,
    credentialGridCols,
    syncViewportState,
  }
}
