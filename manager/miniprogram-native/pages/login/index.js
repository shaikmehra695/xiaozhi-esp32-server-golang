const api = require('../../utils/api')

Page({
  data: {
    baseURL: '',
    username: '',
    password: '',
    loading: false
  },

  onLoad() {
    const app = getApp()
    const baseURL = app.globalData.baseURL || wx.getStorageSync('baseURL') || 'https://your-manager-domain.com'
    this.setData({ baseURL })
  },

  onBaseURLInput(e) {
    this.setData({ baseURL: e.detail.value })
  },

  onUsernameInput(e) {
    this.setData({ username: e.detail.value })
  },

  onPasswordInput(e) {
    this.setData({ password: e.detail.value })
  },

  async onSubmit() {
    const { baseURL, username, password } = this.data
    if (!baseURL || !username || !password) {
      wx.showToast({ title: '请完整填写信息', icon: 'none' })
      return
    }

    this.setData({ loading: true })
    try {
      wx.setStorageSync('baseURL', baseURL)
      getApp().globalData.baseURL = baseURL

      const res = await api.login({ username, password })
      wx.setStorageSync('token', res.token)
      wx.setStorageSync('user', res.user)

      wx.switchTab({ url: '/pages/console/index' })
    } catch (err) {
      wx.showToast({ title: err?.data?.error || '登录失败', icon: 'none' })
    } finally {
      this.setData({ loading: false })
    }
  }
})
