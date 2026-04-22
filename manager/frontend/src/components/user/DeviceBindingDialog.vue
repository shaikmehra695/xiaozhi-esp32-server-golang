<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="480px"
    :close-on-click-modal="false"
    @closed="resetForm"
  >
    <div class="device-binding-dialog">
      <div class="device-binding-hero">
        <div class="hero-icon">
          <el-icon><Monitor /></el-icon>
        </div>
        <div>
          <h3>{{ heroTitle }}</h3>
          <p>支持填写设备验证码或设备 MAC，绑定后会自动关联到目标智能体。</p>
        </div>
      </div>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
      >
        <el-form-item v-if="!hasFixedAgent" label="目标智能体" prop="agent_id">
          <el-select
            v-model="form.agent_id"
            placeholder="请选择要绑定的智能体"
            style="width: 100%"
            filterable
          >
            <el-option
              v-for="agent in agents"
              :key="agent.id"
              :label="agent.name"
              :value="agent.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="设备验证码或 MAC" prop="identifier">
          <el-input
            v-model="form.identifier"
            placeholder="请输入 6 位验证码或设备 MAC"
            clearable
            autocomplete="off"
          />
        </el-form-item>

        <div class="form-hint">
          <span>示例：</span>
          <code>123456</code>
          <code>28:0A:C6:1D:3B:E8</code>
        </div>
      </el-form>
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleClose">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">
          {{ submitting ? '绑定中...' : '绑定设备' }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Monitor } from '@element-plus/icons-vue'
import api from '../../utils/api'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  },
  agents: {
    type: Array,
    default: () => []
  },
  fixedAgentId: {
    type: [Number, String, null],
    default: null
  },
  title: {
    type: String,
    default: '添加设备'
  }
})

const emit = defineEmits(['update:modelValue', 'success'])

const formRef = ref()
const submitting = ref(false)
const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const form = reactive({
  agent_id: '',
  identifier: ''
})

const hasFixedAgent = computed(() => props.fixedAgentId !== null && props.fixedAgentId !== undefined && props.fixedAgentId !== '')
const dialogTitle = computed(() => props.title || '添加设备')
const heroTitle = computed(() => hasFixedAgent.value ? '绑定到当前智能体' : '绑定设备到智能体')

const validateIdentifier = (_, value, callback) => {
  const text = String(value || '').trim()
  if (!text) {
    callback(new Error('请输入设备验证码或设备 MAC'))
    return
  }
  callback()
}

const rules = computed(() => ({
  agent_id: hasFixedAgent.value
    ? []
    : [{ required: true, message: '请选择目标智能体', trigger: 'change' }],
  identifier: [{ validator: validateIdentifier, trigger: 'blur' }]
}))

const resetForm = () => {
  form.agent_id = hasFixedAgent.value ? String(props.fixedAgentId) : ''
  form.identifier = ''
  formRef.value?.clearValidate?.()
}

watch(
  () => props.modelValue,
  (visible) => {
    if (visible) {
      resetForm()
    }
  }
)

watch(
  () => props.fixedAgentId,
  () => {
    if (props.modelValue) {
      resetForm()
    }
  }
)

const closeDialog = () => {
  visible.value = false
}

const buildPayload = (identifier) => {
  const text = identifier.trim()
  if (/^\d{6}$/.test(text)) {
    return { code: text }
  }
  return { device_mac: text }
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
  } catch {
    return
  }

  const agentId = hasFixedAgent.value ? props.fixedAgentId : form.agent_id
  if (!agentId) return

  submitting.value = true
  try {
    const response = await api.post(`/user/agents/${agentId}/devices`, buildPayload(form.identifier))
    if (response.data?.success) {
      ElMessage.success('设备绑定成功')
      emit('success', response.data?.data || null)
      closeDialog()
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '设备绑定失败')
  } finally {
    submitting.value = false
  }
}

const handleClose = () => {
  resetForm()
  closeDialog()
}
</script>

<style scoped>
.device-binding-dialog {
  display: grid;
  gap: 18px;
}

.device-binding-hero {
  display: flex;
  gap: 14px;
  padding: 16px 18px;
  border-radius: 20px;
  border: 1px solid rgba(229, 229, 234, 0.78);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.96) 0%, rgba(248, 250, 252, 0.92) 100%);
}

.hero-icon {
  width: 46px;
  height: 46px;
  border-radius: 16px;
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(180deg, #2e90ff 0%, #007aff 100%);
  box-shadow: 0 12px 24px rgba(0, 122, 255, 0.18);
}

.device-binding-hero h3 {
  margin: 0 0 4px;
  font-size: 16px;
  color: var(--apple-text);
}

.device-binding-hero p {
  margin: 0;
  font-size: 13px;
  color: var(--apple-text-secondary);
  line-height: 1.6;
}

.form-hint {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12px;
  color: var(--apple-text-secondary);
}

.form-hint code {
  padding: 4px 8px;
  border-radius: 999px;
  background: rgba(0, 122, 255, 0.08);
  color: var(--apple-primary);
  font-family: ui-monospace, SFMono-Regular, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 768px) {
  .dialog-footer {
    flex-wrap: wrap;
  }

  .dialog-footer .el-button {
    flex: 1;
    min-width: 120px;
  }
}
</style>
