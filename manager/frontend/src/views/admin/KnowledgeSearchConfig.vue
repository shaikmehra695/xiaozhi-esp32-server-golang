<template>
  <div class="config-page">
    <div class="page-header">
      <h2>知识库检索配置</h2>
      <el-button type="primary" @click="openDialog()">添加配置</el-button>
    </div>

    <el-table :data="items" v-loading="loading" style="width: 100%">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="provider" label="提供商" width="120" />
      <el-table-column prop="name" label="名称" width="160" />
      <el-table-column prop="config_id" label="配置ID" width="170" />
      <el-table-column label="配置摘要">
        <template #default="scope">{{ getConfigSummary(scope.row) }}</template>
      </el-table-column>
      <el-table-column label="启用" width="80">
        <template #default="scope"><el-tag :type="scope.row.enabled ? 'success' : 'info'">{{ scope.row.enabled ? '是' : '否' }}</el-tag></template>
      </el-table-column>
      <el-table-column label="默认" width="80">
        <template #default="scope"><el-tag :type="scope.row.is_default ? 'success' : 'info'">{{ scope.row.is_default ? '是' : '否' }}</el-tag></template>
      </el-table-column>
      <el-table-column label="操作" width="220">
        <template #default="scope">
          <el-button size="small" @click="openDialog(scope.row)">编辑</el-button>
          <el-button size="small" @click="toggle(scope.row.id)">{{ scope.row.enabled ? '禁用' : '启用' }}</el-button>
          <el-button size="small" type="danger" @click="remove(scope.row.id)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑配置' : '新增配置'" width="700px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="提供商">
          <el-select v-model="form.provider" style="width: 100%" @change="onProviderChange">
            <el-option value="dify" label="dify" />
            <el-option value="ragflow" label="ragflow" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="配置ID"><el-input v-model="form.config_id" /></el-form-item>
        <template v-if="form.provider === 'dify'">
          <el-form-item label="Base URL"><el-input v-model="form.base_url" :placeholder="DEFAULT_DIFY_BASE_URL" /></el-form-item>
          <el-form-item label="API Key"><el-input v-model="form.api_key" type="password" show-password /></el-form-item>
          <el-form-item label="阈值"><el-input-number v-model="form.score_threshold" :min="0" :max="1" :step="0.01" :precision="2" style="width:100%" /></el-form-item>
          <el-form-item label="Dataset权限">
            <el-select v-model="form.dataset_permission" style="width: 100%" placeholder="请选择">
              <el-option value="only_me" label="only_me（仅自己可见）" />
              <el-option value="all_team_members" label="all_team_members（团队可见）" />
              <el-option value="partial_members" label="partial_members（部分成员可见）" />
            </el-select>
            <div style="color:#909399; font-size:12px; line-height:1.4; margin-top:6px;">
              控制外部知识库平台中该 dataset 的可见范围，不影响本系统用户权限。
            </div>
          </el-form-item>
          <el-form-item label="Dataset提供方"><el-input v-model="form.dataset_provider" placeholder="vendor" /></el-form-item>
          <el-form-item label="索引策略">
            <el-select v-model="form.dataset_indexing_technique" style="width: 100%" placeholder="请选择">
              <el-option value="high_quality" label="high_quality（高质量）" />
              <el-option value="economy" label="economy（经济）" />
            </el-select>
          </el-form-item>
        </template>
        <template v-else-if="form.provider === 'ragflow'">
          <el-form-item label="Base URL"><el-input v-model="form.base_url" :placeholder="DEFAULT_RAGFLOW_BASE_URL" /></el-form-item>
          <el-form-item label="API Key"><el-input v-model="form.api_key" type="password" show-password /></el-form-item>
          <el-form-item label="相似度阈值"><el-input-number v-model="form.similarity_threshold" :min="0" :max="1" :step="0.01" :precision="2" style="width:100%" /></el-form-item>
          <el-form-item label="向量权重"><el-input-number v-model="form.vector_similarity_weight" :min="0" :max="1" :step="0.01" :precision="2" style="width:100%" /></el-form-item>
          <el-form-item label="启用关键词"><el-switch v-model="form.keyword" /></el-form-item>
          <el-form-item label="启用高亮"><el-switch v-model="form.highlight" /></el-form-item>
          <el-form-item label="Dataset权限">
            <el-select v-model="form.dataset_permission" style="width: 100%" placeholder="请选择">
              <el-option value="me" label="me（仅自己可见）" />
              <el-option value="team" label="team（团队可见）" />
            </el-select>
            <div style="color:#909399; font-size:12px; line-height:1.4; margin-top:6px;">
              控制外部知识库平台中该 dataset 的可见范围，不影响本系统用户权限。
            </div>
          </el-form-item>
          <el-form-item label="分块策略">
            <el-select v-model="form.dataset_chunk_method" style="width: 100%" placeholder="请选择">
              <el-option value="naive" label="naive" />
              <el-option value="qa" label="qa" />
              <el-option value="table" label="table" />
              <el-option value="paper" label="paper" />
            </el-select>
          </el-form-item>
        </template>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
        <el-form-item label="默认"><el-switch v-model="form.is_default" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '@/utils/api'

const items = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref(false)
const currentId = ref(null)

const DEFAULT_DIFY_BASE_URL = 'https://api.dify.ai/v1'
const DEFAULT_RAGFLOW_BASE_URL = 'http://127.0.0.1'
const DEFAULT_DIFY_SCORE_THRESHOLD = 0.2
const DEFAULT_RAGFLOW_SIMILARITY_THRESHOLD = 0.2

const form = reactive({
  name: '',
  config_id: '',
  provider: 'dify',
  base_url: DEFAULT_DIFY_BASE_URL,
  api_key: '',
  score_threshold: DEFAULT_DIFY_SCORE_THRESHOLD,
  dataset_permission: '',
  dataset_provider: '',
  dataset_indexing_technique: '',
  similarity_threshold: DEFAULT_RAGFLOW_SIMILARITY_THRESHOLD,
  vector_similarity_weight: 0.3,
  keyword: false,
  highlight: false,
  dataset_chunk_method: '',
  enabled: true,
  is_default: false
})

const loadData = async () => {
  loading.value = true
  try {
    const res = await api.get('/admin/knowledge-search-configs')
    items.value = res.data.data || []
  } finally {
    loading.value = false
  }
}

const applyProviderDefaults = (provider, force = false) => {
  if (provider === 'dify') {
    if (force || !form.base_url || form.base_url === DEFAULT_RAGFLOW_BASE_URL) {
      form.base_url = DEFAULT_DIFY_BASE_URL
    }
    if (force || Number.isNaN(Number(form.score_threshold))) {
      form.score_threshold = DEFAULT_DIFY_SCORE_THRESHOLD
    }
    return
  }
  if (provider === 'ragflow') {
    if (force || !form.base_url || form.base_url === DEFAULT_DIFY_BASE_URL) {
      form.base_url = DEFAULT_RAGFLOW_BASE_URL
    }
    if (force || Number.isNaN(Number(form.similarity_threshold))) {
      form.similarity_threshold = DEFAULT_RAGFLOW_SIMILARITY_THRESHOLD
    }
  }
}

const onProviderChange = (provider) => {
  applyProviderDefaults(provider, true)
}

const openDialog = (row = null) => {
  editing.value = !!row
  currentId.value = row?.id || null
  const data = row?.json_data ? JSON.parse(row.json_data || '{}') : {}
  const provider = row?.provider || 'dify'
  form.name = row?.name || ''
  form.config_id = row?.config_id || ''
  form.provider = provider
  form.base_url = data.base_url || (provider === 'ragflow' ? DEFAULT_RAGFLOW_BASE_URL : DEFAULT_DIFY_BASE_URL)
  form.api_key = data.api_key || ''
  form.score_threshold = Number(data.score_threshold ?? DEFAULT_DIFY_SCORE_THRESHOLD)
  form.dataset_permission = data.dataset_permission || ''
  form.dataset_provider = data.dataset_provider || ''
  form.dataset_indexing_technique = data.dataset_indexing_technique || ''
  form.similarity_threshold = Number(data.similarity_threshold ?? DEFAULT_RAGFLOW_SIMILARITY_THRESHOLD)
  form.vector_similarity_weight = Number(data.vector_similarity_weight ?? 0.3)
  form.keyword = !!data.keyword
  form.highlight = !!data.highlight
  form.dataset_chunk_method = data.dataset_chunk_method || ''
  form.enabled = row?.enabled ?? true
  form.is_default = row?.is_default ?? false
  if (!row) {
    applyProviderDefaults(provider, true)
  }
  dialogVisible.value = true
}

const submit = async () => {
  const payload = {
    type: 'knowledge_search',
    name: form.name,
    config_id: form.config_id,
    provider: form.provider,
    enabled: form.enabled,
    is_default: form.is_default,
    json_data: JSON.stringify(form.provider === 'dify' ? {
      base_url: form.base_url,
      api_key: form.api_key,
      score_threshold: form.score_threshold,
      dataset_permission: form.dataset_permission,
      dataset_provider: form.dataset_provider,
      dataset_indexing_technique: form.dataset_indexing_technique
    } : {
      base_url: form.base_url,
      api_key: form.api_key,
      similarity_threshold: form.similarity_threshold,
      vector_similarity_weight: form.vector_similarity_weight,
      keyword: form.keyword,
      highlight: form.highlight,
      dataset_permission: form.dataset_permission,
      dataset_chunk_method: form.dataset_chunk_method
    })
  }
  try {
    if (editing.value) {
      await api.put(`/admin/knowledge-search-configs/${currentId.value}`, payload)
    } else {
      await api.post('/admin/knowledge-search-configs', payload)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    await loadData()
  } catch (e) {
    ElMessage.error('保存失败')
  }
}

const toggle = async (id) => {
  await api.post(`/admin/configs/${id}/toggle`)
  await loadData()
}

const remove = async (id) => {
  try {
    await ElMessageBox.confirm('确认删除该配置吗？', '提示', { type: 'warning' })
    await api.delete(`/admin/knowledge-search-configs/${id}`)
    ElMessage.success('删除成功')
    await loadData()
  } catch {}
}

const getConfigSummary = (row) => {
  const data = row?.json_data ? JSON.parse(row.json_data || '{}') : {}
  if (row.provider === 'dify') {
    return `base_url: ${data.base_url || DEFAULT_DIFY_BASE_URL}; score_threshold: ${data.score_threshold ?? DEFAULT_DIFY_SCORE_THRESHOLD}`
  }
  if (row.provider === 'ragflow') {
    return `base_url: ${data.base_url || DEFAULT_RAGFLOW_BASE_URL}; similarity_threshold: ${data.similarity_threshold ?? DEFAULT_RAGFLOW_SIMILARITY_THRESHOLD}`
  }
  return '-'
}

onMounted(loadData)
</script>
