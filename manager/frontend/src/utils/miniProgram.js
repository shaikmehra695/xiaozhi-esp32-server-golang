const TOKEN_QUERY_KEY = 'token'
const USER_QUERY_KEY = 'user'
const USER_B64_QUERY_KEY = 'user_b64'
const REDIRECT_QUERY_KEY = 'redirect'

const safeJsonParse = (raw) => {
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch (e) {
    return null
  }
}

const decodeBase64Url = (raw) => {
  try {
    const base64 = raw.replace(/-/g, '+').replace(/_/g, '/')
    const padding = '='.repeat((4 - (base64.length % 4)) % 4)
    return atob(base64 + padding)
  } catch (e) {
    return ''
  }
}

export const isMiniProgramWebView = () => {
  const ua = (navigator.userAgent || '').toLowerCase()
  if (ua.includes('miniprogram') || ua.includes('micromessenger')) {
    return true
  }

  if (typeof window !== 'undefined' && window.__wxjs_environment === 'miniprogram') {
    return true
  }

  return false
}

export const initMiniProgramAuthFromQuery = () => {
  if (typeof window === 'undefined') {
    return null
  }

  const url = new URL(window.location.href)
  const token = url.searchParams.get(TOKEN_QUERY_KEY)
  const userRaw = url.searchParams.get(USER_QUERY_KEY)
  const userB64 = url.searchParams.get(USER_B64_QUERY_KEY)
  const redirect = url.searchParams.get(REDIRECT_QUERY_KEY)

  if (!token) {
    return null
  }

  let user = safeJsonParse(userRaw)
  if (!user && userB64) {
    user = safeJsonParse(decodeBase64Url(userB64))
  }

  localStorage.setItem('token', token)
  if (user) {
    localStorage.setItem('user', JSON.stringify(user))
  }

  url.searchParams.delete(TOKEN_QUERY_KEY)
  url.searchParams.delete(USER_QUERY_KEY)
  url.searchParams.delete(USER_B64_QUERY_KEY)
  const cleanUrl = `${url.pathname}${url.search}${url.hash}`
  window.history.replaceState({}, '', cleanUrl)

  return {
    token,
    user,
    redirect
  }
}

export const notifyMiniProgram = (event, payload = {}) => {
  if (typeof window === 'undefined') {
    return
  }

  try {
    if (window.wx?.miniProgram?.postMessage) {
      window.wx.miniProgram.postMessage({ data: { event, ...payload } })
      return
    }
  } catch (e) {
    // ignore and fallback
  }

  try {
    window.parent?.postMessage?.({ event, ...payload }, '*')
  } catch (e) {
    // ignore
  }
}
