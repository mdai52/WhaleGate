<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { UsagePoint } from '@/api/types'

echarts.use([BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const props = withDefaults(defineProps<{ points: UsagePoint[]; height?: number }>(), {
  height: 300,
})

const container = ref<HTMLDivElement | null>(null)
let chart: echarts.ECharts | null = null

function buildOption(): echarts.EChartsCoreOption {
  const points = props.points ?? []
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['请求数', '提示 Token', '生成 Token', '消耗点数'], bottom: 0 },
    grid: { left: 48, right: 24, top: 24, bottom: 56 },
    xAxis: { type: 'category', data: points.map((p) => p.date), boundaryGap: true },
    yAxis: [
      { type: 'value', name: '次数 / Token' },
      { type: 'value', name: '点数' },
    ],
    series: [
      {
        name: '请求数',
        type: 'bar',
        barMaxWidth: 24,
        itemStyle: { color: '#023b82', borderRadius: [4, 4, 0, 0] },
        data: points.map((p) => p.requests),
      },
      {
        name: '提示 Token',
        type: 'line',
        smooth: true,
        itemStyle: { color: '#3f8cff' },
        data: points.map((p) => p.prompt_tokens),
      },
      {
        name: '生成 Token',
        type: 'line',
        smooth: true,
        itemStyle: { color: '#13c2c2' },
        data: points.map((p) => p.completion_tokens),
      },
      {
        name: '消耗点数',
        type: 'line',
        yAxisIndex: 1,
        smooth: true,
        itemStyle: { color: '#fa8c16' },
        data: points.map((p) => p.points),
      },
    ],
  }
}

function render() {
  if (!chart) {
    return
  }
  chart.setOption(buildOption(), true)
}

function resize() {
  chart?.resize()
}

onMounted(() => {
  if (!container.value) {
    return
  }
  chart = echarts.init(container.value)
  render()
  window.addEventListener('resize', resize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resize)
  chart?.dispose()
  chart = null
})

watch(() => props.points, render, { deep: true })
</script>

<template>
  <div ref="container" :style="{ width: '100%', height: `${props.height}px` }" />
</template>
