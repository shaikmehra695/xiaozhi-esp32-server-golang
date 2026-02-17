const api = require('../../utils/api')
const { ensureLogin } = require('../../utils/auth')

Page({
  data: {
    user: null,
    stats: null,
    loading: false
  },

  onShow() {
    if (!ensureLogin()) return
    this.loadData()
  },

  async loadData() {
    this.setData({ loading: true })
    try {
      const [profileRes, statsRes] = await Promise.all([
        api.getProfile(),
        api.getDashboardStats()
      ])
      this.setData({
        user: profileRes.user,
        stats: statsRes
      })
    } catch (err) {
      wx.showToast({ title: err?.data?.error || '加载失败', icon: 'none' })
    } finally {
      this.setData({ loading: false })
    }
  }
})
