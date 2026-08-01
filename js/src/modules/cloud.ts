import type { SdkClient } from '../client'

/**
 * ⚠️ 云变量/云函数模块 — 已下线（孤儿模块）
 *
 * 对应的服务端路由 `/api/app/cloud/*` 已于 commit 6f947dd 移除（AppSign 路由组整体下线）。
 * 本模块保留仅为兼容引用，调用会 404。云变量/云函数的管理已改走 admin-web 后台（/api/admin/cloud/*）。
 *
 * TODO: 确认无下游引用后删除本文件。
 */
export function createCloudModule(client: SdkClient) {
  return {
    /**
     * 获取所有可读云变量
     * @param scope_key 用户/设备标识（scope=user/device 时需要）
     */
    getVars(scopeKey?: string) {
      return client.get<Record<string, any>>('/app/cloud/vars', scopeKey ? { scope_key: scopeKey } : undefined)
    },

    /**
     * 获取单个云变量
     * @param key 变量名
     * @param scope_key 用户/设备标识
     */
    getVar(key: string, scopeKey?: string) {
      return client.get<any>(`/app/cloud/vars/${key}`, scopeKey ? { scope_key: scopeKey } : undefined)
    },

    /**
     * 写入云变量值（仅 access=read_write + scope=user/device 时允许）
     * @param key 变量名
     * @param value 变量值
     * @param scopeKey 用户/设备标识
     */
    setVar(key: string, value: any, scopeKey: string) {
      return client.put(`/app/cloud/vars/${key}`, {
        scope_key: scopeKey,
        value: String(value)
      })
    },

    /**
     * 调用云函数
     * @param funcName 函数名
     * @param params 调用参数
     * @param scopeKey 调用者标识
     */
    call<T = any>(funcName: string, params?: Record<string, any>, scopeKey?: string) {
      return client.post<T>(`/app/cloud/call/${funcName}`, {
        params: params || {},
        scope_key: scopeKey || ''
      })
    }
  }
}
