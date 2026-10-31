package buyerblock

import "testing"

// TestNormalizeBuyerID 核验稳定买家 ID 的兼容后缀和拒绝边界，不使用真实用户标识。
func TestNormalizeBuyerID(t *testing.T) {
	for _, input := range []string{"123", " 123@goofish "} { // input 是合法标识的两种常见格式。
		// got、err 为规范化输出和验证结果。
		got, err := NormalizeBuyerID(input)
		if err != nil || got != "123" {
			t.Fatalf("normalize: %q %v", got, err)
		}
	}
	for _, input := range []string{"", "a b", "123@other"} { // input 是不允许猜作稳定身份的内容。
		// err 必须拒绝歧义标识。
		if _, err := NormalizeBuyerID(input); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}
