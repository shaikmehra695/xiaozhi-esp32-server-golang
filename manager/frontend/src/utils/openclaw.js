const OPENCLAW_PLUGIN_INSTALL_COMMAND = 'openclaw plugins install @xiaozhi_openclaw/xiaozhi'
const OPENCLAW_GATEWAY_RESTART_COMMAND = 'openclaw gateway restart'
const OPENCLAW_CHANNEL_NAME = 'xiaozhi'

const EMPTY_COMMAND_DATA = {
  ready: false,
  url: '',
  token: '',
  steps: [],
  commands: [],
  copyText: ''
}

export function buildOpenClawCommands(endpoint) {
  const trimmedEndpoint = String(endpoint || '').trim()
  if (!trimmedEndpoint) {
    return EMPTY_COMMAND_DATA
  }

  try {
    const parsed = new URL(trimmedEndpoint)
    const token = String(parsed.searchParams.get('token') || '').trim()
    parsed.search = ''
    parsed.hash = ''

    const url = parsed.toString()
    if (!url || !token) {
      return EMPTY_COMMAND_DATA
    }

    const steps = [
      {
        title: '安装插件',
        command: OPENCLAW_PLUGIN_INSTALL_COMMAND
      },
      {
        title: '配置渠道',
        command: `openclaw channels add --channel ${OPENCLAW_CHANNEL_NAME} --url ${url} --token ${token}`
      },
      {
        title: '重启网关',
        command: OPENCLAW_GATEWAY_RESTART_COMMAND
      }
    ]
    const commands = steps.map((step) => step.command)

    return {
      ready: true,
      url,
      token,
      steps,
      commands,
      copyText: commands.join('\n')
    }
  } catch (error) {
    console.error('解析 OpenClaw endpoint 失败:', error)
    return EMPTY_COMMAND_DATA
  }
}
