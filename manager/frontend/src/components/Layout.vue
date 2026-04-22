<template>
  <div v-if="!isMobileDevice" class="layout-shell">
    <aside class="sidebar-shell">
      <div class="sidebar-card apple-surface">
        <div class="brand-panel">
          <div class="brand-mark">XZ</div>
          <div class="brand-copy">
            <p class="brand-eyebrow">Control Center</p>
            <h3>小智管理系统</h3>
            <p>{{ authStore.isAdmin ? 'AI 服务与设备管理台' : '设备与智能体工作台' }}</p>
          </div>
        </div>

        <div class="sidebar-meta">
          <span class="apple-chip is-primary">{{ authStore.isAdmin ? '管理员模式' : '用户模式' }}</span>
          <span class="apple-chip is-success">在线中</span>
        </div>

        <el-scrollbar class="sidebar-scroll">
          <el-menu
            :default-active="$route.path"
            class="sidebar-menu"
            router
            unique-opened
            :collapse-transition="false"
          >
            <el-menu-item v-if="authStore.isAdmin" index="/dashboard">
              <el-icon><House /></el-icon>
              <span>仪表板</span>
            </el-menu-item>

            <el-menu-item v-if="!authStore.isAdmin" index="/agents">
              <el-icon><Connection /></el-icon>
              <span>智能体管理</span>
            </el-menu-item>

            <el-menu-item v-if="!authStore.isAdmin" index="/user/roles">
              <el-icon><User /></el-icon>
              <span>我的角色</span>
            </el-menu-item>

            <el-menu-item v-if="!authStore.isAdmin" index="/speakers">
              <el-icon><Microphone /></el-icon>
              <span>声纹管理</span>
            </el-menu-item>

            <el-menu-item v-if="!authStore.isAdmin" index="/voice-clones">
              <el-icon><Microphone /></el-icon>
              <span>声音复刻</span>
            </el-menu-item>

            <el-menu-item v-if="!authStore.isAdmin" index="/user/knowledge-bases">
              <el-icon><Document /></el-icon>
              <span>我的知识库</span>
            </el-menu-item>

            <el-sub-menu v-if="authStore.isAdmin" index="/admin/service-config">
              <template #title>
                <el-icon><Tools /></el-icon>
                <span>服务配置</span>
              </template>
              <el-menu-item index="/admin/ota-config">OTA 配置</el-menu-item>
              <el-menu-item index="/admin/mqtt-config">MQTT 配置</el-menu-item>
              <el-menu-item index="/admin/mqtt-server-config">MQTT Server 配置</el-menu-item>
              <el-menu-item index="/admin/udp-config">UDP 配置</el-menu-item>
              <el-sub-menu index="/admin/mcp-config-group">
                <template #title>MCP 配置</template>
                <el-menu-item index="/admin/mcp-config">配置</el-menu-item>
                <el-menu-item index="/admin/mcp-market">MCP 市场</el-menu-item>
              </el-sub-menu>
              <el-menu-item index="/admin/speaker-config">声纹识别配置</el-menu-item>
              <el-menu-item index="/admin/chat-settings">聊天设置</el-menu-item>
            </el-sub-menu>

            <el-sub-menu v-if="authStore.isAdmin" index="/admin/ai-config">
              <template #title>
                <el-icon><Cpu /></el-icon>
                <span>AI 配置</span>
              </template>
              <el-menu-item index="/admin/vad-config">VAD 配置</el-menu-item>
              <el-menu-item index="/admin/asr-config">ASR 配置</el-menu-item>
              <el-menu-item index="/admin/llm-config">LLM 配置</el-menu-item>
              <el-menu-item index="/admin/tts-config">TTS 配置</el-menu-item>
              <el-menu-item index="/admin/vision-config">Vision 配置</el-menu-item>
              <el-menu-item index="/admin/memory-config">Memory 配置</el-menu-item>
              <el-menu-item index="/admin/knowledge-search-config">知识库检索配置</el-menu-item>
            </el-sub-menu>

            <el-menu-item v-if="authStore.isAdmin" index="/voice-clones">
              <el-icon><Microphone /></el-icon>
              <span>声音复刻</span>
            </el-menu-item>

            <el-menu-item v-if="authStore.isAdmin" index="/admin/pool-stats">
              <el-icon><DataAnalysis /></el-icon>
              <span>资源池统计</span>
            </el-menu-item>

            <el-menu-item v-if="authStore.isAdmin" index="/admin/global-roles">
              <el-icon><Setting /></el-icon>
              <span>全局角色</span>
            </el-menu-item>

            <el-menu-item v-if="authStore.isAdmin" index="/admin/users">
              <el-icon><UserFilled /></el-icon>
              <span>用户管理</span>
            </el-menu-item>

            <el-menu-item v-if="authStore.isAdmin" index="/admin/devices">
              <el-icon><Iphone /></el-icon>
              <span>设备管理</span>
            </el-menu-item>

            <el-menu-item v-if="authStore.isAdmin" index="/admin/agents">
              <el-icon><Connection /></el-icon>
              <span>智能体管理</span>
            </el-menu-item>
          </el-menu>
        </el-scrollbar>
      </div>
    </aside>

    <div class="content-shell">
      <header class="header-shell">
        <div class="header-card apple-surface">
          <div class="header-copy">
            <p class="header-eyebrow">{{ authStore.isAdmin ? 'Admin Console' : 'User Workspace' }}</p>
            <div class="header-title-row">
              <div>
                <h1 class="header-title">{{ currentPageTitle }}</h1>
                <p class="header-subtitle">
                  {{ authStore.isAdmin ? '统一管理 AI 配置、设备连接和运行状态。' : '集中管理您的设备、智能体与语音能力。' }}
                </p>
              </div>
            </div>
          </div>

          <div class="header-actions">
            <template v-if="authStore.isAdmin">
              <router-link to="/admin/config-wizard" custom v-slot="{ navigate, isActive }">
                <el-button
                  class="header-nav-btn"
                  :class="{ 'is-active': isActive }"
                  plain
                  @click="navigate"
                >
                  <el-icon><Guide /></el-icon>
                  <span>配置向导</span>
                </el-button>
              </router-link>
              <router-link to="/admin/ota-config" custom v-slot="{ navigate, isActive }">
                <el-button
                  class="header-nav-btn"
                  :class="{ 'is-active': isActive }"
                  plain
                  @click="navigate"
                >
                  <el-icon><Upload /></el-icon>
                  <span>OTA 配置</span>
                </el-button>
              </router-link>
            </template>

            <el-dropdown @command="handleCommand">
              <button class="profile-button" type="button">
                <span class="profile-avatar">{{ usernameInitial }}</span>
                <span class="profile-copy">
                  <strong>{{ authStore.user?.username }}</strong>
                  <small>{{ authStore.isAdmin ? '管理员' : '普通用户' }}</small>
                </span>
                <el-icon><ArrowDown /></el-icon>
              </button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="!authStore.isAdmin" command="api-tokens">API Token</el-dropdown-item>
                  <el-dropdown-item command="logout">退出登录</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </header>

      <main class="main-shell">
        <router-view />
      </main>
    </div>
  </div>

  <MobileLayout v-else />
</template>

<script setup>
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { isMobile } from '../utils/device'
import MobileLayout from './MobileLayout.vue'
import {
  House,
  Monitor,
  Setting,
  User,
  ArrowDown,
  Tools,
  Cpu,
  UserFilled,
  Iphone,
  Connection,
  Microphone,
  DataAnalysis,
  Guide,
  Upload,
  Document
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const isMobileDevice = computed(() => isMobile())

const currentPageTitle = computed(() => route.meta?.title || (authStore.isAdmin ? '仪表板' : '我的智能体'))

const usernameInitial = computed(() => {
  const username = authStore.user?.username || 'U'
  return username.slice(0, 1).toUpperCase()
})

const handleCommand = async (command) => {
  if (command === 'api-tokens') {
    router.push('/user/api-tokens')
    return
  }

  if (command === 'logout') {
    try {
      await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      })

      authStore.logout()
      ElMessage.success('已退出登录')
      router.push('/login')
    } catch {
      // 用户取消
    }
  }
}
</script>

<style scoped>
.layout-shell {
  min-height: 100dvh;
  padding: 20px;
  display: grid;
  grid-template-columns: 292px minmax(0, 1fr);
  gap: 20px;
}

.sidebar-shell {
  min-width: 0;
}

.sidebar-card {
  height: calc(100dvh - 40px);
  padding: 18px;
  border-radius: 30px;
  display: flex;
  flex-direction: column;
}

.brand-panel {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 16px;
}

.brand-mark {
  width: 46px;
  height: 46px;
  border-radius: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(180deg, #2e90ff 0%, #007aff 100%);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.36), 0 12px 22px rgba(0, 122, 255, 0.24);
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.04em;
  flex: none;
}

.brand-copy h3 {
  margin: 0;
  font-size: 17px;
}

.brand-copy p {
  margin: 3px 0 0;
  color: var(--apple-text-secondary);
  font-size: 13px;
}

.brand-eyebrow {
  margin: 0 0 4px !important;
  color: var(--apple-primary) !important;
  font-size: 11px !important;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.sidebar-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 18px;
}

.sidebar-scroll {
  flex: 1;
  min-height: 0;
  margin: 0 -4px -4px;
  padding-right: 4px;
}

.sidebar-menu {
  background: transparent;
  border-right: 0;
  padding: 2px 0 12px;
}

.sidebar-menu :deep(.el-menu-item),
.sidebar-menu :deep(.el-sub-menu__title) {
  height: 46px;
  margin-bottom: 6px;
  border-radius: 16px;
  color: var(--apple-text-secondary);
  font-weight: 600;
}

.sidebar-menu :deep(.el-menu-item:hover),
.sidebar-menu :deep(.el-sub-menu__title:hover) {
  color: var(--apple-text);
  background: rgba(255, 255, 255, 0.82);
}

.sidebar-menu :deep(.el-menu-item.is-active) {
  color: var(--apple-primary);
  background: rgba(0, 122, 255, 0.1);
  box-shadow: inset 0 0 0 1px rgba(0, 122, 255, 0.08);
}

.sidebar-menu :deep(.el-sub-menu .el-menu-item) {
  height: 40px;
  margin: 4px 0 4px 8px;
  padding-left: 20px !important;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.48);
}

.sidebar-menu :deep(.el-menu-item .el-icon),
.sidebar-menu :deep(.el-sub-menu__title .el-icon) {
  margin-right: 12px;
  font-size: 17px;
}

.content-shell {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header-shell {
  padding: 0;
  height: auto;
}

.header-card {
  min-height: 110px;
  padding: 22px 24px;
  border-radius: 30px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}

.header-copy {
  min-width: 0;
}

.header-eyebrow {
  margin: 0 0 8px;
  color: var(--apple-primary);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.header-title {
  margin: 0;
  font-size: 30px;
  line-height: 1.08;
  letter-spacing: -0.04em;
}

.header-subtitle {
  margin: 8px 0 0;
  color: var(--apple-text-secondary);
  font-size: 14px;
  line-height: 1.7;
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 12px;
}

.header-nav-btn {
  min-height: 42px;
  padding-inline: 16px;
}

.header-nav-btn.is-active {
  border-color: transparent;
  background: rgba(0, 122, 255, 0.1);
  color: var(--apple-primary);
}

.profile-button {
  min-height: 48px;
  padding: 8px 12px 8px 8px;
  border-radius: 999px;
  border: 1px solid var(--apple-line);
  background: rgba(255, 255, 255, 0.86);
  box-shadow: var(--apple-shadow-sm);
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  color: var(--apple-text);
}

.profile-button:hover {
  transform: translateY(-1px);
  box-shadow: var(--apple-shadow-md);
}

.profile-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(180deg, #eff6ff 0%, #dcebff 100%);
  color: var(--apple-primary);
  font-size: 13px;
  font-weight: 700;
  flex: none;
}

.profile-copy {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.2;
}

.profile-copy strong {
  font-size: 13px;
}

.profile-copy small {
  color: var(--apple-text-secondary);
  font-size: 11px;
}

.main-shell {
  min-width: 0;
  padding: 0 4px 4px 0;
}

@media (max-width: 1360px) {
  .layout-shell {
    grid-template-columns: 268px minmax(0, 1fr);
  }

  .header-card {
    flex-direction: column;
    align-items: flex-start;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
