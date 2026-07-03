import { http, unwrap } from '@/lib/http-client'

export interface IlinkAssistant {
  account_id: string
  display_name: string
  wechat_id?: string
  qrcode_url?: string
}

export interface IlinkStatus {
  available: boolean
  bound: boolean
  status: string
  default_project_id?: string
  assistant?: IlinkAssistant
  bound_at?: string
}

export interface IlinkBindCodeResult {
  available: boolean
  bind_code: string
  expires_at: string
  assistant?: IlinkAssistant
  status: string
}

export const ilinkApi = {
  status: () => unwrap<IlinkStatus>(http.get('/ilink/status')),
  createBindCode: () => unwrap<IlinkBindCodeResult>(http.post('/ilink/bind-code')),
  unbind: () => unwrap<{ unbound: boolean }>(http.post('/ilink/unbind')),
  setDefaultProject: (projectId: string) =>
    unwrap<{ default_project_id: string }>(http.put('/ilink/default-project', { project_id: projectId })),
}
