import { createRoot } from 'react-dom/client'
import LivePortraitOnboarding from './LivePortraitOnboarding'
import '@/index.css'

const localOnlinePreview = location.hostname === '127.0.0.1' && location.port === '5175'
createRoot(document.getElementById('root')!).render(localOnlinePreview
  ? <LivePortraitOnboarding onlineDelivery />
  : <p>请通过本机线上联调入口（127.0.0.1:5175）打开此页面。</p>)
