package native

// KV 配置读取（GET /as/v1/app/kv，DeviceSign 签名，复用 ads.go 的 getSigned/decodeEnvelope）。
// 服务端返回本应用 + 公共区（__common__）的合并结果，本应用同 key 优先。

import (
	"context"
	"net/url"
)

// endpointKV KV 配置端点
const endpointKV = "/as/v1/app/kv"

// GetKV 拉取当前应用可见的全部 KV 配置（DeviceSign 鉴权，头 X-App-ID）。
// 返回 {key: value} 映射，值已由服务端按类型解析：
// string→string、number→float64、boolean→bool、json→原生 map/slice。
func (n *Native) GetKV(ctx context.Context, deviceID string) (map[string]any, error) {
	var vars map[string]any
	if err := n.getSigned(ctx, endpointKV, url.Values{}, deviceID, &vars); err != nil {
		return nil, err
	}
	return vars, nil
}
