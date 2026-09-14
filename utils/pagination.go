package utils

const defaultFallbackPageSize = 20

// GetPage 返回合法的页码（最小为 1）。
func GetPage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

// GetPageSize 返回合法的分页大小。
// 当 pageSize <= 0 时使用 defaultSize，若 defaultSize 非法则退回通用默认值。
func GetPageSize(pageSize, defaultSize int) int {
	if pageSize > 0 {
		return pageSize
	}

	fallback := defaultSize
	if fallback <= 0 {
		fallback = defaultFallbackPageSize
	}

	return fallback
}
