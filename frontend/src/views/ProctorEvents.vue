<template>
  <div class="page-card">
    <div class="page-header">
      <h2>监考事件复核中心</h2>
    </div>
    <div class="toolbar">
      <el-radio-group v-model="query.status" @change="onFilterChange">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="pending">待处理</el-radio-button>
        <el-radio-button value="confirmed">已确认</el-radio-button>
        <el-radio-button value="ignored">已忽略</el-radio-button>
      </el-radio-group>
      <el-select v-model="query.exam_id" placeholder="全部考试" clearable style="width: 220px" @change="onFilterChange">
        <el-option v-for="exam in exams" :key="exam.id" :label="exam.title" :value="exam.id" />
      </el-select>
      <el-select v-model="query.type" placeholder="全部类型" clearable style="width: 150px" @change="onFilterChange">
        <el-option label="切屏" value="tab_switch" />
        <el-option label="退出全屏" value="fullscreen_exit" />
        <el-option label="离开页面" value="page_leave" />
      </el-select>
      <el-button type="primary" @click="load">查询</el-button>
    </div>
    <el-table :data="rows" v-loading="loading" border>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="发生时间" width="170">
        <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
      </el-table-column>
      <el-table-column prop="exam_title" label="考试" min-width="160" show-overflow-tooltip />
      <el-table-column prop="student_name" label="学生" width="110" />
      <el-table-column label="事件类型" width="110">
        <template #default="{ row }">
          <el-tag :type="typeTagType(row.type)">{{ typeLabel(row.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="处理人" width="110">
        <template #default="{ row }">{{ row.reviewer_name || '-' }}</template>
      </el-table-column>
      <el-table-column label="处理时间" width="170">
        <template #default="{ row }">{{ row.reviewed_at ? formatTime(row.reviewed_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="goDetail(row)">
            {{ row.status === 'pending' ? '去处理' : '查看' }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无监考事件" />
      </template>
    </el-table>
    <el-pagination
      class="pager"
      v-model:current-page="query.page"
      v-model:page-size="query.page_size"
      :total="total"
      layout="total, prev, pager, next"
      @current-change="load"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import dayjs from 'dayjs'
import { examApi, proctorApi } from '../api'
import type { Exam, ProctorEvent, ProctorEventStatus, ProctorEventType } from '../types'

const router = useRouter()
const rows = ref<ProctorEvent[]>([])
const exams = ref<Exam[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive<{ page: number; page_size: number; status: ProctorEventStatus | ''; type: ProctorEventType | ''; exam_id?: number }>({
  page: 1,
  page_size: 10,
  status: '',
  type: '',
  exam_id: undefined
})

function formatTime(v: string) {
  return v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-'
}

function typeLabel(type: ProctorEventType) {
  const map: Record<string, string> = { tab_switch: '切屏', fullscreen_exit: '退出全屏', page_leave: '离开页面' }
  return map[type] || type
}

function typeTagType(type: ProctorEventType) {
  const map: Record<string, 'warning' | 'danger' | 'info'> = { tab_switch: 'warning', fullscreen_exit: 'danger', page_leave: 'info' }
  return map[type] || 'info'
}

function statusLabel(status: ProctorEventStatus) {
  const map: Record<string, string> = { pending: '待处理', confirmed: '已确认', ignored: '已忽略' }
  return map[status] || status
}

function statusTagType(status: ProctorEventStatus) {
  const map: Record<string, 'warning' | 'danger' | 'info'> = { pending: 'warning', confirmed: 'danger', ignored: 'info' }
  return map[status] || 'info'
}

function onFilterChange() {
  query.page = 1
  load()
}

function goDetail(row: ProctorEvent) {
  router.push(`/proctor-events/${row.id}`)
}

async function load() {
  loading.value = true
  try {
    const res = await proctorApi.list({ ...query })
    rows.value = res.items
    total.value = res.total
  } finally {
    loading.value = false
  }
}

async function loadExams() {
  // 教师只能看到自己创建的考试，管理员看到全部（服务端已按角色过滤）
  const res = await examApi.list({ page: 1, page_size: 100 })
  exams.value = res.items
}

onMounted(() => {
  load()
  loadExams().catch(() => undefined)
})
</script>

<style scoped>
.pager {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
