package native

// KV 配置读取（GET /as/v1/app/kv，DeviceSign 签名，复用 ads.go 的 getSigned/decodeEnvelope）。
// 服务端返回本应用 + 公共区（__common__）的合并结果，本应用同 key 优先。

import (
	"context"
	"net/url"
	"strings"
)

// endpointKV KV 配置端点
const endpointKV = "/as/v1/app/kv"

// GetKV 拉取应用可见的 KV 配置（DeviceSign 鉴权，头 X-App-ID）。
// 返回 {key: value} 映射，值已由服务端按类型解析：
// string→string、number→float64、boolean→bool、json→原生 map/slice。
//
// keys 可选（variadic）：不传返回全部；传则只返回指定键（?key=k1,k2），
// 不存在的键直接缺席。按需过滤仅节省传输，不是访问控制。
func (n *Native) GetKV(ctx context.Context, deviceID string, keys ...string) (map[string]any, error) {
	query := url.Values{}
	if len(keys) > 0 {
		query.Set("key", strings.Join(keys, ","))
	}
	var vars map[string]any
	if err := n.getSigned(ctx, endpointKV, query, deviceID, &vars); err != nil {
		return nil, err
	}
	return vars, nil
}
