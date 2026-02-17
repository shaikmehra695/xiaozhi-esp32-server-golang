const getBaseURL = () => {
  const app = getApp()
  const baseURL = app?.globalData?.baseURL || wx.getStorageSync('baseURL')
  return (baseURL || '').replace(/\/$/, '')
}

const request = ({ url, method = 'GET', data, withAuth = true }) => {
  const token = wx.getStorageSync('token')
  const headers = {
    'Content-Type': 'application/json'
  }

  if (withAuth && token) {
    headers.Authorization = `Bearer ${token}`
  }

  return new Promise((resolve, reject) => {
    wx.request({
      url: `${getBaseURL()}${url}`,
      method,
      data,
      header: headers,
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data)
          return
        }

        if (res.statusCode === 401) {
          wx.removeStorageSync('token')
          wx.removeStorageSync('user')
          wx.reLaunch({ url: '/pages/login/index' })
        }

        reject({ statusCode: res.statusCode, data: res.data })
      },
      fail: reject
    })
  })
}

const api = {
  login(payload) {
    return request({ url: '/api/login', method: 'POST', data: payload, withAuth: false })
  },
  getProfile() {
    return request({ url: '/api/profile' })
  },
  getDashboardStats() {
    return request({ url: '/api/dashboard/stats' })
  },
  getAgents() {
    return request({ url: '/api/user/agents' })
  },
  getDevices() {
    return request({ url: '/api/user/devices' })
  }
}

module.exports = api
