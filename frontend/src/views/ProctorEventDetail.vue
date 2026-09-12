<template>
  <div class="page-card" v-loading="loading">
    <div class="page-header">
      <h2>监考事件详情</h2>
      <el-button @click="router.back()">返回</el-button>
    </div>

    <template v-if="event">
      <el-descriptions :column="2" border class="event-info">
        <el-descriptions-item label="事件 ID">{{ event.id }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusTagType(event.status)">{{ statusLabel(event.status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="考试">{{ event.exam_title }}（#{{ event.exam_id }}）</el-descriptions-item>
        <el-descriptions-item label="学生">{{ event.student_name }}（#{{ event.student_id }}）</el-descriptions-item>
        <el-descriptions-item label="事件类型">
          <el-tag :type="typeTagType(event.type)">{{ typeLabel(event.type) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="发生时间">{{ formatTime(event.occurred_at) }}</el-descriptions-item>
        <el-descriptions-item label="考试场次">#{{ event.attempt_id }}</el-descriptions-item>
        <el-descriptions-item label="上报详情">{{ event.detail || '-' }}</el-descriptions-item>
      </el-descriptions>

      <!-- 待处理：填写处理结论 -->
      <el-card v-if="event.status === 'pending'" class="review-card" shadow="never">
        <template #header>
          <span>处理结论</span>
        </template>
        <el-alert type="warning" :closable="false" show-icon title="结论提交后不可再修改，请谨慎填写。" class="review-alert" />
        <el-form label-width="90px">
          <el-form-item label="处理结果" required>
            <el-radio-group v-model="form.status">
              <el-radio value="confirmed">确认违规</el-radio>
              <el-radio value="ignored">忽略（误报）</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="处理结论" required>
            <el-input
              v-model="form.note"
              type="textarea"
              :rows="4"
              maxlength="512"
              show-word-limit
              placeholder="请填写处理结论，例如：经核实该生多次切屏查看资料，确认违规"
            />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="submitting" @click="onSubmit">提交结论</el-button>
          </el-form-item>
        </el-form>
      </el-card>

      <!-- 已处理：结论只读 -->
      <el-card v-else class="review-card" shadow="never">
        <template #header>
          <span>处理结论（已提交，不可修改）</span>
        </template>
        <el-descriptions :column="1" border>
          <el-descriptions-item label="处理结果">
            <el-tag :type="statusTagType(event.status)">{{ statusLabel(event.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="处理结论">{{ event.review_note }}</el-descriptions-item>
          <el-descriptions-item label="处理人">{{ event.reviewer_name || `#${event.reviewed_by}` }}</el-descriptions-item>
          <el-descriptions-item label="处理时间">{{ event.reviewed_at ? formatTime(event.reviewed_at) : '-' }}</el-descriptions-item>
        </el-descriptions>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import dayjs from 'dayjs'
import { proctorApi } from '../api'
import type { ProctorEvent, ProctorEventStatus, ProctorEventType } from '../types'

const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)
const event = ref<ProctorEvent | null>(null)
const loading = ref(false)
const submitting = ref(false)
const form = reactive<{ status: 'confirmed' | 'ignored'; note: string }>({ status: 'confirmed', note: '' })

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

async function load() {
  loading.value = true
  try {
    event.value = await proctorApi.detail(id)
  } catch {
    // 无权限或不存在时拦截器已提示，返回列表
    router.replace('/proctor-events')
  } finally {
    loading.value = false
  }
}

async function onSubmit() {
  if (!event.value) return
  if (!form.note.trim()) {
    ElMessage.warning('请填写处理结论')
    return
  }
  await ElMessageBox.confirm('处理结论提交后不可再修改，确认提交？', '提交确认', { type: 'warning' })
  submitting.value = true
  try {
    const updated = await proctorApi.review(event.value.id, { status: form.status, note: form.note.trim() })
    event.value = updated
    ElMessage.success('处理结论已提交')
  } catch {
    // 已被他人处理（409）等情况拦截器已提示，重新加载最新状态
    await load()
  } finally {
    submitting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.event-info {
  margin-bottom: 20px;
}
.review-card {
  max-width: 720px;
}
.review-alert {
  margin-bottom: 16px;
}
</style>
