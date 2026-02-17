import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// Vant 4 按需引入，减少打包体积
import {
  NavBar,
  Tabbar,
  TabbarItem,
  Form,
  Field,
  CellGroup,
  Button,
  Tabs,
  Tab,
  Cell,
  Popup,
  Icon
} from 'vant'
import 'vant/lib/index.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
import { initMiniProgramAuthFromQuery } from './utils/miniProgram'

// 小程序 web-view 可通过 URL 注入 token/user，前端启动时自动接管并清理 URL
initMiniProgramAuthFromQuery()

const app = createApp(App)

// 注册所有Element Plus图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

// 注册Vant组件（按需引入）
app.use(NavBar)
app.use(Tabbar)
app.use(TabbarItem)
app.use(Form)
app.use(Field)
app.use(CellGroup)
app.use(Button)
app.use(Tabs)
app.use(Tab)
app.use(Cell)
app.use(Popup)
app.use(Icon)

app.use(createPinia())
app.use(router)
app.use(ElementPlus) // 桌面端使用

app.mount('#app')
