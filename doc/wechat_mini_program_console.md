# 控制台微信小程序接入方案（Web-View）

本文档提供将现有 Manager 控制台接入微信小程序的完整落地方案。

## 1. 架构说明

- 小程序端使用 `web-view` 打开控制台 H5 页面。
- 控制台前端支持从 URL 参数读取登录态（`token` + `user/user_b64`）。
- 控制台后端支持微信小程序常见跨域请求头和来源域名。
- 控制台登录/登出后，可通过 `postMessage` 回传给小程序。

## 2. 后端能力（本仓库已支持）

### 2.1 CORS 兼容

已在 `manager/backend/router/router.go` 中增加：

- 微信小程序相关来源识别（`servicewechat.com` / `wechat.com` / `weixin.qq.com`）。
- 放开 `OPTIONS` 等预检请求方法。
- 放开微信网关常见头：
  - `X-WX-EXCLUDE-CREDENTIALS`
  - `X-WX-GATEWAY-ID`
  - `Wechat-Gateway-Request-Id`

### 2.2 认证方式兼容

已在 `manager/backend/middleware/auth.go` 中支持多种 token 传递方式：

1. `Authorization: Bearer xxx`
2. `X-Access-Token: xxx`
3. `X-WX-Token: xxx`
4. URL 参数：`?token=xxx`

> 推荐优先使用 `Authorization`，其余方式用于小程序/网关受限场景。

## 3. 前端能力（本仓库已支持）

### 3.1 URL 登录态注入

前端启动时会自动读取 URL 参数：

- `token`：必填
- `user`：可选，JSON 字符串
- `user_b64`：可选，Base64URL 编码 JSON
- `redirect`：可选，后续可用于跳转控制

读取后会写入 `localStorage` 并自动清理 URL，避免 token 在地址栏泄露。

### 3.2 与小程序通信

控制台登录/登出时会触发消息：

- `auth:login`
- `auth:logout`

发送方式优先 `window.wx.miniProgram.postMessage`，失败则降级到 `window.parent.postMessage`。

## 4. 小程序端接入示例

## 4.1 打开控制台

```js
const token = '后端签发的JWT'
const user = encodeURIComponent(JSON.stringify({ id: 1, username: 'demo', role: 'user' }))
const consoleUrl = `https://your-console-domain/login?token=${encodeURIComponent(token)}&user=${user}`

wx.navigateTo({
  url: `/pages/console/index?url=${encodeURIComponent(consoleUrl)}`
})
```

## 4.2 web-view 页面

```xml
<web-view src="{{url}}" bindmessage="onConsoleMessage" />
```

```js
Page({
  data: { url: '' },
  onLoad(query) {
    this.setData({ url: decodeURIComponent(query.url || '') })
  },
  onConsoleMessage(e) {
    const payload = e?.detail?.data?.[0] || {}
    if (payload.event === 'auth:logout') {
      // 例如：同步清理小程序侧会话
    }
  }
})
```

## 5. 域名与发布要求

1. 小程序后台需配置业务域名（控制台域名）。
2. 控制台域名必须为 HTTPS。
3. 若前后端分离部署，请确保 API 域名也在小程序合法域名白名单中。

## 6. 安全建议

1. token 建议短时效，并在服务端支持刷新机制。
2. 尽量使用 `user_b64`，避免明文 JSON 直接暴露在 URL。
3. 控制台页面加载后立即清理 URL 参数（本项目已处理）。
4. 如需更高安全等级，建议改为“一次性登录票据 + 后端换 token”模式。
