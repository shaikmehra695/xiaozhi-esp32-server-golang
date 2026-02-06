import api from './api'

/**
 * 测试单个或单类配置
 * @param {string} type - 类型：ota | vad | asr | llm | tts
 * @param {string} [configId] - 可选，指定 config_id 则只测该条
 * @returns {Promise<{ ok: boolean, message: string }>} 单条时直接返回结果；多条时返回第一条或汇总
 */
export async function testSingleConfig(type, configId) {
  const body = {
    types: [type],
    config_ids: configId ? { [type]: [configId] } : {}
  }
  const res = await api.post('/admin/configs/test', body, { timeout: 30000 })
  const data = res.data?.data ?? res.data
  const typeResult = data?.[type]
  if (!typeResult || typeof typeResult !== 'object') {
    return { ok: false, message: '未返回测试结果' }
  }
  const entries = Object.entries(typeResult).filter(([k]) => !k.startsWith('_'))
  if (configId && typeResult[configId]) {
    const item = typeResult[configId]
    return { ok: !!item.ok, message: item.message || '' }
  }
  if (entries.length === 0) {
    const err = typeResult._error || typeResult._no_client || typeResult._none
    const msg = err && typeof err === 'object' ? (err.message || '').trim() : ''
    const fallback = typeResult._none ? '未配置或未启用' : '无测试结果'
    return { ok: false, message: msg || fallback }
  }
  const first = entries[0][1]
  return { ok: !!first.ok, message: first.message || '' }
}

/**
 * 测试某类型全部配置，返回按 config_id 的结果（用于“测试全部”并在每行展示）
 * @param {string} type - 类型：vad | asr | llm | tts
 * @returns {Promise<Record<string, { ok: boolean, message: string }>>} config_id -> { ok, message }
 */
export async function testAllConfigs(type) {
  const body = { types: [type] }
  const res = await api.post('/admin/configs/test', body, { timeout: 60000 })
  const data = res.data?.data ?? res.data
  const typeResult = data?.[type]
  const out = {}
  if (!typeResult || typeof typeResult !== 'object') {
    return out
  }
  const err = typeResult._error || typeResult._no_client || typeResult._none
  const errMsg = err && typeof err === 'object' ? (err.message || '').trim() : '未返回测试结果'
  for (const [k, v] of Object.entries(typeResult)) {
    if (k.startsWith('_')) continue
    const item = v && typeof v === 'object' ? v : {}
    out[k] = { ok: !!item.ok, message: item.message || '' }
  }
  if (Object.keys(out).length === 0 && errMsg) {
    out._global = { ok: false, message: errMsg }
  }
  return out
}

/**
 * 将 getJsonData() 返回值转为可合并对象（表单返回的是 JSON 字符串）
 * @param {string|object} jsonData - getJsonData() 返回值
 * @returns {object}
 */
export function parseJsonData(jsonData) {
  if (jsonData == null) return {}
  if (typeof jsonData === 'object') return jsonData
  if (typeof jsonData !== 'string') return {}
  try {
    return JSON.parse(jsonData) || {}
  } catch {
    return {}
  }
}

/**
 * 使用自定义 data 测试（未保存草稿 / 向导当前步）
 * @param {string} type - 类型：vad | asr | llm | tts
 * @param {Record<string, object>} typeData - 该类型下 config_id -> 配置对象，与接口 data[type] 一致
 * @returns {Promise<{ ok: boolean, message: string }>} 单条结果（仅支持单条）
 */
export async function testWithData(type, typeData) {
  const body = { types: [type], data: { [type]: typeData } }
  const res = await api.post('/admin/configs/test', body, { timeout: 30000 })
  const data = res.data?.data ?? res.data
  const typeResult = data?.[type]
  if (!typeResult || typeof typeResult !== 'object') {
    return { ok: false, message: '未返回测试结果' }
  }
  const err = typeResult._error || typeResult._no_client
  if (err && typeof err === 'object' && err.message) {
    return { ok: false, message: err.message }
  }
  const entries = Object.entries(typeResult).filter(([k]) => !k.startsWith('_'))
  if (entries.length === 0) {
    return { ok: false, message: typeResult._none?.message || '无测试结果' }
  }
  const first = entries[0][1]
  return { ok: !!first.ok, message: first.message || '' }
}
