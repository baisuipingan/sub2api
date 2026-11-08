package repository

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestStudioErrorTruncationKeepsValidUTF8AndUsefulDetails(t *testing.T) {
	for _, value := range []string{
		"[MODERATION_BLOCKED]: " + strings.Repeat("审核拒绝", 400),
		" x" + strings.Repeat("图片生成失败", 400),
		strings.Repeat("🙂", 400),
		"invalid byte \xff is removed",
	} {
		got := truncateStudioError(value)
		require.True(t, utf8.ValidString(got))
		require.LessOrEqual(t, len(got), 1000)
		require.NotEmpty(t, got)
	}
}
