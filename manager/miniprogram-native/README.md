# 原生小程序控制台（MVP）

这是基于现有 `manager/backend` API 的原生微信小程序控制台示例，不依赖 web-view。

## 已实现页面

- 登录：`pages/login`
- 控制台首页：`pages/console`
- 智能体列表：`pages/agents`
- 设备列表：`pages/devices`
- 我的信息/退出：`pages/profile`

## 对接接口

- `POST /api/login`
- `GET /api/profile`
- `GET /api/dashboard/stats`
- `GET /api/user/agents`
- `GET /api/user/devices`

## 使用方式

1. 在微信开发者工具中导入 `manager/miniprogram-native`。
2. 登录页填入后端地址（例如 `https://your-manager-domain.com`）和账号密码。
3. 登录成功后进入控制台 tab。

## 说明

- 当前采用 Bearer Token（`Authorization`）鉴权。
- 若你需要“管理员配置管理”等高级页面，可在此基础上继续扩展。
- 生产环境请在小程序后台配置合法域名，且后端必须 HTTPS。
