const api = require('../../utils/api')
const { ensureLogin } = require('../../utils/auth')

Page({
  data: {
    list: [],
    loading: false
  },

  onShow() {
    if (!ensureLogin()) return
    this.loadAgents()
  },

  async loadAgents() {
    this.setData({ loading: true })
    try {
      const res = await api.getAgents()
      const list = res.agents || res.data || []
      this.setData({ list })
    } catch (err) {
      wx.showToast({ title: err?.data?.error || '加载智能体失败', icon: 'none' })
    } finally {
      this.setData({ loading: false })
    }
  }
})
