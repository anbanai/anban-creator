import { http, unwrap } from '@/lib/http-client'

/**
 * WeChat bot binding (wcfLink sidecar). Lets a user bind their own WeChat so the
 * server can push task success/failure notifications and accept commands via the
 * 文件传输助手 self-chat. See server/handler/wcf.go + server/service/wcf_binding.go.
 */

export interface WeChatBindStartResult {
  session_id: string
  qrcode_url: string // data:image/png;base64,...
  status: string // "pending"
}

export interface WeChatBindStatus {
  available: boolean
  /** unbound | pending | active | wait | scanned | expired | error */
  status: string
  account_id?: string
  default_project_id?: string
  bound_at?: string
}

export const wechatApi = {
  /** POST /wechat/bind/start — start a QR login; returns the QR data URI + session id. */
  bindStart: () => unwrap<WeChatBindStartResult>(http.post('/wechat/bind/start')),

  /** GET /wechat/bind/status?session_id= — poll until status flips to "active". */
  bindStatus: (sessionId: string) =>
    unwrap<WeChatBindStatus>(http.get('/wechat/bind/status', { params: { session_id: sessionId } })),

  /** POST /wechat/unbind — remove the current binding. */
  unbind: () => unwrap<{ unbound: boolean }>(http.post('/wechat/unbind')),

  /** GET /wechat/status — current binding state (also reports sidecar availability). */
  status: () => unwrap<WeChatBindStatus>(http.get('/wechat/status')),

  /** PUT /wechat/default-project — set the project used by inbound create-task commands. */
  setDefaultProject: (projectId: string) =>
    unwrap<{ default_project_id: string }>(http.put('/wechat/default-project', { project_id: projectId })),
}
