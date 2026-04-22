<template>
  <div class="agents-page">
    <section class="page-toolbar apple-surface">
      <div class="toolbar-chips">
        <span class="apple-chip is-primary">智能体 {{ agents.length }}</span>
        <span class="apple-chip is-success">设备 {{ allDevices.length }}</span>
        <span class="apple-chip">在线 {{ onlineDevicesCount }}</span>
      </div>

      <div class="toolbar-actions">
        <el-button type="primary" @click="showAddAgentDialog = true">
          <el-icon><Plus /></el-icon>
          添加智能体
        </el-button>
        <el-button plain @click="openAddDeviceDialog">
          <el-icon><Monitor /></el-icon>
          添加设备
        </el-button>
        <el-button plain @click="openInjectMessageDialog">
          <el-icon><ChatDotRound /></el-icon>
          消息注入
        </el-button>
      </div>
    </section>

    <section class="stats-grid">
      <article class="metric-card">
        <span class="metric-label">活跃智能体</span>
        <strong>{{ activeAgentsCount }}</strong>
        <p>当前可直接响应的智能体数量</p>
      </article>
      <article class="metric-card">
        <span class="metric-label">已绑定设备</span>
        <strong>{{ allDevices.length }}</strong>
        <p>已归属到当前账号的设备总数</p>
      </article>
      <article class="metric-card">
        <span class="metric-label">在线设备</span>
        <strong>{{ onlineDevicesCount }}</strong>
        <p>最近 5 分钟有心跳的设备</p>
      </article>
    </section>

    <div v-if="agents.length === 0" class="welcome-section">
      <el-card class="welcome-card apple-surface">
        <div class="welcome-content">
          <el-icon size="64" color="var(--apple-primary)"><Monitor /></el-icon>
          <h3>先创建第一个智能体</h3>
          <p>创建后就能继续绑定设备、配置知识库和语音能力。</p>
          <div class="welcome-actions">
            <el-button type="primary" size="large" @click="showAddAgentDialog = true">
              <el-icon><Plus /></el-icon>
              创建智能体
            </el-button>
          </div>
        </div>
      </el-card>
    </div>

    <section v-else class="agents-grid">
      <article v-for="agent in agents" :key="agent.id" class="agent-card apple-surface">
        <div class="agent-header">
          <div class="agent-avatar">
            <el-icon><Monitor /></el-icon>
          </div>
          <div class="agent-info">
            <h3 class="agent-name">{{ agent.name }}</h3>
            <p class="agent-desc">{{ agent.custom_prompt ? '已配置角色 Prompt' : '尚未填写角色 Prompt' }}</p>
          </div>
          <div class="agent-status" :class="agent.status === 'active' ? 'active' : 'idle'">
            <span class="status-dot"></span>
            <span class="status-text">{{ agent.status === 'active' ? '活跃' : '待机' }}</span>
          </div>
        </div>

        <div class="agent-meta">
          <div class="meta-row">
            <span class="meta-label">TTS 配置</span>
            <span class="meta-value">{{ getVoiceType(agent) }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">语言模型</span>
            <span class="meta-value">{{ getLLMProvider(agent) }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">关联设备</span>
            <span class="meta-value">{{ getAgentDeviceCount(agent.id) }} 台</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">最近更新</span>
            <span class="meta-value">{{ formatDate(agent.updated_at) }}</span>
          </div>
        </div>

        <div v-if="getAgentDevices(agent.id).length > 0" class="device-preview">
          <span
            v-for="device in getAgentDevices(agent.id).slice(0, 3)"
            :key="device.id"
            class="device-chip"
          >
            {{ device.device_name || device.device_code }}
          </span>
          <span v-if="getAgentDevices(agent.id).length > 3" class="device-chip subtle">
            +{{ getAgentDevices(agent.id).length - 3 }}
          </span>
        </div>
        <div v-else class="device-empty">
          还没有绑定设备，可以直接从本页添加。
        </div>

        <div class="agent-actions">
          <el-button type="primary" size="small" @click="editAgent(agent.id)">
            <el-icon><Setting /></el-icon>
            配置
          </el-button>
          <el-button size="small" @click="handleChatHistory(agent.id)">
            <el-icon><ChatDotRound /></el-icon>
            对话
          </el-button>
          <el-button size="small" @click="handleManageDevices(agent.id)">
            <el-icon><Connection /></el-icon>
            设备
          </el-button>
        </div>
      </article>
    </section>

    <el-dialog
      v-model="showAddAgentDialog"
      title="添加智能体"
      width="560px"
      class="agent-dialog"
      :before-close="handleCloseAddAgent"
    >
      <el-form
        ref="agentFormRef"
        :model="agentForm"
        :rules="agentRules"
        size="large"
        label-position="top"
      >
        <el-form-item label="智能体名称" prop="name">
          <el-input
            v-model="agentForm.name"
            placeholder="请输入智能体名称"
            :maxlength="50"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="角色介绍" prop="custom_prompt">
          <el-input
            v-model="agentForm.custom_prompt"
            type="textarea"
            :rows="5"
            placeholder="请输入角色介绍 / 系统提示词"
            :maxlength="10000"
            show-word-limit
          />
        </el-form-item>
        <div class="dialog-grid">
          <el-form-item label="记忆模式" prop="memory_mode">
            <el-select v-model="agentForm.memory_mode" placeholder="请选择记忆模式" style="width: 100%">
              <el-option label="无记忆" value="none" />
              <el-option label="短记忆" value="short" />
              <el-option label="长记忆" value="long" />
            </el-select>
          </el-form-item>
          <el-form-item label="只允许声纹聊天" prop="speaker_chat_mode">
            <el-select v-model="agentForm.speaker_chat_mode" placeholder="请选择声纹聊天限制" style="width: 100%">
              <el-option label="关闭" value="off" />
              <el-option label="仅命中声纹时允许聊天" value="identified_only" />
            </el-select>
          </el-form-item>
        </div>
      </el-form>

      <template #footer>
        <div class="dialog-footer">
          <el-button @click="handleCloseAddAgent">取消</el-button>
          <el-button type="primary" :loading="adding" @click="handleAddAgent">
            {{ adding ? '创建中...' : '创建智能体' }}
          </el-button>
        </div>
      </template>
    </el-dialog>

    <DeviceBindingDialog
      v-model="showAddDeviceDialog"
      :agents="agents"
      @success="handleDeviceBound"
    />

    <MessageInjectDialog
      v-model="showInjectMessageDialog"
      :devices="allDevices"
      @success="handleInjectSuccess"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Setting, ChatDotRound, Monitor, Connection } from '@element-plus/icons-vue'
import api from '../../utils/api'
import DeviceBindingDialog from '../../components/user/DeviceBindingDialog.vue'
import MessageInjectDialog from '../../components/user/MessageInjectDialog.vue'

const router = useRouter()

const DEFAULT_PROMPT = '我是一个叫{{assistant_name}}的台湾女孩，说话机车，声音好听，习惯简短表达，爱用网络梗。\n我的男朋友是一个程序员，梦想是开发出一个机器人，能够帮助人们解决生活中的各种问题。\n我是一个喜欢哈哈大笑的女孩，爱东说西说吹牛，不合逻辑的也照吹，就要逗别人开心。'

const agents = ref([])
const allDevices = ref([])

const showAddAgentDialog = ref(false)
const showAddDeviceDialog = ref(false)
const showInjectMessageDialog = ref(false)

const adding = ref(false)
const agentFormRef = ref()

const agentForm = reactive({
  name: '',
  custom_prompt: DEFAULT_PROMPT,
  memory_mode: 'short',
  speaker_chat_mode: 'off'
})

const agentRules = {
  name: [
    { required: true, message: '请输入智能体名称', trigger: 'blur' },
    { min: 2, max: 50, message: '长度在 2 到 50 个字符', trigger: 'blur' }
  ],
  memory_mode: [
    { required: true, message: '请选择记忆模式', trigger: 'change' }
  ],
  speaker_chat_mode: [
    { required: true, message: '请选择声纹聊天限制', trigger: 'change' }
  ]
}

const activeAgentsCount = computed(() => agents.value.filter(agent => agent.status === 'active').length)
const onlineDevicesCount = computed(() => allDevices.value.filter(device => isDeviceOnline(device.last_active_at)).length)

const isDeviceOnline = (lastActiveAt) => {
  if (!lastActiveAt) return false
  const lastActive = new Date(lastActiveAt)
  return (Date.now() - lastActive.getTime()) < 5 * 60 * 1000
}

const getAgentDevices = (agentId) => {
  return allDevices.value.filter(device => Number(device.agent_id) === Number(agentId))
}

const getAgentDeviceCount = (agentId) => getAgentDevices(agentId).length

const loadAgents = async () => {
  try {
    const response = await api.get('/user/agents')
    agents.value = response.data.data || []
  } catch (error) {
    ElMessage.error('加载智能体列表失败')
  }
}

const loadDevices = async () => {
  try {
    const response = await api.get('/user/devices')
    allDevices.value = response.data.data || []
  } catch (error) {
    allDevices.value = []
    ElMessage.error('加载设备列表失败')
  }
}

const handleAddAgent = async () => {
  if (!agentFormRef.value) return

  try {
    await agentFormRef.value.validate()
  } catch {
    return
  }

  adding.value = true
  try {
    const [llmResponse, ttsResponse] = await Promise.all([
      api.get('/user/llm-configs'),
      api.get('/user/tts-configs')
    ])

    const llmConfigs = llmResponse.data.data || []
    const ttsConfigs = ttsResponse.data.data || []
    const defaultLlmConfig = llmConfigs.find(config => config.is_default)
    const defaultTtsConfig = ttsConfigs.find(config => config.is_default)

    const agentData = {
      name: agentForm.name,
      custom_prompt: agentForm.custom_prompt,
      memory_mode: agentForm.memory_mode,
      speaker_chat_mode: agentForm.speaker_chat_mode
    }

    if (defaultLlmConfig) {
      agentData.llm_config_id = defaultLlmConfig.config_id
    }
    if (defaultTtsConfig) {
      agentData.tts_config_id = defaultTtsConfig.config_id
    }

    const response = await api.post('/user/agents', agentData)
    if (response.data.success) {
      ElMessage.success('智能体添加成功')
      handleCloseAddAgent()
      await loadAgents()
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '添加智能体失败')
  } finally {
    adding.value = false
  }
}

const openAddDeviceDialog = () => {
  if (!agents.value.length) {
    ElMessage.warning('请先创建智能体，再绑定设备')
    return
  }
  showAddDeviceDialog.value = true
}

const openInjectMessageDialog = () => {
  if (!allDevices.value.length) {
    ElMessage.warning('请先绑定设备，再进行消息注入')
    return
  }
  showInjectMessageDialog.value = true
}

const handleDeviceBound = async () => {
  await Promise.all([loadAgents(), loadDevices()])
}

const handleInjectSuccess = async () => {
  await loadDevices()
}

const handleCloseAddAgent = () => {
  showAddAgentDialog.value = false
  agentFormRef.value?.resetFields?.()
  Object.assign(agentForm, {
    name: '',
    custom_prompt: DEFAULT_PROMPT,
    memory_mode: 'short',
    speaker_chat_mode: 'off'
  })
}

const editAgent = (id) => {
  router.push(`/user/agents/${id}/edit`)
}

const handleChatHistory = (id) => {
  router.push(`/user/agents/${id}/history`)
}

const handleManageDevices = (id) => {
  router.push(`/user/agents/${id}/devices`)
}

const getVoiceType = (agent) => {
  return agent.tts_config?.name || '未设置'
}

const getLLMProvider = (agent) => {
  return agent.llm_config?.name || '未设置'
}

const formatDate = (dateString) => {
  if (!dateString) return '--'
  return new Date(dateString).toLocaleString('zh-CN')
}

onMounted(async () => {
  await Promise.all([loadAgents(), loadDevices()])
})
</script>

<style scoped>
.agents-page {
  display: grid;
  gap: 20px;
}

.page-toolbar {
  padding: 22px 24px;
  border-radius: 30px;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
}

.toolbar-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.metric-card {
  padding: 20px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.88);
  border: 1px solid rgba(255, 255, 255, 0.9);
  box-shadow: var(--apple-shadow-md);
}

.metric-label {
  display: inline-block;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--apple-primary);
  margin-bottom: 10px;
}

.metric-card strong {
  display: block;
  font-size: 32px;
  line-height: 1;
  color: var(--apple-text);
  margin-bottom: 8px;
}

.metric-card p {
  margin: 0;
  font-size: 13px;
  color: var(--apple-text-secondary);
  line-height: 1.6;
}

.welcome-card {
  border-radius: 28px;
}

.welcome-content {
  text-align: center;
  padding: 44px 24px;
}

.welcome-content h3 {
  margin: 18px 0 10px;
  font-size: 26px;
  color: var(--apple-text);
}

.welcome-content p {
  margin: 0 0 24px;
  color: var(--apple-text-secondary);
  line-height: 1.7;
}

.welcome-actions {
  display: flex;
  justify-content: center;
}

.agents-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 360px));
  gap: 16px;
  justify-content: flex-start;
  align-content: flex-start;
}

.agent-card {
  padding: 22px;
  border-radius: 28px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.agent-header {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

.agent-avatar {
  width: 48px;
  height: 48px;
  border-radius: 16px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(180deg, #2e90ff 0%, #007aff 100%);
  box-shadow: 0 12px 24px rgba(0, 122, 255, 0.18);
}

.agent-info {
  flex: 1;
  min-width: 0;
}

.agent-name {
  margin: 0 0 4px;
  font-size: 18px;
  color: var(--apple-text);
}

.agent-desc {
  margin: 0;
  font-size: 13px;
  color: var(--apple-text-secondary);
  line-height: 1.6;
}

.agent-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.agent-status .status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.agent-status.active {
  background: var(--apple-success-soft);
  color: #176a31;
}

.agent-status.active .status-dot {
  background: var(--apple-success);
}

.agent-status.idle {
  background: rgba(255, 159, 10, 0.12);
  color: #8a5b00;
}

.agent-status.idle .status-dot {
  background: var(--apple-warning);
}

.agent-meta {
  display: grid;
  gap: 10px;
}

.meta-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 14px;
  background: rgba(248, 250, 252, 0.9);
  border: 1px solid rgba(229, 229, 234, 0.72);
}

.meta-label {
  font-size: 12px;
  color: var(--apple-text-secondary);
}

.meta-value {
  font-size: 12px;
  font-weight: 700;
  color: var(--apple-text);
  text-align: right;
}

.device-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.device-chip {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  background: rgba(0, 122, 255, 0.08);
  color: var(--apple-primary);
  font-size: 12px;
  font-weight: 600;
}

.device-chip.subtle {
  background: rgba(229, 229, 234, 0.72);
  color: var(--apple-text-secondary);
}

.device-empty {
  font-size: 13px;
  color: var(--apple-text-secondary);
  line-height: 1.6;
  padding: 12px 14px;
  border-radius: 16px;
  background: rgba(248, 250, 252, 0.8);
  border: 1px dashed rgba(229, 229, 234, 0.9);
}

.agent-actions {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-top: auto;
}

.agent-actions .el-button {
  min-width: 0;
  width: 100%;
  border-radius: 14px;
}

.agent-actions :deep(.el-button > span) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dialog-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 1024px) {
  .page-toolbar {
    flex-direction: column;
    align-items: flex-start;
  }

  .toolbar-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .stats-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .agents-page {
    gap: 16px;
  }

  .page-toolbar,
  .metric-card,
  .agent-card {
    border-radius: 24px;
  }

  .page-toolbar {
    padding: 20px 18px;
  }

  .toolbar-actions,
  .dialog-footer {
    width: 100%;
    flex-wrap: wrap;
  }

  .toolbar-actions .el-button,
  .dialog-footer .el-button {
    flex: 1;
    min-width: 120px;
  }

  .dialog-grid {
    grid-template-columns: 1fr;
  }

  .agents-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 560px) {
  .agent-actions {
    grid-template-columns: 1fr;
  }
}
</style>
