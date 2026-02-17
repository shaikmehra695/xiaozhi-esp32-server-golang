App({
  globalData: {
    baseURL: ''
  },

  onLaunch() {
    const savedBaseURL = wx.getStorageSync('baseURL')
    this.globalData.baseURL = savedBaseURL || 'https://your-manager-domain.com'
  }
})
