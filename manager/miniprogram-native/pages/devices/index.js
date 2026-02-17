const api = require('../../utils/api')
const { ensureLogin } = require('../../utils/auth')

Page({
  data: {
    list: [],
    loading: false
  },

  onShow() {
    if (!ensureLogin()) return
    this.loadDevices()
  },

  async loadDevices() {
    this.setData({ loading: true })
    try {
      const res = await api.getDevices()
      const list = res.devices || res.data || []
      this.setData({ list })
    } catch (err) {
      wx.showToast({ title: err?.data?.error || '加载设备失败', icon: 'none' })
    } finally {
      this.setData({ loading: false })
    }
  }
})
