const ensureLogin = () => {
  const token = wx.getStorageSync('token')
  if (!token) {
    wx.reLaunch({ url: '/pages/login/index' })
    return false
  }
  return true
}

module.exports = {
  ensureLogin
}
